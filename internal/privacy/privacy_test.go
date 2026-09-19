package privacy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseMode(t *testing.T) {
	cases := map[string]PrivacyMode{
		"": ModeFull, "full": ModeFull, "minimal": ModeMinimal, "masked": ModeMasked,
		"structural": ModeStructural, "abstract": ModeAbstract,
		"local_only": ModeLocalOnly, "local-only": ModeLocalOnly,
	}
	for in, want := range cases {
		got, err := ParseMode(in)
		if err != nil || got != want {
			t.Errorf("ParseMode(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := ParseMode("bogus"); err == nil {
		t.Error("expected error for unknown mode")
	}
}

func TestModeMappingsComplete(t *testing.T) {
	for _, m := range []PrivacyMode{ModeFull, ModeMinimal, ModeMasked, ModeStructural, ModeAbstract, ModeLocalOnly} {
		if _, ok := ModeDisclosure[m]; !ok {
			t.Errorf("missing disclosure for %s", m)
		}
		if _, ok := ModeReconstructionRisk[m]; !ok {
			t.Errorf("missing risk for %s", m)
		}
	}
	if ModeDisclosure[ModeLocalOnly] != DisclosureNone {
		t.Error("LOCAL_ONLY must disclose nothing")
	}
}

func TestPolicyDefaultsAndValidate(t *testing.T) {
	p := DefaultPolicy("proj")
	if p.ProjectID != "proj" || p.MaxSourceBytes != 512 || p.MaxContextTokens != 4096 {
		t.Fatalf("unexpected defaults: %+v", p)
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	p.MaxSourceBytes = -1
	if p.Validate() == nil {
		t.Error("negative max_source_bytes should fail")
	}
	p.MaxSourceBytes = 1
	p.Mode = "NOPE"
	if p.Validate() == nil {
		t.Error("bad mode should fail")
	}
}

func TestPolicyLists(t *testing.T) {
	p := DefaultPolicy("p")
	p.AllowedIdentifiers = []string{"ok"}
	p.ForbiddenIdentifiers = []string{"secretFn"}
	p.AllowedPaths = []string{"/src/"}
	p.ForbiddenPaths = []string{"/etc/"}
	p.AllowedLiterals = []string{"a"}
	p.ForbiddenLiterals = []string{"b"}
	if !p.IsIdentifierAllowed("ok") || p.IsIdentifierAllowed("x") {
		t.Error("identifier allow")
	}
	if !p.IsIdentifierForbidden("secretFn") || p.IsIdentifierForbidden("ok") {
		t.Error("identifier forbid")
	}
	if !p.IsPathAllowed("/src/a.go") || p.IsPathAllowed("/other") {
		t.Error("path allow")
	}
	if !p.IsPathForbidden("/etc/passwd") || p.IsPathForbidden("/src/a") {
		t.Error("path forbid")
	}
	if !p.IsLiteralAllowed("a") || !p.IsLiteralForbidden("b") || p.IsLiteralForbidden("a") {
		t.Error("literals")
	}
}

func TestPolicyAllowsAndJSON(t *testing.T) {
	p := DefaultPolicy("p")
	if p.AllowsSource() || p.AllowsStrings() {
		t.Error("defaults must deny source and strings")
	}
	p.AllowExactSource, p.AllowStrings = true, true
	if !p.AllowsSource() || !p.AllowsStrings() {
		t.Error("expected allowed")
	}
	p.Mode = ModeLocalOnly
	if p.AllowsSource() || p.AllowsStrings() {
		t.Error("LOCAL_ONLY overrides allow flags")
	}
	p.Mode = ModeMasked
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var q PrivacyPolicy
	if err := json.Unmarshal(b, &q); err != nil {
		t.Fatal(err)
	}
	if q.Mode != ModeMasked || q.ProjectID != "p" || !q.AllowExactSource {
		t.Errorf("round trip mismatch: %+v", q)
	}
}

func TestPolicyRiskFallback(t *testing.T) {
	p := &PrivacyPolicy{Mode: ModeStructural}
	if p.Risk() != RiskLow {
		t.Errorf("got %s", p.Risk())
	}
	p.ReconstructionRisk = RiskHigh
	if p.Risk() != RiskHigh {
		t.Error("explicit risk should win")
	}
}

func TestClassifierSecrets(t *testing.T) {
	c := NewContentClassifier()
	secrets := []string{
		`api_key = "abcdefghijklmnop1234"`,
		`password: hunter2hunter2`,
		`token=abcdefghijklmnopqrstuv`,
		"-----BEGIN RSA PRIVATE KEY-----",
		`postgres://user:pw@host/db`,
		`ghp_abcdefghijklmnopqrstuvwxyz`,
		`eyJhbGciOiJIUzI1.eyJzdWIiOiIxMjM0.SflKxwRJSMeKKF2QT4`,
	}
	for _, s := range secrets {
		if !c.IsSecret(s) {
			t.Errorf("not detected: %q", s)
		}
		if out := c.RedactSecrets(s); out == s {
			t.Errorf("not redacted: %q", s)
		}
	}
	if c.IsSecret("func add(a, b int) int { return a + b }") {
		t.Error("false positive")
	}
}

func TestClassifierRedactKeepsKey(t *testing.T) {
	c := NewContentClassifier()
	out := c.RedactSecrets(`api_key = "abcdefghijklmnop1234"`)
	if !strings.Contains(out, "api_key") || !strings.Contains(out, "***REDACTED***") || strings.Contains(out, "abcdefghijklmnop1234") {
		t.Errorf("got %q", out)
	}
}

func TestClassifyContent(t *testing.T) {
	c := NewContentClassifier()
	cases := map[string]ContentKind{
		`password=supersecret1`: ContentLiteral,
		`/home/alice/x.go`:      ContentPath,
		`// a comment`:          ContentComment,
		`"hello"`:               ContentString,
		`x := 1`:                ContentSource,
	}
	for in, want := range cases {
		if got := c.ClassifyContent(in); got != want {
			t.Errorf("ClassifyContent(%q)=%s want %s", in, got, want)
		}
	}
}

func TestClassifySymbol(t *testing.T) {
	cases := map[string]PseudonymKind{
		"pkg.Fn": PseudoModule, "a::b": PseudoModule, "func main": PseudoFunc,
		"class Foo": PseudoType, "var x": PseudoVar, "m_count": PseudoField, "plain": PseudoUnknown,
	}
	for in, want := range cases {
		if got := ClassifySymbol(in); got != want {
			t.Errorf("ClassifySymbol(%q)=%s want %s", in, got, want)
		}
	}
}

func TestPseudonymizerStable(t *testing.T) {
	p := NewPseudonymizer()
	a := p.Pseudonymize("Login", PseudoFunc)
	if a != "FUNC_1" || p.Pseudonymize("Login", PseudoFunc) != a {
		t.Fatalf("not stable: %s", a)
	}
	if b := p.Pseudonymize("User", PseudoType); b != "TYPE_1" {
		t.Errorf("got %s", b)
	}
	if c := p.Pseudonymize("Logout", PseudoFunc); c != "FUNC_2" {
		t.Errorf("got %s", c)
	}
	if n, ok := p.Resolve(a); !ok || n != "Login" {
		t.Error("resolve")
	}
	if _, ok := p.Resolve("nope"); ok {
		t.Error("resolve unknown")
	}
	if len(p.ResolveAll()) != 3 {
		t.Error("ResolveAll size")
	}
}

func TestPseudonymizeTextLongestFirst(t *testing.T) {
	p := NewPseudonymizer()
	short := p.Pseudonymize("get", PseudoFunc)
	long := p.Pseudonymize("getUser", PseudoFunc)
	out := p.PseudonymizeText("getUser() then get()")
	if out != long+"() then "+short+"()" {
		t.Errorf("got %q", out)
	}
}

func TestPseudonymizerSnapshotRestore(t *testing.T) {
	p := NewPseudonymizer()
	p.Pseudonymize("Alpha", PseudoFunc)
	snap := p.Snapshot()
	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var back PseudonymSnapshot
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	q := NewPseudonymizer()
	q.Restore(&back)
	if q.Pseudonymize("Alpha", PseudoFunc) != "FUNC_1" {
		t.Error("restored mapping lost")
	}
	if got := q.Pseudonymize("Beta", PseudoFunc); got != "FUNC_2" {
		t.Errorf("counter not restored: %s", got)
	}
	// snapshot must be a copy
	snap.NameToToken["Alpha"] = "X"
	if p.Pseudonymize("Alpha", PseudoFunc) != "FUNC_1" {
		t.Error("snapshot aliases internal state")
	}
}

func TestRedactorLocalOnly(t *testing.T) {
	p := DefaultPolicy("p")
	p.Mode = ModeLocalOnly
	r := NewRedactor(p)
	if res := r.Redact("anything"); res.Sanitized != "***LOCAL_ONLY***" {
		t.Errorf("got %q", res.Sanitized)
	}
	if r.IsAllowed("anything") {
		t.Error("LOCAL_ONLY must not allow")
	}
}

func TestRedactorSecretsPathsAndTruncation(t *testing.T) {
	p := DefaultPolicy("p")
	r := NewRedactor(p)
	res := r.Redact(`api_key = "abcdefghijklmnop1234" at /home/bob/x`)
	if strings.Contains(res.Sanitized, "abcdefghijklmnop1234") || strings.Contains(res.Sanitized, "/home/bob") {
		t.Errorf("leak: %q", res.Sanitized)
	}
	p.MaxSourceBytes = 10
	res = r.Redact(strings.Repeat("a", 100))
	if len(res.Sanitized) != 13 || !strings.HasSuffix(res.Sanitized, "...") {
		t.Errorf("truncation: %q", res.Sanitized)
	}
}

func TestRedactSourceRequiresPolicy(t *testing.T) {
	p := DefaultPolicy("p")
	r := NewRedactor(p)
	ps := NewPseudonymizer()
	if res := r.RedactSource("x := 1", ps); res.Sanitized != "***SOURCE_NOT_ALLOWED***" {
		t.Errorf("got %q", res.Sanitized)
	}
	p.AllowExactSource = true
	p.Mode = ModeMasked
	ps.Pseudonymize("Secret", PseudoFunc)
	if res := r.RedactSource("Secret()", ps); res.Sanitized != "FUNC_1()" {
		t.Errorf("got %q", res.Sanitized)
	}
}

func TestRedactorHasChecks(t *testing.T) {
	p := DefaultPolicy("p")
	p.ForbiddenIdentifiers = []string{"topSecret"}
	p.ForbiddenPaths = []string{"/etc/"}
	r := NewRedactor(p)
	if !r.HasSecrets("password=abcdefgh1") || r.HasSecrets("plain") {
		t.Error("HasSecrets")
	}
	if !r.HasForbiddenIdentifier("call topSecret()") || r.HasForbiddenIdentifier("ok") {
		t.Error("HasForbiddenIdentifier")
	}
	if !r.HasForbiddenPath("/etc/passwd") || r.HasForbiddenPath("/src/x") {
		t.Error("HasForbiddenPath")
	}
}

func TestFirewallModes(t *testing.T) {
	p := DefaultPolicy("p")
	p.Mode = ModeLocalOnly
	fw := NewFirewall(p)
	if res := fw.SanitizeContext("x"); res.Allowed || res.Disclosure != DisclosureNone {
		t.Errorf("LOCAL_ONLY: %+v", res)
	}

	p2 := DefaultPolicy("p")
	fw = NewFirewall(p2)
	res := fw.SanitizeContext(`password=hunter2hunter2`)
	if res.Allowed || strings.Contains(res.Sanitized, "hunter2hunter2") {
		t.Errorf("secret must block and redact: %+v", res)
	}

	p2.ForbiddenIdentifiers = []string{"topSecret"}
	if res := fw.SanitizeContext("call topSecret()"); res.Allowed {
		t.Error("forbidden identifier must block")
	}
	p2.ForbiddenPaths = []string{"/etc/"}
	if res := fw.SanitizeContext("/etc/passwd"); res.Allowed {
		t.Error("forbidden path must block")
	}
}

func TestFirewallMaskedPseudonymizes(t *testing.T) {
	p := DefaultPolicy("p")
	p.Mode = ModeMasked
	fw := NewFirewall(p)
	tok := fw.Pseudonymize("BillingEngine", PseudoType)
	res := fw.SanitizeContext("use BillingEngine here")
	if !res.Allowed || strings.Contains(res.Sanitized, "BillingEngine") || !strings.Contains(res.Sanitized, tok) {
		t.Errorf("got %+v", res)
	}
	if fw.GetPseudonyms()[tok] != "BillingEngine" {
		t.Error("GetPseudonyms")
	}
}

func TestFirewallTokenLimit(t *testing.T) {
	p := DefaultPolicy("p")
	p.MaxContextTokens = 3
	fw := NewFirewall(p)
	res := fw.SanitizeContext("a b c d e f")
	if !res.Allowed || res.Sanitized != "a b c..." {
		t.Errorf("got %q", res.Sanitized)
	}
}

func TestFirewallAbstractAuditValidate(t *testing.T) {
	p := DefaultPolicy("p")
	fw := NewFirewall(p)
	res := fw.Abstract(`see /home/a/b and password=hunter2hunter2`)
	if !res.Allowed || strings.Contains(res.Sanitized, "/home/a") || strings.Contains(res.Sanitized, "hunter2hunter2") {
		t.Errorf("got %q", res.Sanitized)
	}
	if !fw.ValidateContext("clean") || fw.ValidateContext("password=hunter2hunter2") {
		t.Error("ValidateContext")
	}
	a := fw.AuditTransmission("provider-x", "password=hunter2hunter2")
	if a.Allowed || a.Destination != "provider-x" || len(a.Warnings) == 0 {
		t.Errorf("audit: %+v", a)
	}
	if fw.CalculateDisclosureLevel() != DisclosureUnrestricted || fw.DetectReconstructionRisk() != RiskHigh {
		t.Error("level/risk")
	}
	if fw.GetPolicy() != p {
		t.Error("GetPolicy")
	}
}

func TestRedactNoSpuriousPathEntry(t *testing.T) {
	r := NewRedactor(DefaultPolicy("p"))
	if res := r.Redact("x := 1"); len(res.Redactions) != 0 {
		t.Errorf("unexpected redactions: %+v", res.Redactions)
	}
}
