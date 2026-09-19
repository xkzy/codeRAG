// Package search provides replaceable ranked retrieval over small in-memory
// corpora. Services build a corpus per query from graph nodes, so rankers are
// stateless and any implementation (BM25, vector, hybrid) can be swapped in.
package search

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// Field is one weighted piece of a document, e.g. a title counted more than a body.
type Field struct {
	Text   string
	Weight float64
}

// Document is a retrievable item identified by ID.
type Document struct {
	ID     string
	Fields []Field
}

// Hit is a ranked result. Higher scores are better.
type Hit struct {
	ID    string
	Score float64
}

// Ranker orders documents by relevance to a query and returns at most limit
// hits. Documents with no relevance must be omitted.
type Ranker interface {
	Rank(query string, docs []Document, limit int) []Hit
}

// Tokenize lower-cases text and splits it on non-alphanumerics and on
// camelCase / snake_case boundaries. Identifiers also keep their whole form, so
// "ParsePacket" yields "parsepacket", "parse" and "packet". Plurals are folded.
func Tokenize(text string) []string {
	var out []string
	for _, word := range strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		parts := splitCamel(word)
		if len(parts) > 1 {
			out = append(out, stem(strings.ToLower(word)))
		}
		for _, p := range parts {
			out = append(out, stem(strings.ToLower(p)))
		}
	}
	return out
}

func splitCamel(word string) []string {
	runes := []rune(word)
	var parts []string
	start := 0
	for i := 1; i < len(runes); i++ {
		prev, cur := runes[i-1], runes[i]
		boundary := (unicode.IsLower(prev) && unicode.IsUpper(cur)) ||
			(unicode.IsLetter(prev) != unicode.IsLetter(cur)) ||
			(unicode.IsUpper(prev) && unicode.IsUpper(cur) && i+1 < len(runes) && unicode.IsLower(runes[i+1]))
		if boundary {
			parts = append(parts, string(runes[start:i]))
			start = i
		}
	}
	return append(parts, string(runes[start:]))
}

func stem(t string) string {
	switch {
	case len(t) > 4 && strings.HasSuffix(t, "ies"):
		return t[:len(t)-3] + "y"
	case len(t) > 4 && strings.HasSuffix(t, "es") && !strings.HasSuffix(t, "ses"):
		return t[:len(t)-2]
	case len(t) > 3 && strings.HasSuffix(t, "s") && !strings.HasSuffix(t, "ss"):
		return t[:len(t)-1]
	}
	return t
}

// BM25 is Okapi BM25 with per-field weights (a BM25F-style weighted term
// frequency). The zero value is not usable; use NewBM25.
type BM25 struct {
	K1, B float64
}

func NewBM25() *BM25 { return &BM25{K1: 1.2, B: 0.75} }

func (b *BM25) Rank(query string, docs []Document, limit int) []Hit {
	qTerms := uniqueTerms(Tokenize(query))
	if len(qTerms) == 0 || len(docs) == 0 {
		return nil
	}
	type stats struct {
		tf  map[string]float64
		len float64
	}
	all := make([]stats, len(docs))
	df := map[string]int{}
	var totalLen float64
	for i, d := range docs {
		st := stats{tf: map[string]float64{}}
		for _, f := range d.Fields {
			for _, tok := range Tokenize(f.Text) {
				st.tf[tok] += f.Weight
				st.len += f.Weight
			}
		}
		for tok := range st.tf {
			df[tok]++
		}
		all[i] = st
		totalLen += st.len
	}
	avg := totalLen / float64(len(docs))
	if avg == 0 {
		return nil
	}
	n := float64(len(docs))
	var hits []Hit
	for i, d := range docs {
		var score float64
		for _, t := range qTerms {
			f := all[i].tf[t]
			if f == 0 {
				continue
			}
			idf := math.Log(1 + (n-float64(df[t])+0.5)/(float64(df[t])+0.5))
			score += idf * f * (b.K1 + 1) / (f + b.K1*(1-b.B+b.B*all[i].len/avg))
		}
		if score > 0 {
			hits = append(hits, Hit{ID: d.ID, Score: score})
		}
	}
	return topK(hits, limit)
}

func uniqueTerms(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range in {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

func topK(hits []Hit, limit int) []Hit {
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].ID < hits[j].ID
	})
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// VectorRanker ranks documents by cosine similarity between bag-of-words
// vectors of the query and each document's fields. It provides a semantic
// signal orthogonal to BM25's keyword matching and can be plugged into
// Hybrid alongside BM25.
type VectorRanker struct {
	K1 float64 // boost for exact term matches within the vector space
}

func NewVectorRanker() *VectorRanker { return &VectorRanker{K1: 1.0 } }

func (v *VectorRanker) Rank(query string, docs []Document, limit int) []Hit {
	qVec := docVector(Tokenize(query))
	if len(qVec) == 0 || len(docs) == 0 {
		return nil
	}
	var hits []Hit
	for _, d := range docs {
		dVec := docVector(fieldsTokens(d.Fields))
		if len(dVec) == 0 {
			continue
		}
		sim := cosineSimilarity(qVec, dVec)
		if sim > 0 {
			hits = append(hits, Hit{ID: d.ID, Score: sim})
		}
	}
	return topK(hits, limit)
}

func fieldsTokens(fields []Field) []string {
	var out []string
	for _, f := range fields {
		out = append(out, Tokenize(f.Text)...)
	}
	return out
}

func docVector(tokens []string) map[string]float64 {
	v := map[string]float64{}
	for _, t := range tokens {
		v[t]++
	}
	return v
}

func cosineSimilarity(a, b map[string]float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	var dot, magA, magB float64
	for k, av := range a {
		magA += av * av
		if bv, ok := b[k]; ok {
			dot += av * bv
		}
	}
	for _, bv := range b {
		magB += bv * bv
	}
	if magA == 0 || magB == 0 {
		return 0
	}
	return dot / (math.Sqrt(magA) * math.Sqrt(magB))
}

// Hybrid fuses several rankers with reciprocal rank fusion, so a vector ranker
// can be combined with BM25 without their scores being comparable.
type Hybrid struct {
	Rankers []Ranker
	K       float64 // RRF constant, default 60
}

func (h *Hybrid) Rank(query string, docs []Document, limit int) []Hit {
	k := h.K
	if k <= 0 {
		k = 60
	}
	fused := map[string]float64{}
	for _, r := range h.Rankers {
		for rank, hit := range r.Rank(query, docs, 0) {
			fused[hit.ID] += 1 / (k + float64(rank+1))
		}
	}
	hits := make([]Hit, 0, len(fused))
	for id, s := range fused {
		hits = append(hits, Hit{ID: id, Score: s})
	}
	return topK(hits, limit)
}
