package services

import (
	"regexp"
	"sort"
	"strings"
)

// langSpec describes how to index a language that has no tree-sitter grammar
// wired in. Function, type and import discovery is regex based; assembly has a
// dedicated extractor because its "functions" are labels.
type langSpec struct {
	name     string
	exts     []string
	group    string           // call-resolution group; "" means name (calls never cross groups)
	funcRes  []*regexp.Regexp // group 1 = name, remaining groups = parameters (first non-empty wins)
	typeRe   *regexp.Regexp   // group 1 = keyword, group 2 = name
	importRe *regexp.Regexp   // first non-empty group = import target
	callRe   *regexp.Regexp   // first non-empty group = callee; nil means name(
	endNext  bool             // a function ends where the next one starts (no closing brace to find)
	custom   func(content string) []funcInfo
}

// structKeywords map to the graph "Struct" kind; every other type keyword is a "Class".
var structKeywords = map[string]bool{"struct": true, "union": true, "message": true, "table": true, "record": true}

// notNames are keywords that look like calls or definitions to a regex.
var notNames = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true, "catch": true, "return": true,
	"else": true, "elif": true, "do": true, "try": true, "foreach": true, "using": true,
	"lock": true, "sizeof": true, "typeof": true, "when": true, "match": true, "case": true,
	"function": true, "fn": true, "def": true, "begin": true, "then": true, "until": true,
}

func re(p string) *regexp.Regexp { return regexp.MustCompile(p) }

// typeKw builds a type-declaration regex over the given keywords, tolerating
// common leading modifiers.
func typeKw(kws string) *regexp.Regexp {
	return re(`(?m)^[ \t]*(?:(?:public|private|protected|internal|static|final|abstract|sealed|open|data|enum|partial|export|pub|inline|value|unsafe|readonly|mutable|case|extern|fileprivate|@\w+)[ \t]+)*(` +
		kws + `)[ \t]+([A-Za-z_]\w*)`)
}

// jsSpecFuncRes splits jsFuncRe's alternation into single-name regexes.
var jsSpecFuncRes = []*regexp.Regexp{
	re(`(?m)^\s*(?:export\s+)?(?:async\s+)?function\s*\*?\s*([A-Za-z_$][\w$]*)\s*\(([^)]*)\)`),
	re(`(?m)^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s+)?\(([^)]*)\)\s*=>`),
}

const cMods = `(?:public|private|protected|internal|static|virtual|override|abstract|async|sealed|extern|unsafe|partial|new|final|synchronized|native|inline|export|pub|readonly)`

var langSpecs = []*langSpec{
	{name: "jsx", exts: []string{".jsx", ".mjs", ".cjs"}, group: "js", funcRes: jsSpecFuncRes},
	{name: "vue", exts: []string{".vue"}, group: "js", funcRes: jsSpecFuncRes,
		importRe: re(`(?m)^\s*import\s+(?:[^'"]*?\s+from\s+)?['"]([^'"]+)['"]`)},
	{name: "svelte", exts: []string{".svelte"}, group: "js", funcRes: jsSpecFuncRes,
		importRe: re(`(?m)^\s*import\s+(?:[^'"]*?\s+from\s+)?['"]([^'"]+)['"]`)},
	{name: "csharp", exts: []string{".cs"},
		funcRes:  []*regexp.Regexp{re(`(?m)^[ \t]*(?:` + cMods + `[ \t]+)+[\w<>\[\],.?]+[ \t]+([A-Za-z_]\w*)[ \t]*(?:<[^>()]*>)?[ \t]*\(([^)]*)\)`)},
		typeRe:   typeKw(`class|struct|interface|enum|record`),
		importRe: re(`(?m)^\s*using\s+(?:static\s+)?([\w.]+)\s*;`)},
	{name: "kotlin", exts: []string{".kt", ".kts"},
		funcRes:  []*regexp.Regexp{re(`(?m)^[ \t]*(?:(?:public|private|protected|internal|override|open|abstract|final|inline|suspend|operator|infix|tailrec|external|actual|expect)[ \t]+)*fun[ \t]+(?:<[^>]*>[ \t]+)?(?:[\w.<>?]+\.)?([A-Za-z_]\w*)[ \t]*\(([^)]*)\)`)},
		typeRe:   typeKw(`class|interface|object`),
		importRe: re(`(?m)^\s*import\s+([\w.]+)`)},
	{name: "swift", exts: []string{".swift"},
		funcRes:  []*regexp.Regexp{re(`(?m)^[ \t]*(?:(?:public|private|fileprivate|internal|open|static|class|final|override|mutating|nonmutating|@\w+)[ \t]+)*func[ \t]+([A-Za-z_]\w*)[ \t]*(?:<[^>]*>)?[ \t]*\(([^)]*)\)`)},
		typeRe:   typeKw(`class|struct|protocol|enum|actor|extension`),
		importRe: re(`(?m)^\s*import\s+(?:\w+\s+)?([\w.]+)`)},
	{name: "scala", exts: []string{".scala", ".sc"},
		funcRes:  []*regexp.Regexp{re(`(?m)^[ \t]*(?:(?:override|private|protected|final|implicit|inline|lazy)(?:\[\w+\])?[ \t]+)*def[ \t]+([A-Za-z_]\w*)[ \t]*(?:\[[^\]]*\])?[ \t]*(?:\(([^)]*)\))?`)},
		typeRe:   typeKw(`class|trait|object`),
		importRe: re(`(?m)^\s*import\s+([\w.]+)`)},
	{name: "ruby", exts: []string{".rb", ".rake", ".gemspec"}, endNext: false,
		funcRes:  []*regexp.Regexp{re(`(?m)^[ \t]*def[ \t]+(?:self\.)?([A-Za-z_]\w*[?!=]?)[ \t]*(?:\(([^)]*)\)|[ \t]+([^\n#]*))?`)},
		typeRe:   typeKw(`class|module`),
		importRe: re(`(?m)^\s*require(?:_relative)?[ \t(]+['"]([^'"]+)['"]`)},
	{name: "crystal", exts: []string{".cr"},
		funcRes:  []*regexp.Regexp{re(`(?m)^[ \t]*(?:(?:private|protected|abstract)[ \t]+)*def[ \t]+(?:self\.)?([A-Za-z_]\w*[?!]?)[ \t]*(?:\(([^)]*)\))?`)},
		typeRe:   typeKw(`class|struct|module|enum`),
		importRe: re(`(?m)^\s*require\s+"([^"]+)"`)},
	{name: "php", exts: []string{".php", ".phtml"},
		funcRes:  []*regexp.Regexp{re(`(?m)^[ \t]*(?:(?:public|private|protected|static|final|abstract)[ \t]+)*function[ \t]+&?([A-Za-z_]\w*)[ \t]*\(([^)]*)\)`)},
		typeRe:   typeKw(`class|interface|trait|enum`),
		importRe: re(`(?m)^\s*(?:use\s+([\w\\]+)|(?:require|include)(?:_once)?[ \t(]+['"]([^'"]+)['"])`)},
	{name: "lua", exts: []string{".lua"},
		funcRes:  []*regexp.Regexp{re(`(?m)^[ \t]*(?:local[ \t]+)?function[ \t]+([A-Za-z_][\w.:]*)[ \t]*\(([^)]*)\)`)},
		importRe: re(`require\s*\(?\s*['"]([^'"]+)['"]`)},
	{name: "perl", exts: []string{".pl", ".pm", ".t"},
		funcRes:  []*regexp.Regexp{re(`(?m)^[ \t]*sub[ \t]+([A-Za-z_]\w*)()`)},
		typeRe:   re(`(?m)^[ \t]*(package)[ \t]+([A-Za-z_][\w:]*)`),
		importRe: re(`(?m)^\s*(?:use|require)\s+([A-Za-z_][\w:]*)`)},
	{name: "r", exts: []string{".r"},
		funcRes:  []*regexp.Regexp{re(`(?m)^[ \t]*([A-Za-z_.][\w.]*)[ \t]*(?:<-|=)[ \t]*function[ \t]*\(([^)]*)\)`)},
		importRe: re(`(?m)^\s*(?:library|require|source)\(\s*['"]?([\w./-]+)`)},
	{name: "julia", exts: []string{".jl"},
		funcRes: []*regexp.Regexp{
			re(`(?m)^[ \t]*function[ \t]+([A-Za-z_]\w*!?)[ \t]*(?:\{[^}]*\})?[ \t]*\(([^)]*)\)`),
			re(`(?m)^([A-Za-z_]\w*!?)\(([^)]*)\)[ \t]*=[^=]`)},
		typeRe:   typeKw(`struct|abstract type|primitive type`),
		importRe: re(`(?m)^\s*(?:using|import)\s+([A-Za-z_][\w.]*)`)},
	{name: "dart", exts: []string{".dart"},
		funcRes:  []*regexp.Regexp{re(`(?m)^[ \t]*(?:(?:static|final|external|abstract|@override)[ \t]+)*[\w<>?,\[\]]+[ \t]+([A-Za-z_]\w*)[ \t]*\(([^)]*)\)[ \t]*(?:async\*?[ \t]*)?(?:\{|=>)`)},
		typeRe:   typeKw(`class|mixin|enum|extension`),
		importRe: re(`(?m)^\s*(?:import|export|part)\s+['"]([^'"]+)['"]`)},
	{name: "haskell", exts: []string{".hs", ".lhs"},
		funcRes:  []*regexp.Regexp{re(`(?m)^([a-z_][\w']*)[ \t]*::()`)},
		typeRe:   re(`(?m)^(data|newtype|class)[ \t]+(?:\([^)]*\)[ \t]*=>[ \t]*)?([A-Z]\w*)`),
		importRe: re(`(?m)^import\s+(?:qualified\s+)?([A-Z][\w.]*)`)},
	{name: "ocaml", exts: []string{".ml", ".mli"},
		funcRes:  []*regexp.Regexp{re(`(?m)^[ \t]*(?:let|and)[ \t]+(?:rec[ \t]+)?([a-z_]\w*)[ \t]+([^=\n]+?)=`)},
		typeRe:   re(`(?m)^[ \t]*(type|module)[ \t]+(?:rec[ \t]+)?([A-Za-z_]\w*)`),
		importRe: re(`(?m)^\s*(?:open|include)\s+([A-Z][\w.]*)`)},
	{name: "fsharp", exts: []string{".fs", ".fsx", ".fsi"},
		funcRes:  []*regexp.Regexp{re(`(?m)^[ \t]*let[ \t]+(?:(?:inline|rec|private|public|internal)[ \t]+)*([A-Za-z_]\w*)[ \t]+([^=\n]+?)=`)},
		typeRe:   re(`(?m)^[ \t]*(type|module)[ \t]+(?:private[ \t]+|internal[ \t]+)?([A-Za-z_]\w*)`),
		importRe: re(`(?m)^\s*open\s+([\w.]+)`)},
	{name: "elixir", exts: []string{".ex", ".exs"},
		funcRes:  []*regexp.Regexp{re(`(?m)^[ \t]*defp?[ \t]+([a-z_]\w*[?!]?)(?:\(([^)]*)\))?`)},
		typeRe:   re(`(?m)^[ \t]*(defmodule)[ \t]+([A-Z][\w.]*)`),
		importRe: re(`(?m)^\s*(?:import|alias|use|require)\s+([A-Z][\w.]*)`)},
	{name: "erlang", exts: []string{".erl", ".hrl"},
		funcRes:  []*regexp.Regexp{re(`(?m)^([a-z]\w*)\(([^)]*)\)[ \t]*(?:when[^\n]*)?->`)},
		importRe: re(`(?m)^-include(?:_lib)?\("([^"]+)"\)`)},
	{name: "clojure", exts: []string{".clj", ".cljs", ".cljc", ".edn"},
		funcRes:  []*regexp.Regexp{re(`\((?:defn-?|defmacro|defmulti)[ \t]+(?:\^\S+[ \t]+)?([^\s\[(]+)()`)},
		callRe:   re(`\(([A-Za-z_][\w.*!?<>=+/-]*)`),
		importRe: re(`\[([a-z][\w.-]*)\s+:(?:as|refer)`)},
	{name: "groovy", exts: []string{".groovy", ".gradle", ".gvy"},
		funcRes: []*regexp.Regexp{
			re(`(?m)^[ \t]*(?:(?:public|private|protected|static|final|synchronized)[ \t]+)*def[ \t]+([A-Za-z_]\w*)[ \t]*\(([^)]*)\)`),
			javaFuncRe},
		typeRe:   typeKw(`class|interface|trait|enum`),
		importRe: re(`(?m)^\s*import\s+(?:static\s+)?([\w.*]+)`)},
	{name: "shell", exts: []string{".sh", ".bash", ".zsh", ".ksh"},
		funcRes: []*regexp.Regexp{
			re(`(?m)^[ \t]*function[ \t]+([A-Za-z_][\w:.-]*)[ \t]*(?:\(\))?()`),
			re(`(?m)^[ \t]*([A-Za-z_][\w:.-]*)[ \t]*\(\)[ \t]*()`)},
		callRe:   re(`(?m)(?:^|[;&|(]|\$\()[ \t]*([A-Za-z_][\w:.-]*)`),
		importRe: re(`(?m)^\s*(?:source|\.)\s+["']?([^\s"';]+)`)},
	{name: "powershell", exts: []string{".ps1", ".psm1"},
		funcRes:  []*regexp.Regexp{re(`(?im)^[ \t]*function[ \t]+([\w-]+)[ \t]*(?:\(([^)]*)\))?`)},
		callRe:   re(`\b([A-Za-z]+-[A-Za-z]+)\b`),
		importRe: re(`(?im)^\s*(?:Import-Module|\.)\s+["']?([^\s"']+)`)},
	{name: "objc", exts: []string{".m", ".mm"}, group: "c",
		funcRes:  []*regexp.Regexp{re(`(?m)^[ \t]*[-+][ \t]*\([^)]*\)[ \t]*([A-Za-z_]\w*)()`), genericFuncRe},
		typeRe:   re(`(?m)^[ \t]*@(interface|implementation|protocol)[ \t]+([A-Za-z_]\w*)`),
		importRe: re(`(?m)^\s*#(?:import|include)\s*[<"]([^>"]+)`)},
	{name: "zig", exts: []string{".zig"},
		funcRes:  []*regexp.Regexp{re(`(?m)^[ \t]*(?:pub[ \t]+)?(?:(?:inline|export|extern)[ \t]+)*fn[ \t]+([A-Za-z_]\w*)[ \t]*\(([^)]*)\)`)},
		typeRe:   re(`(?m)^[ \t]*(?:pub[ \t]+)?const[ \t]+([A-Za-z_]\w*)[ \t]*=[ \t]*(?:packed[ \t]+|extern[ \t]+)?(struct|enum|union)`),
		importRe: re(`@import\("([^"]+)"\)`)},
	{name: "nim", exts: []string{".nim", ".nims"},
		funcRes:  []*regexp.Regexp{re(`(?m)^[ \t]*(?:proc|func|method|iterator|template|macro)[ \t]+[\x60]?([A-Za-z_]\w*)[\x60]?\*?[ \t]*(?:\[[^\]]*\])?[ \t]*\(([^)]*)\)`)},
		importRe: re(`(?m)^\s*(?:import|from|include)\s+([\w/]+)`)},
	{name: "d", exts: []string{".d"},
		funcRes:  []*regexp.Regexp{re(`(?m)^[ \t]*(?:(?:public|private|static|final|override|pure|nothrow|@\w+)[ \t]+)*[\w!\[\]*]+[ \t]+([A-Za-z_]\w*)[ \t]*\(([^)]*)\)[ \t]*(?:const|nothrow|pure|@\w+|[ \t])*\{`)},
		typeRe:   typeKw(`class|struct|interface|union|enum`),
		importRe: re(`(?m)^\s*import\s+([\w.]+)`)},
	{name: "fortran", exts: []string{".f90", ".f95", ".f03", ".f08", ".f", ".for"},
		funcRes:  []*regexp.Regexp{re(`(?im)^[ \t]*(?:(?:pure|elemental|recursive|impure|integer|real|double[ \t]+precision|logical|character(?:\([^)]*\))?|complex)[ \t]+)*(?:subroutine|function)[ \t]+([A-Za-z_]\w*)[ \t]*(?:\(([^)]*)\))?`)},
		callRe:   re(`(?i)\bcall[ \t]+([A-Za-z_]\w*)|\b([A-Za-z_]\w*)[ \t]*\(`),
		typeRe:   re(`(?im)^[ \t]*(type|module)[ \t]*(?:::)?[ \t]*([A-Za-z_]\w*)`),
		importRe: re(`(?im)^\s*use\s+([A-Za-z_]\w*)`)},
	{name: "cobol", exts: []string{".cob", ".cbl", ".cobol", ".cpy"},
		funcRes: []*regexp.Regexp{
			re(`(?i)program-id\.[ \t]*([\w-]+)()`),
			re(`(?im)^[ \t\d]*([A-Za-z0-9][\w-]*)[ \t]+section[ \t]*\.()`)},
		callRe:   re(`(?i)\b(?:perform|call)[ \t]+["']?([\w-]+)`),
		importRe: re(`(?im)^\s*copy\s+([\w-]+)`), endNext: true},
	{name: "pascal", exts: []string{".pas", ".pp", ".dpr"},
		funcRes:  []*regexp.Regexp{re(`(?im)^[ \t]*(?:class[ \t]+)?(?:procedure|function|constructor|destructor)[ \t]+(?:\w+\.)?([A-Za-z_]\w*)[ \t]*(?:\(([^)]*)\))?`)},
		typeRe:   re(`(?im)^[ \t]*([A-Za-z_]\w*)[ \t]*=[ \t]*(?:packed[ \t]+)?(class|record|interface|object)\b`),
		importRe: re(`(?im)^\s*uses\s+([\w., ]+);`)},
	{name: "ada", exts: []string{".adb", ".ads", ".ada"},
		funcRes:  []*regexp.Regexp{re(`(?im)^[ \t]*(?:procedure|function)[ \t]+([A-Za-z_]\w*)[ \t]*(?:\(([^)]*)\))?`)},
		typeRe:   re(`(?im)^[ \t]*(type|package)[ \t]+([A-Za-z_][\w.]*)`),
		importRe: re(`(?im)^\s*with\s+([\w.]+)`)},
	{name: "solidity", exts: []string{".sol"},
		funcRes:  []*regexp.Regexp{re(`(?m)^[ \t]*function[ \t]+([A-Za-z_]\w*)[ \t]*\(([^)]*)\)`)},
		typeRe:   typeKw(`contract|library|interface|struct|enum`),
		importRe: re(`(?m)^\s*import\s+(?:[^'"]*from\s+)?['"]([^'"]+)['"]`)},
	{name: "verilog", exts: []string{".v", ".sv", ".svh", ".vh"},
		funcRes: []*regexp.Regexp{
			re(`(?m)^[ \t]*(?:module|macromodule|task)[ \t]+(?:automatic[ \t]+)?([A-Za-z_]\w*)()`),
			re(`(?m)^[ \t]*function[ \t]+(?:automatic[ \t]+)?(?:\[[^\]]*\][ \t]+|\w+[ \t]+)?([A-Za-z_]\w*)[ \t]*(?:\(([^)]*)\))?`)},
		importRe: re("(?m)^\\s*`include\\s+\"([^\"]+)\"")},
	{name: "vhdl", exts: []string{".vhd", ".vhdl"},
		funcRes:  []*regexp.Regexp{re(`(?im)^[ \t]*(?:procedure|function|entity)[ \t]+([A-Za-z_]\w*)()`)},
		importRe: re(`(?im)^\s*use\s+([\w.]+)`)},
	{name: "sql", exts: []string{".sql"},
		funcRes: []*regexp.Regexp{re("(?im)^[ \\t]*create[ \\t]+(?:or[ \\t]+replace[ \\t]+)?(?:function|procedure|trigger|view)[ \\t]+(?:if[ \\t]+not[ \\t]+exists[ \\t]+)?([\\w.\"`\\[\\]]+)[ \\t]*(?:\\(([^)]*)\\))?")},
		typeRe:  re("(?im)^[ \\t]*create[ \\t]+(?:temporary[ \\t]+)?(table)[ \\t]+(?:if[ \\t]+not[ \\t]+exists[ \\t]+)?([A-Za-z_]\\w*)")},
	{name: "protobuf", exts: []string{".proto"},
		funcRes:  []*regexp.Regexp{re(`(?m)^[ \t]*rpc[ \t]+([A-Za-z_]\w*)[ \t]*\(([^)]*)\)`)},
		typeRe:   typeKw(`message|service|enum`),
		importRe: re(`(?m)^\s*import\s+(?:public\s+|weak\s+)?"([^"]+)"`)},
	{name: "lisp", exts: []string{".lisp", ".lsp", ".el", ".cl"},
		funcRes:  []*regexp.Regexp{re(`\((?:defun|defmacro|defsubst|cl-defun|defgeneric|defmethod)[ \t]+([^\s()]+)[ \t]*\(([^)]*)\)`)},
		callRe:   re(`\(([A-Za-z_][\w*!?<>=+/:-]*)`),
		importRe: re(`\((?:require|load|use-package)[ \t]+'?["']?([\w./-]+)`)},
	{name: "scheme", exts: []string{".scm", ".ss", ".rkt", ".sld"},
		funcRes:  []*regexp.Regexp{re(`\(define(?:-syntax)?[ \t]+\(([^\s()]+)([^)]*)\)`)},
		callRe:   re(`\(([A-Za-z_][\w*!?<>=+/:-]*)`),
		importRe: re(`\((?:require|import)[ \t]+([\w./-]+)`)},
	{name: "tcl", exts: []string{".tcl"},
		funcRes:  []*regexp.Regexp{re(`(?m)^[ \t]*proc[ \t]+([\w:]+)[ \t]+\{([^}]*)\}`)},
		importRe: re(`(?m)^\s*(?:package\s+require|source)\s+([\w./:-]+)`)},
	{name: "cuda", exts: []string{".cu", ".cuh", ".glsl", ".hlsl", ".frag", ".vert", ".wgsl"}, group: "c",
		funcRes:  []*regexp.Regexp{genericFuncRe},
		typeRe:   typeKw(`struct|class`),
		importRe: re(`(?m)^\s*#include\s*[<"]([^>"]+)`)},
	{name: "asm", exts: []string{".asm", ".s", ".nasm", ".masm", ".inc"}, group: "asm", custom: extractAsmFunctions,
		importRe: re(`(?im)^\s*[%.#]?include\s+["<']?([^">'\s]+)`)},
}

var langByExt = func() map[string]*langSpec {
	m := map[string]*langSpec{}
	for _, l := range langSpecs {
		for _, e := range l.exts {
			m[e] = l
		}
	}
	return m
}()

func init() {
	for ext := range langByExt {
		supportedExts[ext] = true
	}
}

// specFor returns the regex-based spec for a file suffix (case-insensitive).
func specFor(suffix string) *langSpec { return langByExt[strings.ToLower(suffix)] }

// treeSitterLangNames lists languages parsed with a tree-sitter grammar.
var treeSitterLangNames = map[string]string{
	".py": "python", ".go": "go", ".rs": "rust", ".c": "c", ".h": "c", ".cc": "cpp", ".cpp": "cpp",
	".hpp": "cpp", ".cxx": "cpp", ".hxx": "cpp", ".hh": "cpp", ".java": "java", ".js": "javascript",
	".ts": "typescript", ".tsx": "tsx",
}

// LanguageOf returns the language name for a file suffix, or "" when unsupported.
func LanguageOf(suffix string) string {
	s := strings.ToLower(suffix)
	if n, ok := treeSitterLangNames[s]; ok {
		return n
	}
	if sp := specFor(s); sp != nil {
		return sp.name
	}
	return ""
}

// SupportedLanguages lists every indexable language with its file extensions.
func SupportedLanguages() []map[string]any {
	byName := map[string][]string{}
	engine := map[string]string{}
	for ext, n := range treeSitterLangNames {
		byName[n] = append(byName[n], ext)
		engine[n] = "tree-sitter"
	}
	for _, sp := range langSpecs {
		byName[sp.name] = append(byName[sp.name], sp.exts...)
		if sp.custom != nil {
			engine[sp.name] = "label-scanner"
		} else if _, ok := engine[sp.name]; !ok {
			engine[sp.name] = "regex"
		}
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]map[string]any, 0, len(names))
	for _, n := range names {
		exts := byName[n]
		sort.Strings(exts)
		out = append(out, map[string]any{"language": n, "extensions": exts, "engine": engine[n]})
	}
	return out
}

// lineIndex maps byte offsets and 1-based line numbers in O(log n) / O(1), so
// extractors stay linear on files with tens of thousands of definitions.
type lineIndex struct {
	content string
	starts  []int // byte offset of each line's first byte
}

func newLineIndex(content string) *lineIndex {
	starts := []int{0}
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return &lineIndex{content, starts}
}

// lineOf returns the 1-based line containing the byte offset.
func (x *lineIndex) lineOf(offset int) int {
	return sort.Search(len(x.starts), func(i int) bool { return x.starts[i] > offset })
}

// span returns the byte range of lines startLine..endLine (inclusive, 1-based),
// matching lineSpan's result without rescanning the file.
func (x *lineIndex) span(startLine, endLine int) span {
	start, end := 0, len(x.content)
	if startLine >= 1 && startLine <= len(x.starts) {
		start = x.starts[startLine-1]
	}
	if endLine >= 1 && endLine < len(x.starts) {
		end = x.starts[endLine] - 1
	}
	return span{startByte: start, endByte: end}
}

// cleanSymbolName reduces schema-qualified, quoted or owner-qualified names to the bare identifier.
func cleanSymbolName(name string) string {
	name = strings.Trim(name, "\"`[]'")
	if i := strings.LastIndexAny(name, ".:"); i >= 0 && i < len(name)-1 {
		name = name[i+1:]
	}
	return strings.Trim(name, "\"`[]'")
}

// specFunctions finds function definitions with a spec's regexes.
func specFunctions(sp *langSpec, content string, lx *lineIndex) []functionMatch {
	var out []functionMatch
	seen := map[int]bool{}
	for _, r := range sp.funcRes {
		for _, m := range r.FindAllStringSubmatchIndex(content, -1) {
			if m[2] < 0 || seen[m[2]] {
				continue
			}
			name := cleanSymbolName(content[m[2]:m[3]])
			if name == "" || notNames[strings.ToLower(name)] {
				continue
			}
			params := ""
			for g := 2; 2*g+1 < len(m); g++ {
				if m[2*g] >= 0 {
					params = content[m[2*g]:m[2*g+1]]
					break
				}
			}
			seen[m[2]] = true
			out = append(out, functionMatch{name: name, params: params, start: lx.lineOf(m[2])})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].start < out[j].start })
	return out
}

// specFunctionInfos builds funcInfos (with call lists and line ranges) for a spec.
func specFunctionInfos(sp *langSpec, content string) []funcInfo {
	if sp.custom != nil {
		return sp.custom(content)
	}
	lines := strings.Split(content, "\n")
	lx := newLineIndex(content)
	matches := specFunctions(sp, content, lx)
	cre := callRe
	if sp.callRe != nil {
		cre = sp.callRe
	}
	var out []funcInfo
	for i, fm := range matches {
		start := fm.start - 1
		end := start + 80
		if sp.endNext {
			end = len(lines)
		}
		if i+1 < len(matches) && matches[i+1].start-1 < end {
			end = matches[i+1].start - 1
			if end <= start {
				end = start + 1
			}
		}
		if end > len(lines) {
			end = len(lines)
		}
		fi := funcInfo{name: fm.name, params: countParams(fm.params), start: fm.start, end: end,
			span: lx.span(fm.start, end)}
		for _, cm := range cre.FindAllStringSubmatch(strings.Join(lines[start:end], "\n"), -1) {
			for _, g := range cm[1:] {
				if g != "" {
					if g != fm.name && !notNames[strings.ToLower(g)] {
						fi.calls = append(fi.calls, g)
					}
					break
				}
			}
		}
		out = append(out, fi)
	}
	return out
}

// specTypes finds class-like and struct-like declarations for a spec.
func specTypes(sp *langSpec, content string) []typeMatch {
	if sp.typeRe == nil {
		return nil
	}
	lx := newLineIndex(content)
	var out []typeMatch
	for _, idx := range sp.typeRe.FindAllStringSubmatchIndex(content, -1) {
		kw, name := content[idx[2]:idx[3]], content[idx[4]:idx[5]]
		// Zig and Pascal put the name before the keyword.
		if sp.name == "zig" || sp.name == "pascal" {
			kw, name = name, kw
		}
		kw = strings.ToLower(kw)
		kind := "Class"
		if structKeywords[kw] {
			kind = "Struct"
		}
		line := lx.lineOf(idx[4])
		out = append(out, typeMatch{name: name, kind: kind, typeKind: kw, start: line, end: line,
			span: lx.span(line, line)})
	}
	return out
}

// specImports finds import targets for a spec.
func specImports(sp *langSpec, content string) []string {
	if sp.importRe == nil {
		return nil
	}
	var out []string
	for _, m := range sp.importRe.FindAllStringSubmatch(content, -1) {
		for _, g := range m[1:] {
			if g != "" {
				for _, part := range strings.Split(g, ",") {
					if p := strings.TrimSpace(part); p != "" {
						out = append(out, p)
					}
				}
				break
			}
		}
	}
	return out
}
