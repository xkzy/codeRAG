package services

import (
	"strings"
	"testing"
	"time"
)

// Extractors run on untrusted repositories: hostile input must neither panic nor stall.
func TestExtractorsDontHangOrPanicOnHostileInput(t *testing.T) {
	// One extension per extractor: extensions of the same spec share every code path.
	exts := []string{}
	for _, sp := range langSpecs {
		for _, e := range sp.exts {
			if languageFor(e) == nil { // tree-sitter languages are covered by TestTreeSitterParseIsBounded
				exts = append(exts, e)
				break
			}
		}
	}
	adversarial := []string{
		"",
		strings.Repeat("(", 25000),
		strings.Repeat("a", 125000),
		strings.Repeat("func f(", 12000) + strings.Repeat(")", 12000),
		strings.Repeat("\n", 50000),
		strings.Repeat(".globl x\nx:\n", 12000),
		strings.Repeat("call ", 25000) + "x",
		"\x00\x01\xff\xfe" + strings.Repeat("def f():\n", 5000),
		strings.Repeat("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa!", 2000), // catastrophic-backtracking probe
	}
	for _, ext := range exts {
		for i, src := range adversarial {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("%s case %d panicked: %v", ext, i, r)
					}
				}()
				done := make(chan struct{})
				go func() {
					extractFunctionInfos(src, ext)
					extractTypes(src, ext)
					extractImports(src, ext)
					close(done)
				}()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Errorf("%s case %d timed out (possible ReDoS)", ext, i)
				}
			}()
		}
	}
}

func TestExtractorsHandleInvalidUTF8(t *testing.T) {
	bad := "func f(\xff\xfe) {}\n\x00TEXT \xc3\x28(SB)\n"
	for ext := range supportedExts {
		extractFunctionInfos(bad, ext)
		extractTypes(bad, ext)
		extractImports(bad, ext)
	}
}
