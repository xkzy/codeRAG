package privacy

import (
	"regexp"
	"strings"
)

// ContentClassifier classifies content for privacy filtering.
type ContentClassifier struct {
	secretPatterns []*secretPattern
	pathPattern    *regexp.Regexp
}

type secretPattern struct {
	name    string
	pattern *regexp.Regexp
}

// NewContentClassifier creates a classifier with built-in secret detection.
func NewContentClassifier() *ContentClassifier {
	return &ContentClassifier{
		secretPatterns: []*secretPattern{
			{"api_key", regexp.MustCompile(`(?i)(api[_-]?key\s*[:=]\s*["']?)([A-Za-z0-9_\-]{16,})`)},
			{"password", regexp.MustCompile(`(?i)(password\s*[:=]\s*["']?)([^"'\s]{8,})`)},
			{"token", regexp.MustCompile(`(?i)(token\s*[:=]\s*["']?)([A-Za-z0-9_\-\.]{16,})`)},
			{"private_key", regexp.MustCompile(`(?i)(-----BEGIN (RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----)`)},
			{"aws_secret", regexp.MustCompile(`(?i)(aws_secret_access_key\s*[:=]\s*["']?)([A-Za-z0-9/+=]{40})`)},
			{"connection_string", regexp.MustCompile(`(?i)((?:postgres|mysql|mongodb|redis)://[^\s"']+)`)},
			{"jwt", regexp.MustCompile(`(?i)(eyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,})`)},
			{"github_token", regexp.MustCompile(`(?i)(gh[pousr]_[A-Za-z0-9]{20,})`)},
		},
		pathPattern: regexp.MustCompile(`(?i)/(home|users|root|etc|var|tmp|opt|srv|usr|root)/[^\s"']*`),
	}
}

// ClassifyContent classifies a content string into ContentKind.
func (c *ContentClassifier) ClassifyContent(content string) ContentKind {
	if c.IsSecret(content) {
		return ContentLiteral
	}
	if c.IsPath(content) {
		return ContentPath
	}
	if c.IsComment(content) {
		return ContentComment
	}
	if c.IsStringLiteral(content) {
		return ContentString
	}
	return ContentSource
}

// IsSecret returns true if content matches a secret pattern.
func (c *ContentClassifier) IsSecret(content string) bool {
	for _, p := range c.secretPatterns {
		if p.pattern.MatchString(content) {
			return true
		}
	}
	return false
}

// IsPath returns true if content looks like a filesystem path.
func (c *ContentClassifier) IsPath(content string) bool {
	return c.pathPattern.MatchString(content)
}

// IsComment returns true if content is a code comment.
func (c *ContentClassifier) IsComment(content string) bool {
	trimmed := strings.TrimSpace(content)
	return strings.HasPrefix(trimmed, "//") ||
		strings.HasPrefix(trimmed, "#") ||
		strings.HasPrefix(trimmed, "*") ||
		strings.HasPrefix(trimmed, "/*") ||
		strings.HasPrefix(trimmed, "--") ||
		strings.HasPrefix(trimmed, ";")
}

// IsStringLiteral returns true if content is a string literal.
func (c *ContentClassifier) IsStringLiteral(content string) bool {
	trimmed := strings.TrimSpace(content)
	return (strings.HasPrefix(trimmed, `"`) && strings.HasSuffix(trimmed, `"`) && len(trimmed) >= 2) ||
		(strings.HasPrefix(trimmed, "'") && strings.HasSuffix(trimmed, "'") && len(trimmed) >= 2) ||
		(strings.HasPrefix(trimmed, "`") && strings.HasSuffix(trimmed, "`") && len(trimmed) >= 2)
}

// RedactSecrets replaces secret values with a redaction marker.
func (c *ContentClassifier) RedactSecrets(content string) string {
	result := content
	for _, p := range c.secretPatterns {
		result = p.pattern.ReplaceAllStringFunc(result, func(match string) string {
			// Replace the secret value but keep the key prefix
			parts := p.pattern.FindStringSubmatch(match)
			if len(parts) >= 2 {
				return parts[1] + "***REDACTED***"
			}
			return "***REDACTED***"
		})
	}
	return result
}

// ClassifySymbol classifies a symbol name into a pseudonym kind.
func ClassifySymbol(name string) PseudonymKind {
	trimmed := strings.TrimSpace(name)
	if strings.Contains(trimmed, "::") || strings.Contains(trimmed, ".") {
		return PseudoModule
	}
	if strings.HasPrefix(trimmed, "type ") || strings.HasPrefix(trimmed, "struct ") ||
		strings.HasPrefix(trimmed, "class ") || strings.HasPrefix(trimmed, "interface ") {
		return PseudoType
	}
	if strings.HasPrefix(trimmed, "func ") || strings.HasPrefix(trimmed, "fn ") ||
		strings.HasPrefix(trimmed, "def ") || strings.HasPrefix(trimmed, "function ") {
		return PseudoFunc
	}
	if strings.HasPrefix(trimmed, "var ") || strings.HasPrefix(trimmed, "let ") ||
		strings.HasPrefix(trimmed, "const ") || strings.HasPrefix(trimmed, "val ") {
		return PseudoVar
	}
	if strings.HasPrefix(trimmed, "field_") || strings.HasPrefix(trimmed, "m_") ||
		strings.HasPrefix(trimmed, "s_") || strings.HasPrefix(trimmed, "_") {
		return PseudoField
	}
	return PseudoUnknown
}
