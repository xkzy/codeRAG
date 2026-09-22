package runtime

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"

	"codergag/internal/reverse"
)

var (
	fileLineRegex = regexp.MustCompile(`^([a-zA-Z0-9_\-\\/.]+):(\d+)(?::(\d+))?:`)
	addressRegex  = regexp.MustCompile(`^(0x[0-9a-fA-F]+)\s*(?:<([^+>]+)(?:\+0x([0-9a-fA-F]+))?>)?`)
	stackFrameRegex = regexp.MustCompile(`^#(\d+)\s+([^\s\(]+)(?:\s+\(([^\)]+)\))?`)
	exceptionRegex  = regexp.MustCompile(`(?i)(?:exception|error|fault|panic|abort):\s*([^\s]+)`)
	errorCodeRegex = regexp.MustCompile(`(?i)(?:error|errno|code)[:\s=]+([0-9a-fA-FxX\-]+)`)
	testFailRegex   = regexp.MustCompile(`(?i)FAIL\s+(?:---\s+)?([a-zA-Z0-9_\./]+)`)
	symbolOffsetRegex = regexp.MustCompile(`^([a-zA-Z0-9_\.]+)\s*\+\s*(0x[0-9a-fA-F]+|0\d+)`)
	moduleOffsetRegex  = regexp.MustCompile(`^([^\s!]+)!([0-9a-fA-FxX]+)`)
)

type Parser struct {
	logTemplates map[string]*reverse.LogTemplate
	normalizers  []Normalizer
}

type Normalizer func([]byte) []byte

func NewParser() *Parser {
	return &Parser{
		logTemplates: make(map[string]*reverse.LogTemplate),
		normalizers:  defaultNormalizers(),
	}
}

func defaultNormalizers() []Normalizer {
	return []Normalizer{
		func(b []byte) []byte {
			return bytesRemoveANSI(b)
		},
		trimWhitespace,
	}
}

func bytesRemoveANSI(b []byte) []byte {
	result := make([]byte, 0, len(b))
	inEscape := false
	for i := 0; i < len(b); i++ {
		if b[i] == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if b[i] == 'm' {
				inEscape = false
			}
			continue
		}
		result = append(result, b[i])
	}
	return result
}

func trimWhitespace(b []byte) []byte {
	return bytes.TrimSpace(b)
}

func (p *Parser) Parse(raw []byte) *reverse.RuntimeObservation {
	for _, norm := range p.normalizers {
		raw = norm(raw)
	}

	obs := &reverse.RuntimeObservation{
		Extracted: reverse.ExtractedFields{},
	}

	lines := strings.Split(string(raw), "\n")
	var messageLines []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if parsed := p.parseFileLine(line, obs); parsed {
			messageLines = append(messageLines, line)
			continue
		}

		if parsed := p.parseAddress(line, obs); parsed {
			messageLines = append(messageLines, line)
			continue
		}

		if parsed := p.parseStackFrame(line, obs); parsed {
			continue
		}

		if parsed := p.parseException(line, obs); parsed {
			messageLines = append(messageLines, line)
			continue
		}

		if parsed := p.parseErrorCode(line, obs); parsed {
			continue
		}

		if parsed := p.parseTestFailure(line, obs); parsed {
			messageLines = append(messageLines, line)
			continue
		}

		if parsed := p.parseSymbolOffset(line, obs); parsed {
			messageLines = append(messageLines, line)
			continue
		}

		if parsed := p.parseModuleOffset(line, obs); parsed {
			messageLines = append(messageLines, line)
			continue
		}

		messageLines = append(messageLines, line)
	}

	obs.NormalizedText = strings.Join(messageLines, " | ")
	obs.RawHash = hashRaw(raw)
	obs.Severity = p.classifySeverity(obs)
	obs.EventType = p.classifyEventType(obs)

	return obs
}

func (p *Parser) parseFileLine(line string, obs *reverse.RuntimeObservation) bool {
	m := fileLineRegex.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	obs.Extracted.File = m[1]
	if line, err := strconv.Atoi(m[2]); err == nil {
		obs.Extracted.Line = line
	}
	if len(m) > 3 && m[3] != "" {
		if col, err := strconv.Atoi(m[3]); err == nil {
			obs.Extracted.Column = col
		}
	}
	return true
}

func (p *Parser) parseAddress(line string, obs *reverse.RuntimeObservation) bool {
	m := addressRegex.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	obs.Extracted.Address = m[1]
	if len(m) > 2 && m[2] != "" {
		obs.Extracted.Symbol = m[2]
	}
	if len(m) > 3 && m[3] != "" {
		obs.Extracted.Offset = "0x" + m[3]
	}
	return true
}

func (p *Parser) parseStackFrame(line string, obs *reverse.RuntimeObservation) bool {
	m := stackFrameRegex.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	idx, _ := strconv.Atoi(m[1])
	frame := reverse.StackFrame{
		Index: idx,
		Name:  m[2],
	}
	if len(m) > 3 && m[3] != "" {
		frame.Address = m[3]
	}
	obs.Extracted.Stack = append(obs.Extracted.Stack, frame.Name)
	return true
}

func (p *Parser) parseException(line string, obs *reverse.RuntimeObservation) bool {
	m := exceptionRegex.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	obs.Extracted.Exception = m[1]
	return true
}

func (p *Parser) parseErrorCode(line string, obs *reverse.RuntimeObservation) bool {
	m := errorCodeRegex.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	obs.Extracted.ErrorCode = m[1]
	return true
}

func (p *Parser) parseTestFailure(line string, obs *reverse.RuntimeObservation) bool {
	m := testFailRegex.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	obs.Extracted.TestName = m[1]
	obs.EventType = reverse.EventTypeTestFailure
	obs.Severity = reverse.SevP0
	return true
}

func (p *Parser) parseSymbolOffset(line string, obs *reverse.RuntimeObservation) bool {
	m := symbolOffsetRegex.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	obs.Extracted.Symbol = m[1]
	obs.Extracted.Offset = m[2]
	return true
}

func (p *Parser) parseModuleOffset(line string, obs *reverse.RuntimeObservation) bool {
	m := moduleOffsetRegex.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	obs.Extracted.Module = m[1]
	obs.Extracted.Address = m[2]
	return true
}

func (p *Parser) classifySeverity(obs *reverse.RuntimeObservation) reverse.Severity {
	text := strings.ToLower(obs.NormalizedText)

	switch {
	case containsAny(text, "panic", "fatal", "crash", "segfault", "segmentation fault", "assertion failed", "abort"):
		return reverse.SevP0
	case containsAny(text, "error", "exception", "fail", "invalid"):
		return reverse.SevP1
	case containsAny(text, "warning", "warn", "unexpected"):
		return reverse.SevP2
	default:
		return reverse.SevP3
	}
}

func (p *Parser) classifyEventType(obs *reverse.RuntimeObservation) reverse.EventType {
	text := strings.ToLower(obs.NormalizedText)

	switch {
	case containsAny(text, "panic", "fatal", "crash", "segfault", "segmentation fault", "abort"):
		return reverse.EventTypeCrash
	case containsAny(text, "exception"):
		return reverse.EventTypeException
	case containsAny(text, "assertion"):
		return reverse.EventTypeAssertion
	case containsAny(text, "test", "fail"):
		return reverse.EventTypeTestFailure
	case containsAny(text, "linker", "ld:"):
		return reverse.EventTypeLinkerDiag
	case obs.Extracted.File != "" && containsAny(text, "error"):
		return reverse.EventTypeCompilerDiag
	default:
		return reverse.EventTypeInfo
	}
}

func containsAny(s string, substrings ...string) bool {
	for _, sub := range substrings {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func hashRaw(raw []byte) string {
	h := fnv64a()
	h.Write(raw)
	return strconv.FormatUint(h.Sum64(), 16)
}

func fnv64a() *fnv64aHash {
	return &fnv64aHash{}
}

type fnv64aHash struct {
	h uint64
}

func (f *fnv64aHash) Write(p []byte) (int, error) {
	const (
		fnvPrime  = 1099511628211
		fnvOffset = 14695981039346656037
	)
	if f.h == 0 {
		f.h = fnvOffset
	}
	for _, b := range p {
		f.h ^= uint64(b)
		f.h *= fnvPrime
	}
	return len(p), nil
}

func (f *fnv64aHash) Sum64() uint64 {
	return f.h
}

func NormalizeLogTemplate(template string) string {
	result := make([]byte, 0, len(template))
	for i := 0; i < len(template); i++ {
		c := template[i]
		if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_' {
			result = append(result, c)
		} else if c >= '0' && c <= '9' {
			result = append(result, '%')
		} else {
			result = append(result, ' ')
		}
	}
	return strings.TrimSpace(string(result))
}
