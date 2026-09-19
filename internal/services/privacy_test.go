package services

import (
	"testing"

	"codergag/internal/graph"
	"codergag/internal/privacy"
)

func TestPrivacyPseudonymsPersistAcrossRestart(t *testing.T) {
	g := graph.NewMemoryGraphRepository()
	a := NewPrivacyService(g)
	tok, err := a.Pseudonymize("p1", "Billing", privacy.PseudoType)
	if err != nil || tok != "TYPE_1" {
		t.Fatalf("got %q, %v", tok, err)
	}
	// new service over same graph = restart
	b := NewPrivacyService(g)
	if tok2, _ := b.Pseudonymize("p1", "Billing", privacy.PseudoType); tok2 != tok {
		t.Errorf("token changed after restart: %s", tok2)
	}
	if tok3, _ := b.Pseudonymize("p1", "Other", privacy.PseudoType); tok3 != "TYPE_2" {
		t.Errorf("counter not restored: %s", tok3)
	}
}

func TestPrivacyPolicyPersistsAndIsProjectScoped(t *testing.T) {
	g := graph.NewMemoryGraphRepository()
	a := NewPrivacyService(g)
	p := privacy.DefaultPolicy("p1")
	p.Mode = privacy.ModeMasked
	if err := a.SetPolicy(p); err != nil {
		t.Fatal(err)
	}
	b := NewPrivacyService(g)
	if b.Policy("p1").Mode != privacy.ModeMasked {
		t.Error("policy not restored")
	}
	if b.Policy("p2").Mode != privacy.ModeFull {
		t.Error("policy leaked to another project")
	}
	b.Pseudonymize("p1", "X", privacy.PseudoFunc)
	if tok, _ := b.Pseudonymize("p2", "X", privacy.PseudoFunc); tok != "FUNC_1" {
		t.Errorf("pseudonyms leaked across projects: %s", tok)
	}
}

func TestPrivacyPolicyCopyAndValidation(t *testing.T) {
	s := NewPrivacyService(nil)
	s.Policy("p").Mode = privacy.ModeLocalOnly // mutating the copy must not stick
	if s.Policy("p").Mode != privacy.ModeFull {
		t.Error("Policy must return a copy")
	}
	bad := privacy.DefaultPolicy("p")
	bad.MaxSourceBytes = -5
	if s.SetPolicy(bad) == nil {
		t.Error("invalid policy accepted")
	}
}
