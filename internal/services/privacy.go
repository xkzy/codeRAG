package services

import (
	"encoding/json"
	"sync"

	"codergag/internal/cache"
	"codergag/internal/graph"
	"codergag/internal/privacy"
)

const privacyStateKind = "PrivacyState"

// privacyState is the per-project policy and pseudonym table.
type privacyState struct {
	policy  *privacy.PrivacyPolicy
	pseudos *privacy.Pseudonymizer
}

// PrivacyService exposes project-scoped privacy enforcement to the MCP layer.
// Policies and pseudonym tables are persisted as PrivacyState graph nodes so
// pseudonyms stay stable across restarts.
// Optionally uses CacheManager for faster privacy state access.
type PrivacyService struct {
	mu     sync.Mutex
	graph  graph.GraphRepository
	cache  *cache.CacheManager
	states map[string]*privacyState
}

// NewPrivacyService creates a service. g may be nil (state is then in-memory only).
func NewPrivacyService(g graph.GraphRepository) *PrivacyService {
	return &PrivacyService{graph: g, states: map[string]*privacyState{}}
}

// SetCache attaches a CacheManager for privacy state caching.
func (s *PrivacyService) SetCache(cm *cache.CacheManager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = cm
}

// state returns the loaded state for a project, loading from cache/graph on first use.
func (s *PrivacyService) state(projectID string) *privacyState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.states[projectID]; ok {
		return st
	}
	st := &privacyState{policy: privacy.DefaultPolicy(projectID), pseudos: privacy.NewPseudonymizer()}
	loadedFromCache := false

	// Try cache first if available
	if s.cache != nil {
		if policyJSON, err := s.cache.GetPrivacyPolicy(projectID); err == nil && policyJSON != "" {
			var p privacy.PrivacyPolicy
			if json.Unmarshal([]byte(policyJSON), &p) == nil && p.Validate() == nil {
				p.ProjectID = projectID
				st.policy = &p
				loadedFromCache = true
			}
		}
		if snapJSON, err := s.cache.GetPseudonymSnapshot(projectID); err == nil && snapJSON != "" {
			var snap privacy.PseudonymSnapshot
			if json.Unmarshal([]byte(snapJSON), &snap) == nil {
				st.pseudos.Restore(&snap)
				loadedFromCache = true
			}
		}
	}

	// Fall back to graph if cache didn't have it
	if !loadedFromCache && s.graph != nil {
		nodes, err := s.graph.FindNodes(privacyStateKind, map[string]any{"project_id": projectID})
		if err == nil && len(nodes) > 0 {
			props := nodes[0].Properties
			if raw, ok := props["policy"].(string); ok {
				var p privacy.PrivacyPolicy
				if json.Unmarshal([]byte(raw), &p) == nil && p.Validate() == nil {
					p.ProjectID = projectID
					st.policy = &p
				}
			}
			if raw, ok := props["pseudonyms"].(string); ok {
				var snap privacy.PseudonymSnapshot
				if json.Unmarshal([]byte(raw), &snap) == nil {
					st.pseudos.Restore(&snap)
				}
			}
		}
	}

	s.states[projectID] = st
	return st
}

// persist writes the project state to the graph and cache.
func (s *PrivacyService) persist(projectID string, st *privacyState) error {
	if s.graph == nil && s.cache == nil {
		return nil
	}
	pol, err := json.Marshal(st.policy)
	if err != nil {
		return err
	}
	snap, err := json.Marshal(st.pseudos.Snapshot())
	if err != nil {
		return err
	}
	policyJSON := string(pol)
	snapshotJSON := string(snap)

	// Write to graph
	if s.graph != nil {
		_, err = s.graph.UpsertNode(privacyStateKind,
			map[string]any{"project_id": projectID},
			map[string]any{"policy": policyJSON, "pseudonyms": snapshotJSON})
		if err != nil {
			return err
		}
	}

	// Write to cache
	if s.cache != nil {
		if err := s.cache.StorePrivacyPolicy(projectID, policyJSON); err != nil {
			return err
		}
		if err := s.cache.StorePseudonymSnapshot(projectID, snapshotJSON); err != nil {
			return err
		}
	}
	return nil
}

// Policy returns a copy of the project's policy.
func (s *PrivacyService) Policy(projectID string) *privacy.PrivacyPolicy {
	cp := *s.state(projectID).policy
	return &cp
}

// SetPolicy validates, stores and persists the policy for p.ProjectID.
func (s *PrivacyService) SetPolicy(p *privacy.PrivacyPolicy) error {
	if err := p.Validate(); err != nil {
		return err
	}
	st := s.state(p.ProjectID)
	cp := *p
	s.mu.Lock()
	st.policy = &cp
	s.mu.Unlock()
	return s.persist(p.ProjectID, st)
}

// Firewall creates a firewall bound to the project's policy and pseudonym table.
func (s *PrivacyService) Firewall(projectID string) *privacy.PrivacyFirewall {
	return s.FirewallWithPolicy(s.Policy(projectID))
}

// FirewallWithPolicy binds a (possibly per-request) policy to the project's
// pseudonym table without changing the stored policy.
func (s *PrivacyService) FirewallWithPolicy(p *privacy.PrivacyPolicy) *privacy.PrivacyFirewall {
	return privacy.NewFirewallWithPseudonymizer(p, s.state(p.ProjectID).pseudos)
}

// Sanitize runs the full firewall on content.
func (s *PrivacyService) Sanitize(projectID, content string) *privacy.FirewallResult {
	return s.Firewall(projectID).SanitizeContext(content)
}

// Redact runs redaction on content.
func (s *PrivacyService) Redact(projectID, content string) *privacy.RedactionResult {
	return s.Firewall(projectID).Redact(content)
}

// Pseudonymize generates a stable pseudonym and persists the table.
func (s *PrivacyService) Pseudonymize(projectID, name string, kind privacy.PseudonymKind) (string, error) {
	st := s.state(projectID)
	token := st.pseudos.Pseudonymize(name, kind)
	return token, s.persist(projectID, st)
}

// AuditTransmission records an outbound transmission.
func (s *PrivacyService) AuditTransmission(projectID, destination, content string) *privacy.AuditEntry {
	return s.Firewall(projectID).AuditTransmission(destination, content)
}

// ValidateContext checks if content can be transmitted.
func (s *PrivacyService) ValidateContext(projectID, content string) bool {
	return s.Firewall(projectID).ValidateContext(content)
}
