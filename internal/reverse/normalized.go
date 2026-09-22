package reverse

type NormalizedFunction struct {
	Address          string
	Name             string
	Size             *int
	Calls            []string
	Strings          []string
	DecompilerOutput string
	BasicBlocks      []NormalizedBasicBlock
	DataReferences   []NormalizedDataRef // Data cross-references
	CodeReferences   []NormalizedCodeRef // Code cross-references
}

type NormalizedBasicBlock struct {
	Address     string
	Instruction []NormalizedInstruction
}

type NormalizedInstruction struct {
	Address  string
	Mnemonic string
	Operands string
	DataRefs []NormalizedDataRef // Data references from this instruction
	CodeRefs []NormalizedCodeRef // Code references from this instruction
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
	Modules   []NormalizedModule
	Sections  []NormalizedSection
	Globals   []NormalizedGlobal
	Imports   []NormalizedImport
	Exports   []NormalizedExport
	Symbols   []NormalizedSymbol
	Types     []NormalizedType
	Registers []NormalizedRegister
	Constants []NormalizedConstant
	ExternalAPIs []NormalizedExternalAPI
}

type NormalizedModule struct {
	Name    string
	Path    string
}

type NormalizedSection struct {
	Name      string
	Address   string
	Size      int
	Alignment int
}

type NormalizedGlobal struct {
	Address string
	Name    string
	Size    int
}

type NormalizedImport struct {
	Name     string
	Ordinal  int
	Hint     string
}

type NormalizedExport struct {
	Name     string
	Address  string
	Ordinal  int
}

type NormalizedSymbol struct {
	Address string
	Name    string
	Binding string
	Type    string
}

type NormalizedType struct {
	Name   string
	Size   int
	Fields []TypeField
}

type TypeField struct {
	Name   string
	Offset int
	Type   string
	Size   int
}

type NormalizedRegister struct {
	Name       string
	Role       string
	WidthBits  int
}

type NormalizedConstant struct {
	Value    string
	Type     string
	Address  string
}

type NormalizedExternalAPI struct {
	Name         string
	Module       string
	ReturnType   string
	ArgumentTypes []string
}

// RuntimeTrace represents a runtime execution trace.
type RuntimeTrace struct {
	BinaryID     string
	FunctionAddr string
	TraceID      string
	Timestamp    int64
	Instructions []TraceInstruction
	Registers    map[string]string
	MemoryReads  []TraceMemoryAccess
	MemoryWrites []TraceMemoryAccess
}

type TraceInstruction struct {
	Address   string
	Mnemonic  string
	Operands  string
	Registers map[string]string // Register state after instruction
}

type TraceMemoryAccess struct {
	Address string
	Size    int
	Value   string
}

// Hypothesis represents a competing hypothesis in reverse engineering.
type Hypothesis struct {
	ID              string
	SubjectID       string // Binary function or address
	Claim           string
	EvidenceFor     []EvidenceRef
	EvidenceAgainst []EvidenceRef
	Confidence      float64
	Status          string // "active", "refuted", "confirmed"
	CreatedAt       int64
	UpdatedAt       int64
	Analyst         string
}

type EvidenceRef struct {
	ID          string
	Description string
	Confidence  float64
	Kind        string // "static", "dynamic", "manual"
}

// BehavioralEquivalence represents a comparison between binary and source.
type BehavioralEquivalence struct {
	BinaryFunctionID  string
	SourceFunctionID  string
	EquivalenceStatus string // "equivalent", "divergent", "unknown"
	Confidence        float64
	Method            string // "symbolic", "concolic", "differential", "manual"
	TestCases         []EquivalenceTestCase
	Differences       []EquivalenceDifference
	CreatedAt         int64
	Analyst           string
}

type EquivalenceTestCase struct {
	Input     map[string]any
	BinaryOut map[string]any
	SourceOut map[string]any
	Match     bool
}

type EquivalenceDifference struct {
	Type        string // "control_flow", "data_flow", "side_effect", "timing"
	Description string
	Severity    string // "critical", "major", "minor"
}

// ObservationSource identifies the kind of process/tool that produced output.
type ObservationSource string

const (
	SourceAgent        ObservationSource = "AGENT"
	SourceProcess      ObservationSource = "PROCESS"
	SourceApplication   ObservationSource = "APPLICATION"
	SourceCompiler     ObservationSource = "COMPILER"
	SourceTest         ObservationSource = "TEST"
	SourceDebugger     ObservationSource = "DEBUGGER"
	SourceGDB          ObservationSource = "GDB"
	SourceLLDB         ObservationSource = "LLDB"
	SourceGhidra       ObservationSource = "GHIDRA"
	SourceIDA          ObservationSource = "IDA"
	SourceBinaryNinja   ObservationSource = "BINARY_NINJA"
	SourceObjdump      ObservationSource = "OBJDUMP"
	SourceReadelf      ObservationSource = "READelf"
)

// ObservationStream is the channel the raw text arrived on.
type ObservationStream string

const (
	StreamStdout ObservationStream = "stdout"
	StreamStderr ObservationStream = "stderr"
	StreamLog   ObservationStream = "log"
	StreamBuild ObservationStream = "build"
	StreamDebug ObservationStream = "debug"
)

// Severity ranks observations for processing priority.
type Severity string

const (
	SevP0 Severity = "P0" // crash / fatal / exception / assertion / segfault / test failure
	SevP1 Severity = "P1" // error / compiler error / linker error / debugger breakpoint / failed validation
	SevP2 Severity = "P2" // warning / performance anomaly / unexpected state
	SevP3 Severity = "P3" // normal informational logs
	SevP4 Severity = "P4" // high-volume repetitive logs
)

// EventType classifies the observation for downstream routing.
type EventType string

const (
	EventTypeCrash          EventType = "CRASH"
	EventTypeException      EventType = "EXCEPTION"
	EventTypeAssertion      EventType = "ASSERTION"
	EventTypeTestFailure    EventType = "TEST_FAILURE"
	EventTypeCompilerDiag   EventType = "COMPILER_DIAG"
	EventTypeLinkerDiag     EventType = "LINKER_DIAG"
	EventTypeRuntimeError   EventType = "RUNTIME_ERROR"
	EventTypeDebuggerBreak EventType = "DEBUGGER_BREAK"
	EventTypeDecompilerOut  EventType = "DECOMPILER_OUTPUT"
	EventTypeInfo           EventType = "INFO"
	EventTypeAgentEvent     EventType = "AGENT_EVENT"
)

// EvidenceLevel records the provenance strength of an observation-to-code relation.
type EvidenceLevel string

const (
	EvidenceDirect    EvidenceLevel = "DIRECT"
	EvidenceStrong    EvidenceLevel = "STRONG"
	EvidenceInferred  EvidenceLevel = "INFERRED"
	EvidenceHypothesis EvidenceLevel = "HYPOTHESIS"
	EvidenceUnresolved EvidenceLevel = "UNRESOLVED"
)

// ExtractedFields holds the deterministic information parsed from raw output.
type ExtractedFields struct {
	File       string            `json:"file,omitempty"`
	Line       int               `json:"line,omitempty"`
	Column     int               `json:"column,omitempty"`
	Symbol     string            `json:"symbol,omitempty"`
	Address    string            `json:"address,omitempty"`
	Offset     string            `json:"offset,omitempty"`
	Module     string            `json:"module,omitempty"`
	Exception  string            `json:"exception,omitempty"`
	ErrorCode  string            `json:"error_code,omitempty"`
	TestName   string            `json:"test_name,omitempty"`
	FuncName   string            `json:"func_name,omitempty"`
	Class      string            `json:"class,omitempty"`
	Struct     string            `json:"struct,omitempty"`
	Stack      []string          `json:"stack,omitempty"`
	Fields     map[string]string `json:"fields,omitempty"`
}

// RuntimeObservation is the normalized record produced by the interceptor.
type RuntimeObservation struct {
	ID               string            `json:"id"`
	ProjectID        string            `json:"project_id"`
	SessionID        string            `json:"session_id"`
	ProcessID        string            `json:"process_id"`
	Source           ObservationSource `json:"source"`
	Timestamp        string            `json:"timestamp"`
	Stream           ObservationStream `json:"stream"`
	RawHash          string            `json:"raw_hash"`
	NormalizedText   string            `json:"normalized_text"`
	Severity         Severity          `json:"severity"`
	EventType       EventType         `json:"event_type"`
	Extracted        ExtractedFields   `json:"extracted_fields"`
	WorkingDirectory string            `json:"working_directory,omitempty"`
	Executable       string            `json:"executable,omitempty"`
	BinaryID         string            `json:"binary_id,omitempty"`
	GitCommit        string            `json:"git_commit,omitempty"`
	GitBranch        string            `json:"git_branch,omitempty"`
	BinaryHash       string            `json:"binary_hash,omitempty"`
	SourceRevision   string            `json:"source_revision,omitempty"`
	GraphVersion     uint64            `json:"graph_version,omitempty"`
	Dropped          bool              `json:"dropped,omitempty"`
	DropReason       string            `json:"drop_reason,omitempty"`
}

// StackFrame is one frame of a parsed stack trace.
type StackFrame struct {
	Index   int    `json:"index"`
	Name    string `json:"name"`
	Address string `json:"address,omitempty"`
	Offset  string `json:"offset,omitempty"`
	Module  string `json:"module,omitempty"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
}

// LogTemplate is a normalized log message with placeholders for variable parts.
type LogTemplate struct {
	ID         string   `json:"id"`
	ProjectID  string   `json:"project_id"`
	Template   string   `json:"template"`
	SourceIDs  []string `json:"source_symbol_ids,omitempty"`
	Count      int     `json:"count"`
	FirstSeen  string  `json:"first_seen"`
	LastSeen   string  `json:"last_seen"`
	SampleVals []string `json:"sample_values,omitempty"`
}

// ObservationRelation links an observation to a graph entity with provenance.
type ObservationRelation struct {
	ObservationID string         `json:"observation_id"`
	TargetID      string         `json:"target_id"`
	Kind          string         `json:"kind"`
	Evidence      EvidenceLevel `json:"evidence"`
	Confidence    float64       `json:"confidence"`
	Method        string        `json:"method"`
	Reason        string        `json:"reason,omitempty"`
}

// ObservationAggregator tracks repeated log lines.
type ObservationAggregator struct {
	TemplateID   string   `json:"template_id"`
	Template     string   `json:"template"`
	Count        int      `json:"count"`
	FirstSeen    string   `json:"first_seen"`
	LastSeen     string   `json:"last_seen"`
	SampleValues []string `json:"sample_values,omitempty"`
	SourceIDs    []string `json:"source_symbol_ids,omitempty"`
}
