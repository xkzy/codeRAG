package privacy

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Pseudonymizer maintains stable local pseudonyms for identifiers.
type Pseudonymizer struct {
	mu          sync.RWMutex
	nameToToken map[string]string
	tokenToName map[string]string
	counters    map[PseudonymKind]int
}

// NewPseudonymizer creates a new pseudonymizer.
func NewPseudonymizer() *Pseudonymizer {
	return &Pseudonymizer{
		nameToToken: make(map[string]string),
		tokenToName: make(map[string]string),
		counters:    make(map[PseudonymKind]int),
	}
}

// Pseudonymize replaces an identifier with a stable local token.
// The same name always maps to the same token within a project.
func (p *Pseudonymizer) Pseudonymize(name string, kind PseudonymKind) string {
	p.mu.Lock()
	defer p.mu.Unlock()

	if token, ok := p.nameToToken[name]; ok {
		return token
	}

	p.counters[kind]++
	token := fmt.Sprintf("%s_%d", kind, p.counters[kind])
	p.nameToToken[name] = token
	p.tokenToName[token] = name
	return token
}

// Resolve reverses a pseudonym to the original identifier.
// Returns the original name and true if found.
func (p *Pseudonymizer) Resolve(token string) (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	name, ok := p.tokenToName[token]
	return name, ok
}

// ResolveAll returns all token-to-name mappings.
func (p *Pseudonymizer) ResolveAll() map[string]string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make(map[string]string, len(p.tokenToName))
	for k, v := range p.tokenToName {
		result[k] = v
	}
	return result
}

// Snapshot returns the current state for persistence.
func (p *Pseudonymizer) Snapshot() *PseudonymSnapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	snap := &PseudonymSnapshot{
		NameToToken: make(map[string]string, len(p.nameToToken)),
		TokenToName: make(map[string]string, len(p.tokenToName)),
		Counters:    make(map[PseudonymKind]int, len(p.counters)),
	}
	for k, v := range p.nameToToken {
		snap.NameToToken[k] = v
	}
	for k, v := range p.tokenToName {
		snap.TokenToName[k] = v
	}
	for k, v := range p.counters {
		snap.Counters[k] = v
	}
	return snap
}

// Restore loads a previous snapshot.
func (p *Pseudonymizer) Restore(snap *PseudonymSnapshot) {
	if snap == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nameToToken = snap.NameToToken
	p.tokenToName = snap.TokenToName
	p.counters = snap.Counters
	if p.nameToToken == nil {
		p.nameToToken = make(map[string]string)
	}
	if p.tokenToName == nil {
		p.tokenToName = make(map[string]string)
	}
	if p.counters == nil {
		p.counters = make(map[PseudonymKind]int)
	}
}

// PseudonymSnapshot is a serializable snapshot of the pseudonymizer.
type PseudonymSnapshot struct {
	NameToToken map[string]string     `json:"name_to_token"`
	TokenToName map[string]string     `json:"token_to_name"`
	Counters    map[PseudonymKind]int `json:"counters"`
}

// PseudonymizeText replaces all known identifiers in text with their tokens.
func (p *Pseudonymizer) PseudonymizeText(text string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	names := make([]string, 0, len(p.nameToToken))
	for name := range p.nameToToken {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return len(names[i]) > len(names[j])
	})

	result := text
	for _, name := range names {
		result = strings.ReplaceAll(result, name, p.nameToToken[name])
	}
	return result
}
