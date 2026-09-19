package privacy

import (
	"errors"
	"strings"
)

// PrivacyMode controls how aggressively context is sanitized before external transmission.
type PrivacyMode string

const (
	ModeFull       PrivacyMode = "FULL"
	ModeMinimal    PrivacyMode = "MINIMAL"
	ModeMasked     PrivacyMode = "MASKED"
	ModeStructural PrivacyMode = "STRUCTURAL"
	ModeAbstract   PrivacyMode = "ABSTRACT"
	ModeLocalOnly  PrivacyMode = "LOCAL_ONLY"
)

// DisclosureLevel ranks how much repository information is exposed.
type DisclosureLevel int

const (
	DisclosureNone          DisclosureLevel = 0 // no repository information
	DisclosureGeneric       DisclosureLevel = 1 // generic structure
	DisclosurePseudonymized DisclosureLevel = 2 // pseudonymized structure
	DisclosureSemantic      DisclosureLevel = 3 // limited semantic information
	DisclosureSource        DisclosureLevel = 4 // selected source information
	DisclosureUnrestricted  DisclosureLevel = 5 // unrestricted selected context
)

// ReconstructionRisk rates how likely sensitive info can be reconstructed.
type ReconstructionRisk string

const (
	RiskLow    ReconstructionRisk = "LOW"
	RiskMedium ReconstructionRisk = "MEDIUM"
	RiskHigh   ReconstructionRisk = "HIGH"
)

// ModeDisclosure maps a privacy mode to its disclosure level.
var ModeDisclosure = map[PrivacyMode]DisclosureLevel{
	ModeFull:       DisclosureUnrestricted,
	ModeMinimal:    DisclosureSemantic,
	ModeMasked:     DisclosurePseudonymized,
	ModeStructural: DisclosureGeneric,
	ModeAbstract:   DisclosurePseudonymized,
	ModeLocalOnly:  DisclosureNone,
}

// ModeReconstructionRisk maps a privacy mode to its reconstruction risk.
var ModeReconstructionRisk = map[PrivacyMode]ReconstructionRisk{
	ModeFull:       RiskHigh,
	ModeMinimal:    RiskMedium,
	ModeMasked:     RiskMedium,
	ModeStructural: RiskLow,
	ModeAbstract:   RiskMedium,
	ModeLocalOnly:  RiskLow,
}

// ParseMode parses a privacy mode string.
func ParseMode(s string) (PrivacyMode, error) {
	switch strings.ToLower(s) {
	case "", "full":
		return ModeFull, nil
	case "minimal":
		return ModeMinimal, nil
	case "masked":
		return ModeMasked, nil
	case "structural":
		return ModeStructural, nil
	case "abstract":
		return ModeAbstract, nil
	case "local_only", "local-only", "localonly":
		return ModeLocalOnly, nil
	default:
		return "", errors.New("unknown privacy mode: " + s)
	}
}

// PseudonymKind classifies identifiers for pseudonymization.
type PseudonymKind string

const (
	PseudoFunc    PseudonymKind = "FUNC"
	PseudoClass   PseudonymKind = "CLASS"
	PseudoStruct  PseudonymKind = "STRUCT"
	PseudoVar     PseudonymKind = "VAR"
	PseudoField   PseudonymKind = "FIELD"
	PseudoModule  PseudonymKind = "MODULE"
	PseudoType    PseudonymKind = "TYPE"
	PseudoUnknown PseudonymKind = "UNKNOWN"
)

// ContentKind classifies content for filtering.
type ContentKind string

const (
	ContentSource   ContentKind = "source"
	ContentStruct   ContentKind = "structural"
	ContentSemantic ContentKind = "semantic"
	ContentDoc      ContentKind = "doc"
	ContentMemory   ContentKind = "memory"
	ContentBinary   ContentKind = "binary"
	ContentString   ContentKind = "string"
	ContentPath     ContentKind = "path"
	ContentComment  ContentKind = "comment"
	ContentLiteral  ContentKind = "literal"
)
