package privacy

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PrivacyPolicy is a project-scoped configuration for outbound context.
type PrivacyPolicy struct {
	ProjectID            string             `json:"project_id"`
	Mode                 PrivacyMode        `json:"mode"`
	AllowedIdentifiers   []string           `json:"allowed_identifiers,omitempty"`
	ForbiddenIdentifiers []string           `json:"forbidden_identifiers,omitempty"`
	AllowedPaths         []string           `json:"allowed_paths,omitempty"`
	ForbiddenPaths       []string           `json:"forbidden_paths,omitempty"`
	AllowedLiterals      []string           `json:"allowed_literals,omitempty"`
	ForbiddenLiterals    []string           `json:"forbidden_literals,omitempty"`
	AllowComments        bool               `json:"allow_comments"`
	AllowGitHistory      bool               `json:"allow_git_history"`
	AllowStrings         bool               `json:"allow_strings"`
	AllowBinaryStrings   bool               `json:"allow_binary_strings"`
	AllowExactSource     bool               `json:"allow_exact_source"`
	MaxSourceBytes       int                `json:"max_source_bytes"`
	MaxContextTokens     int                `json:"max_context_tokens"`
	ReconstructionRisk   ReconstructionRisk `json:"reconstruction_risk"`
	ProviderPolicy       *ProviderPolicy    `json:"provider_policy,omitempty"`
	CreatedAt            string             `json:"created_at"`
	UpdatedAt            string             `json:"updated_at"`
}

// ProviderPolicy holds per-provider privacy settings.
type ProviderPolicy struct {
	Name                 string        `json:"name"`
	MaxTokens            int           `json:"max_tokens"`
	AllowedModes         []PrivacyMode `json:"allowed_modes"`
	RequiresSanitization bool          `json:"requires_sanitization"`
}

// DefaultPolicy returns a policy with safe defaults.
func DefaultPolicy(projectID string) *PrivacyPolicy {
	return &PrivacyPolicy{
		ProjectID:          projectID,
		Mode:               ModeFull,
		AllowComments:      false,
		AllowGitHistory:    false,
		AllowStrings:       false,
		AllowBinaryStrings: false,
		AllowExactSource:   false,
		MaxSourceBytes:     512,
		MaxContextTokens:   4096,
		ReconstructionRisk: RiskHigh,
		CreatedAt:          time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:          time.Now().UTC().Format(time.RFC3339),
	}
}

// Validate checks the policy for consistency.
func (p *PrivacyPolicy) Validate() error {
	if _, err := ParseMode(string(p.Mode)); err != nil {
		return err
	}
	if p.MaxSourceBytes < 0 {
		return fmt.Errorf("max_source_bytes must be >= 0")
	}
	if p.MaxContextTokens < 0 {
		return fmt.Errorf("max_context_tokens must be >= 0")
	}
	return nil
}

// AllowsSource returns whether exact source may be transmitted.
func (p *PrivacyPolicy) AllowsSource() bool {
	return p.AllowExactSource && p.Mode != ModeLocalOnly
}

// AllowsStrings returns whether string literals may be transmitted.
func (p *PrivacyPolicy) AllowsStrings() bool {
	return p.AllowStrings && p.Mode != ModeLocalOnly
}

// IsIdentifierAllowed returns true if the identifier is explicitly allowed.
func (p *PrivacyPolicy) IsIdentifierAllowed(id string) bool {
	for _, a := range p.AllowedIdentifiers {
		if a == id {
			return true
		}
	}
	return false
}

// IsIdentifierForbidden returns true if the identifier is forbidden.
func (p *PrivacyPolicy) IsIdentifierForbidden(id string) bool {
	for _, f := range p.ForbiddenIdentifiers {
		if f == id {
			return true
		}
	}
	return false
}

// IsPathAllowed returns true if the path is explicitly allowed.
func (p *PrivacyPolicy) IsPathAllowed(path string) bool {
	for _, a := range p.AllowedPaths {
		if strings.HasPrefix(path, a) {
			return true
		}
	}
	return false
}

// IsPathForbidden returns true if the path is forbidden.
func (p *PrivacyPolicy) IsPathForbidden(path string) bool {
	for _, f := range p.ForbiddenPaths {
		if strings.HasPrefix(path, f) {
			return true
		}
	}
	return false
}

// IsLiteralAllowed returns true if the literal is explicitly allowed.
func (p *PrivacyPolicy) IsLiteralAllowed(lit string) bool {
	for _, a := range p.AllowedLiterals {
		if a == lit {
			return true
		}
	}
	return false
}

// IsLiteralForbidden returns true if the literal is forbidden.
func (p *PrivacyPolicy) IsLiteralForbidden(lit string) bool {
	for _, f := range p.ForbiddenLiterals {
		if f == lit {
			return true
		}
	}
	return false
}

// Disclosure returns the disclosure level for this policy.
func (p *PrivacyPolicy) Disclosure() DisclosureLevel {
	return ModeDisclosure[p.Mode]
}

// Risk returns the reconstruction risk for this policy.
func (p *PrivacyPolicy) Risk() ReconstructionRisk {
	if p.ReconstructionRisk != "" {
		return p.ReconstructionRisk
	}
	return ModeReconstructionRisk[p.Mode]
}

// MarshalJSON implements custom marshaling.
func (p *PrivacyPolicy) MarshalJSON() ([]byte, error) {
	type alias PrivacyPolicy
	return json.Marshal((*alias)(p))
}

// UnmarshalJSON implements custom unmarshaling.
func (p *PrivacyPolicy) UnmarshalJSON(data []byte) error {
	type alias PrivacyPolicy
	var tmp alias
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	*p = PrivacyPolicy(tmp)
	return nil
}
