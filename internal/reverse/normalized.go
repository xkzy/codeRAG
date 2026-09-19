package reverse

type NormalizedFunction struct {
	Address           string
	Name              string
	Size              *int
	Calls             []string
	Strings           []string
	DecompilerOutput  string
	BasicBlocks       []NormalizedBasicBlock
	DataReferences    []NormalizedDataRef    // Data cross-references
	CodeReferences    []NormalizedCodeRef    // Code cross-references
}

type NormalizedBasicBlock struct {
	Address     string
	Instruction []NormalizedInstruction
}

type NormalizedInstruction struct {
	Address      string
	Mnemonic     string
	Operands     string
	DataRefs     []NormalizedDataRef    // Data references from this instruction
	CodeRefs     []NormalizedCodeRef    // Code references from this instruction
}

type NormalizedDataRef struct {
	FromAddress string // Address of referencing instruction
	ToAddress   string // Address of referenced data
	Type        string // "read", "write", "offset"
	Size        int    // Size of data access
}

type NormalizedCodeRef struct {
	FromAddress string // Address of referencing instruction
	ToAddress   string // Address of referenced code
	Type        string // "call", "jmp", "cond_jmp"
}

type NormalizedBinary struct {
	BinaryID  string
	Path      string
	Sha256    string
	Tool      string
	Functions []NormalizedFunction
}

// RuntimeTrace represents a runtime execution trace.
type RuntimeTrace struct {
	BinaryID      string
	FunctionAddr  string
	TraceID       string
	Timestamp     int64
	Instructions  []TraceInstruction
	Registers     map[string]string
	MemoryReads   []TraceMemoryAccess
	MemoryWrites  []TraceMemoryAccess
}

type TraceInstruction struct {
	Address  string
	Mnemonic string
	Operands string
	Registers map[string]string // Register state after instruction
}

type TraceMemoryAccess struct {
	Address string
	Size    int
	Value   string
}

// Hypothesis represents a competing hypothesis in reverse engineering.
type Hypothesis struct {
	ID          string
	SubjectID   string // Binary function or address
	Claim       string
	EvidenceFor []EvidenceRef
	EvidenceAgainst []EvidenceRef
	Confidence  float64
	Status      string // "active", "refuted", "confirmed"
	CreatedAt   int64
	UpdatedAt   int64
	Analyst     string
}

type EvidenceRef struct {
	ID          string
	Description string
	Confidence  float64
	Kind        string // "static", "dynamic", "manual"
}

// BehavioralEquivalence represents a comparison between binary and source.
type BehavioralEquivalence struct {
	BinaryFunctionID   string
	SourceFunctionID   string
	EquivalenceStatus  string // "equivalent", "divergent", "unknown"
	Confidence         float64
	Method             string // "symbolic", "concolic", "differential", "manual"
	TestCases          []EquivalenceTestCase
	Differences        []EquivalenceDifference
	CreatedAt          int64
	Analyst            string
}

type EquivalenceTestCase struct {
	Input      map[string]any
	BinaryOut  map[string]any
	SourceOut  map[string]any
	Match      bool
}

type EquivalenceDifference struct {
	Type        string // "control_flow", "data_flow", "side_effect", "timing"
	Description string
	Severity    string // "critical", "major", "minor"
}