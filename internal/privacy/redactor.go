package privacy

import (
	"strings"
)

// RedactionResult holds the output of a redaction operation.
type RedactionResult struct {
	Original    string           `json:"original"`
	Sanitized   string           `json:"sanitized"`
	Redactions  []RedactionEntry `json:"redactions"`
	ContentKind ContentKind      `json:"content_kind"`
}

// RedactionEntry describes one redaction.
type RedactionEntry struct {
	Position int    `json:"position"`
	Length   int    `json:"length"`
	Original string `json:"original"`
	Reason   string `json:"reason"`
}

// Redactor applies policy-based redaction to content.
type Redactor struct {
	classifier *ContentClassifier
	policy     *PrivacyPolicy
}

// NewRedactor creates a redactor with the given policy.
func NewRedactor(policy *PrivacyPolicy) *Redactor {
	return &Redactor{
		classifier: NewContentClassifier(),
		policy:     policy,
	}
}

// Redact applies policy-based redaction to content.
func (r *Redactor) Redact(content string) *RedactionResult {
	result := &RedactionResult{
		Original:    content,
		ContentKind: r.classifier.ClassifyContent(content),
	}

	if r.policy.Mode == ModeLocalOnly {
		result.Sanitized = "***LOCAL_ONLY***"
		result.Redactions = append(result.Redactions, RedactionEntry{
			Reason: "privacy mode is LOCAL_ONLY",
		})
		return result
	}

	// Always redact secrets
	sanitized := r.classifier.RedactSecrets(content)
	if sanitized != content {
		result.Redactions = append(result.Redactions, RedactionEntry{
			Reason: "secret detected",
		})
	}

	// Redact paths if forbidden
	if r.policy.IsPathForbidden(content) || !r.policy.IsPathAllowed(content) {
		if replaced := r.classifier.pathPattern.ReplaceAllString(sanitized, "***PATH***"); replaced != sanitized {
			sanitized = replaced
			result.Redactions = append(result.Redactions, RedactionEntry{
				Reason: "path not allowed",
			})
		}
	}

	// Apply max source bytes
	if r.policy.MaxSourceBytes > 0 && len(sanitized) > r.policy.MaxSourceBytes {
		sanitized = sanitized[:r.policy.MaxSourceBytes] + "..."
		result.Redactions = append(result.Redactions, RedactionEntry{
			Reason: "max source bytes exceeded",
		})
	}

	result.Sanitized = sanitized
	return result
}

// RedactSource applies source-level redaction with pseudonymization.
func (r *Redactor) RedactSource(content string, pseudos *Pseudonymizer) *RedactionResult {
	result := r.Redact(content)

	if !r.policy.AllowsSource() {
		result.Sanitized = "***SOURCE_NOT_ALLOWED***"
		result.Redactions = append(result.Redactions, RedactionEntry{
			Reason: "exact source not permitted by policy",
		})
		return result
	}

	if r.policy.Mode == ModeMasked || r.policy.Mode == ModeAbstract {
		result.Sanitized = pseudos.PseudonymizeText(result.Sanitized)
	}

	return result
}

// IsAllowed returns true if content can be transmitted under this policy.
func (r *Redactor) IsAllowed(content string) bool {
	if r.policy.Mode == ModeLocalOnly {
		return false
	}
	if r.classifier.IsSecret(content) && !r.policy.AllowStrings {
		return false
	}
	return true
}

// HasSecrets returns true if content contains detected secrets.
func (r *Redactor) HasSecrets(content string) bool {
	return r.classifier.IsSecret(content)
}

// HasForbiddenPath returns true if content contains a forbidden path.
func (r *Redactor) HasForbiddenPath(content string) bool {
	return r.policy.IsPathForbidden(content)
}

// HasForbiddenIdentifier returns true if content contains a forbidden identifier.
func (r *Redactor) HasForbiddenIdentifier(content string) bool {
	for _, id := range r.policy.ForbiddenIdentifiers {
		if strings.Contains(content, id) {
			return true
		}
	}
	return false
}
