package privacy

import (
	"strings"
	"time"
)

// FirewallResult is the output of a firewall enforcement.
type FirewallResult struct {
	Allowed    bool               `json:"allowed"`
	Sanitized  string             `json:"sanitized"`
	Pseudonyms map[string]string  `json:"pseudonyms,omitempty"`
	Redactions []RedactionEntry   `json:"redactions"`
	Disclosure DisclosureLevel    `json:"disclosure"`
	Risk       ReconstructionRisk `json:"risk"`
	Warnings   []string           `json:"warnings,omitempty"`
	Timestamp  string             `json:"timestamp"`
}

// PrivacyFirewall enforces privacy policy on outbound context.
type PrivacyFirewall struct {
	policy     *PrivacyPolicy
	pseudos    *Pseudonymizer
	redactor   *Redactor
	classifier *ContentClassifier
}

// NewFirewall creates a firewall with the given policy.
func NewFirewall(policy *PrivacyPolicy) *PrivacyFirewall {
	return &PrivacyFirewall{
		policy:     policy,
		pseudos:    NewPseudonymizer(),
		redactor:   NewRedactor(policy),
		classifier: NewContentClassifier(),
	}
}

// NewFirewallWithPseudonymizer creates a firewall that shares an existing
// pseudonymizer, so tokens stay stable across firewalls for one project.
func NewFirewallWithPseudonymizer(policy *PrivacyPolicy, pseudos *Pseudonymizer) *PrivacyFirewall {
	f := NewFirewall(policy)
	if pseudos != nil {
		f.pseudos = pseudos
	}
	return f
}

// SanitizeContext applies the full policy to context text.
func (f *PrivacyFirewall) SanitizeContext(content string) *FirewallResult {
	result := &FirewallResult{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Disclosure: f.policy.Disclosure(),
		Risk:       f.policy.Risk(),
	}

	if f.policy.Mode == ModeLocalOnly {
		result.Allowed = false
		result.Sanitized = "***LOCAL_ONLY***"
		result.Warnings = append(result.Warnings, "privacy mode is LOCAL_ONLY")
		return result
	}

	// Detect secrets
	if f.classifier.IsSecret(content) {
		result.Allowed = false
		result.Sanitized = f.classifier.RedactSecrets(content)
		result.Redactions = append(result.Redactions, RedactionEntry{
			Reason: "secret detected",
		})
		result.Warnings = append(result.Warnings, "secret detected - blocked")
		return result
	}

	// Check forbidden identifiers
	if f.redactor.HasForbiddenIdentifier(content) {
		result.Allowed = false
		result.Sanitized = "***FORBIDDEN_IDENTIFIER***"
		result.Warnings = append(result.Warnings, "forbidden identifier present")
		return result
	}

	// Check forbidden paths
	if f.redactor.HasForbiddenPath(content) {
		result.Allowed = false
		result.Sanitized = "***FORBIDDEN_PATH***"
		result.Warnings = append(result.Warnings, "forbidden path present")
		return result
	}

	// Apply redaction
	redacted := f.redactor.Redact(content)
	result.Sanitized = redacted.Sanitized
	result.Redactions = redacted.Redactions

	// Apply pseudonymization for masked/abstract modes
	if f.policy.Mode == ModeMasked || f.policy.Mode == ModeAbstract {
		result.Sanitized = f.pseudos.PseudonymizeText(result.Sanitized)
	}

	// Apply max context tokens
	if f.policy.MaxContextTokens > 0 {
		tokens := strings.Fields(result.Sanitized)
		if len(tokens) > f.policy.MaxContextTokens {
			result.Sanitized = strings.Join(tokens[:f.policy.MaxContextTokens], " ") + "..."
			result.Redactions = append(result.Redactions, RedactionEntry{
				Reason: "max context tokens exceeded",
			})
		}
	}

	result.Allowed = true
	return result
}

// Redact applies redaction to content.
func (f *PrivacyFirewall) Redact(content string) *RedactionResult {
	return f.redactor.Redact(content)
}

// Pseudonymize replaces identifiers with stable tokens.
func (f *PrivacyFirewall) Pseudonymize(name string, kind PseudonymKind) string {
	return f.pseudos.Pseudonymize(name, kind)
}

// Abstract replaces source with structural summary.
func (f *PrivacyFirewall) Abstract(content string) *FirewallResult {
	result := &FirewallResult{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Disclosure: f.policy.Disclosure(),
		Risk:       f.policy.Risk(),
	}

	if f.policy.Mode == ModeLocalOnly {
		result.Allowed = false
		result.Sanitized = "***LOCAL_ONLY***"
		return result
	}

	// Abstract mode: keep only structural tokens
	abstracted := f.classifier.pathPattern.ReplaceAllString(content, "***PATH***")
	abstracted = f.classifier.RedactSecrets(abstracted)

	result.Sanitized = abstracted
	result.Allowed = true
	return result
}

// EnforcePolicy applies the full policy to context and returns the result.
func (f *PrivacyFirewall) EnforcePolicy(content string) *FirewallResult {
	return f.SanitizeContext(content)
}

// CalculateDisclosureLevel returns the disclosure level for the current mode.
func (f *PrivacyFirewall) CalculateDisclosureLevel() DisclosureLevel {
	return f.policy.Disclosure()
}

// DetectReconstructionRisk returns the reconstruction risk for the current mode.
func (f *PrivacyFirewall) DetectReconstructionRisk() ReconstructionRisk {
	return f.policy.Risk()
}

// ValidateContext checks if context can be transmitted.
func (f *PrivacyFirewall) ValidateContext(content string) bool {
	result := f.SanitizeContext(content)
	return result.Allowed
}

// AuditTransmission records an outbound transmission for auditing.
func (f *PrivacyFirewall) AuditTransmission(destination, content string) *AuditEntry {
	result := f.SanitizeContext(content)
	return &AuditEntry{
		Destination: destination,
		Allowed:     result.Allowed,
		Disclosure:  result.Disclosure,
		Risk:        result.Risk,
		Warnings:    result.Warnings,
		Redactions:  len(result.Redactions),
		Timestamp:   result.Timestamp,
	}
}

// AuditEntry records a transmission audit.
type AuditEntry struct {
	Destination string             `json:"destination"`
	Allowed     bool               `json:"allowed"`
	Disclosure  DisclosureLevel    `json:"disclosure"`
	Risk        ReconstructionRisk `json:"risk"`
	Warnings    []string           `json:"warnings,omitempty"`
	Redactions  int                `json:"redactions"`
	Timestamp   string             `json:"timestamp"`
}

// GetPolicy returns the current policy.
func (f *PrivacyFirewall) GetPolicy() *PrivacyPolicy {
	return f.policy
}

// GetPseudonyms returns the current pseudonym mappings.
func (f *PrivacyFirewall) GetPseudonyms() map[string]string {
	return f.pseudos.ResolveAll()
}
