package services

import (
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func funcNames(src, ext string) []string {
	var names []string
	for _, f := range mergeFuncInfos(extractFunctionInfos(src, ext)) {
		names = append(names, f.name)
	}
	sort.Strings(names)
	return names
}

func TestSupportedLanguageCount(t *testing.T) {
	langs := SupportedLanguages()
	if len(langs) < 40 {
		t.Fatalf("want 40+ languages, got %d", len(langs))
	}
	for _, want := range []string{"asm", "python", "go", "csharp", "kotlin", "fortran", "cobol", "solidity"} {
		found := false
		for _, l := range langs {
			if l["language"] == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing language %s", want)
		}
	}
	for _, ext := range []string{".s", ".S", ".asm", ".kt", ".swift", ".sql"} {
		if !supportedExts[strings.ToLower(ext)] {
			t.Errorf("%s not indexed", ext)
		}
	}
}

func TestRegexLanguageFunctions(t *testing.T) {
	cases := []struct {
		ext, src string
		want     []string
	}{
		{".cs", "public static int Add(int a, int b) {\n  return a+b;\n}\nprivate void Run() { if (x) {} }\n", []string{"Add", "Run"}},
		{".kt", "fun main() {}\nprivate suspend fun <T> load(x: T): T = x\n", []string{"load", "main"}},
		{".swift", "func greet(name: String) {}\npublic static func make() -> Int { 1 }\n", []string{"greet", "make"}},
		{".scala", "def add(a: Int, b: Int) = a + b\noverride def toString = \"x\"\n", []string{"add", "toString"}},
		{".rb", "def hello(name)\nend\ndef self.build\nend\ndef ok?\nend\n", []string{"build", "hello", "ok?"}},
		{".cr", "def run(x)\nend\n", []string{"run"}},
		{".php", "<?php\nfunction a($x) {}\npublic static function b() {}\n", []string{"a", "b"}},
		{".lua", "local function f(a) end\nfunction M.g(b) end\nfunction M:h() end\n", []string{"f", "g", "h"}},
		{".pl", "sub foo {\n}\nsub bar { }\n", []string{"bar", "foo"}},
		{".r", "sq <- function(x) x*x\nadd = function(a, b) a+b\n", []string{"add", "sq"}},
		{".jl", "function f(x)\nend\nsq(x) = x*x\n", []string{"f", "sq"}},
		{".dart", "int add(int a, int b) {\n return a+b;\n}\nvoid main() {\n}\n", []string{"add", "main"}},
		{".hs", "add :: Int -> Int -> Int\nadd a b = a + b\n", []string{"add"}},
		{".ml", "let rec fact n = if n = 0 then 1 else n * fact (n-1)\nlet x = 1\n", []string{"fact"}},
		{".fs", "let add a b = a + b\nlet inline mul a b = a * b\n", []string{"add", "mul"}},
		{".ex", "defmodule M do\n  def a(x), do: x\n  defp b, do: 1\nend\n", []string{"a", "b"}},
		{".erl", "add(A, B) ->\n  A + B.\nfoo(X) when X > 1 ->\n  ok.\n", []string{"add", "foo"}},
		{".clj", "(defn add [a b] (+ a b))\n(defn- helper [] 1)\n", []string{"add", "helper"}},
		{".groovy", "def run(x) { x }\nstatic int calc(int a) {\n a\n}\n", []string{"calc", "run"}},
		{".sh", "build() {\n  echo hi\n}\nfunction deploy {\n  build\n}\n", []string{"build", "deploy"}},
		{".ps1", "function Get-Thing {\n}\nfunction Set-Thing($x) {}\n", []string{"Get-Thing", "Set-Thing"}},
		{".m", "- (void)viewDidLoad {\n}\n+ (id)make {\n}\n", []string{"make", "viewDidLoad"}},
		{".zig", "pub fn add(a: i32, b: i32) i32 { return a + b; }\nfn helper() void {}\n", []string{"add", "helper"}},
		{".nim", "proc add*(a, b: int): int = a + b\nfunc sq(x: int): int = x*x\n", []string{"add", "sq"}},
		{".d", "int add(int a, int b) {\n return a+b;\n}\n", []string{"add"}},
		{".f90", "subroutine hello(x)\nend subroutine\ninteger function sq(x)\nend function\n", []string{"hello", "sq"}},
		{".cob", "       PROGRAM-ID. PAYROLL.\n       MAIN-PARA SECTION.\n           PERFORM CALC-PAY.\n", []string{"MAIN-PARA", "PAYROLL"}},
		{".pas", "procedure Foo(x: Integer);\nfunction Bar: Integer;\n", []string{"Bar", "Foo"}},
		{".adb", "procedure Main is\nbegin\nend Main;\nfunction Sq(X : Integer) return Integer is\n", []string{"Main", "Sq"}},
		{".sol", "contract C {\n  function transfer(address to) public {}\n}\n", []string{"transfer"}},
		{".v", "module top(input a);\nendmodule\nfunction integer f(input x);\nendfunction\n", []string{"f", "top"}},
		{".vhd", "entity Alu is\nend Alu;\nfunction add(a : integer) return integer is\n", []string{"Alu", "add"}},
		{".sql", "CREATE OR REPLACE FUNCTION public.get_user(id int) RETURNS int AS $$ $$;\ncreate procedure do_it() as begin end;\n", []string{"do_it", "get_user"}},
		{".proto", "service S {\n  rpc Get(Req) returns (Resp);\n}\n", []string{"Get"}},
		{".lisp", "(defun add (a b) (+ a b))\n", []string{"add"}},
		{".scm", "(define (add a b) (+ a b))\n", []string{"add"}},
		{".tcl", "proc greet {name} {\n puts $name\n}\n", []string{"greet"}},
		{".cu", "__global__ void kernel(float *x) {\n}\n", []string{"kernel"}},
		{".vue", "<script>\nfunction inc() {}\nconst dec = (x) => x\n</script>\n", []string{"dec", "inc"}},
	}
	for _, c := range cases {
		got := funcNames(c.src, c.ext)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v want %v", c.ext, got, c.want)
		}
	}
}

func TestRegexLanguageTypes(t *testing.T) {
	cases := []struct {
		ext, src, name, kind string
	}{
		{".cs", "public sealed class Foo {}\n", "Foo", "Class"},
		{".cs", "public struct P {}\n", "P", "Struct"},
		{".kt", "data class User(val n: String)\n", "User", "Class"},
		{".swift", "protocol Drawable {}\n", "Drawable", "Class"},
		{".rb", "module Util\nend\n", "Util", "Class"},
		{".php", "trait T {}\n", "T", "Class"},
		{".sol", "contract Token {}\n", "Token", "Class"},
		{".proto", "message Req {}\n", "Req", "Struct"},
		{".zig", "const Point = struct {\n};\n", "Point", "Struct"},
		{".sql", "CREATE TABLE users (id int);\n", "users", "Struct"},
		{".ex", "defmodule Billing.Invoice do\nend\n", "Billing.Invoice", "Class"},
		{".hs", "data Shape = Circle | Square\n", "Shape", "Class"},
	}
	for _, c := range cases {
		found := false
		for _, m := range extractTypes(c.src, c.ext) {
			if m.name == c.name && m.kind == c.kind {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: type %s (%s) not found in %v", c.ext, c.name, c.kind, extractTypes(c.src, c.ext))
		}
	}
}

func TestRegexLanguageImportsAndCalls(t *testing.T) {
	if got := extractImports("using System.Text;\nusing static Foo.Bar;\n", ".cs"); !reflect.DeepEqual(got, []string{"System.Text", "Foo.Bar"}) {
		t.Errorf("cs imports: %v", got)
	}
	if got := extractImports("require 'json'\nrequire_relative \"lib/x\"\n", ".rb"); !reflect.DeepEqual(got, []string{"json", "lib/x"}) {
		t.Errorf("rb imports: %v", got)
	}
	if got := extractImports("import \"google/protobuf/any.proto\";\n", ".proto"); !reflect.DeepEqual(got, []string{"google/protobuf/any.proto"}) {
		t.Errorf("proto imports: %v", got)
	}

	// shell: commands, not parens, are calls
	fs := extractFunctionInfos("build() {\n  echo hi\n}\ndeploy() {\n  build\n}\n", ".sh")
	var deploy funcInfo
	for _, f := range fs {
		if f.name == "deploy" {
			deploy = f
		}
	}
	if !contains(deploy.calls, "build") {
		t.Errorf("shell call not found: %v", deploy.calls)
	}
	// COBOL PERFORM
	cob := extractFunctionInfos("       MAIN-PARA SECTION.\n           PERFORM CALC-PAY.\n       CALC-PAY SECTION.\n", ".cob")
	if len(cob) == 0 || !contains(cob[0].calls, "CALC-PAY") {
		t.Errorf("cobol perform not found: %+v", cob)
	}
	// PowerShell cmdlet calls
	ps := extractFunctionInfos("function Get-A {\n Get-B\n}\nfunction Get-B {\n}\n", ".ps1")
	if len(ps) == 0 || !contains(ps[0].calls, "Get-B") {
		t.Errorf("powershell call not found: %+v", ps)
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

const gasSample = `	.file "a.c"
	.text
	.globl	main
	.type	main, @function
main:
	pushq	%rbp
	call	helper@PLT   # comment
	jmp	.L2
.L2:
	call	printf
	ret
	.size	main, .-main

helper:
	movl	$1, %eax
.loop:
	jne	.loop
	jmp	tail_target
	ret

	.globl tail_target
tail_target:
	ret

	.section .rodata
.LC0:
	.string "hi"
msg:
	.ascii "x"
`

func TestAsmGAS(t *testing.T) {
	fs := extractFunctionInfos(gasSample, ".S")
	got := map[string]funcInfo{}
	for _, f := range fs {
		got[f.name] = f
	}
	for _, want := range []string{"main", "helper", "tail_target"} {
		if _, ok := got[want]; !ok {
			t.Fatalf("missing %s in %v", want, funcNames(gasSample, ".S"))
		}
	}
	for _, not := range []string{".L2", ".loop", ".LC0", "msg"} {
		if _, ok := got[not]; ok {
			t.Errorf("%s must not be a function", not)
		}
	}
	if len(got) != 3 {
		t.Errorf("want 3 functions, got %v", funcNames(gasSample, ".S"))
	}
	if !contains(got["main"].calls, "helper") || !contains(got["main"].calls, "printf") {
		t.Errorf("main calls: %v", got["main"].calls)
	}
	if !contains(got["helper"].calls, "tail_target") {
		t.Errorf("helper tail call: %v", got["helper"].calls)
	}
	if got["main"].start >= got["helper"].start || got["main"].end >= got["helper"].start {
		t.Errorf("ranges overlap: main %d-%d helper %d", got["main"].start, got["main"].end, got["helper"].start)
	}
}

func TestAsmNASM(t *testing.T) {
	src := `section .data
msg: db "hello", 10
section .text
global _start
extern puts
_start:
    mov rdi, msg
    call print
    jmp .done
.done:
    ret
print:
    call puts
    ret
`
	names := funcNames(src, ".asm")
	if !reflect.DeepEqual(names, []string{"_start", "print"}) {
		t.Fatalf("got %v", names)
	}
	for _, f := range extractFunctionInfos(src, ".asm") {
		if f.name == "print" && !contains(f.calls, "puts") {
			t.Errorf("print calls: %v", f.calls)
		}
	}
}

func TestAsmMASMProc(t *testing.T) {
	src := "_TEXT SEGMENT\nmain PROC\n  call helper\n  ret\nmain ENDP\nhelper PROC\n  ret\nhelper ENDP\n"
	if names := funcNames(src, ".asm"); !reflect.DeepEqual(names, []string{"helper", "main"}) {
		t.Errorf("got %v", names)
	}
}

func TestAsmARMAndRISCV(t *testing.T) {
	arm := ".text\n.global main\nmain:\n  bl foo\n  bx lr\nfoo:\n  bl _printf\n  bx lr\n"
	fs := extractFunctionInfos(arm, ".s")
	if len(fs) != 2 || !contains(fs[0].calls, "foo") || !contains(fs[1].calls, "printf") {
		t.Errorf("arm: %+v", fs)
	}
	rv := ".globl f\nf:\n  call g\n  jal ra, h\n  ret\n"
	fs = extractFunctionInfos(rv, ".s")
	if len(fs) != 1 || !contains(fs[0].calls, "g") || !contains(fs[0].calls, "h") {
		t.Errorf("riscv: %+v", fs)
	}
}

func TestAsmImportsAndGroup(t *testing.T) {
	if got := extractImports("%include \"macros.inc\"\n.include \"defs.s\"\n", ".asm"); !reflect.DeepEqual(got, []string{"macros.inc", "defs.s"}) {
		t.Errorf("got %v", got)
	}
	// asm and C share a call-resolution group so asm can call C and vice versa
	for _, native := range []string{"y.c", "y.go", "y.rs", "y.cpp"} {
		if !groupsCompatible(langGroup("x.s"), langGroup(native)) || !groupsCompatible(langGroup(native), langGroup("x.s")) {
			t.Errorf("asm and %s must link both ways", native)
		}
	}
	if groupsCompatible(langGroup("x.s"), langGroup("y.py")) || groupsCompatible(langGroup("y.go"), langGroup("y.c")) {
		t.Error("asm must not link to python, and go must not link to c")
	}
	if langGroup("a.kt") == langGroup("b.java") {
		t.Error("kotlin and java must not share a group")
	}
	if langGroup("a.mjs") != langGroup("b.ts") {
		t.Error("js family must share a group")
	}
}

func TestLanguageOf(t *testing.T) {
	for ext, want := range map[string]string{".py": "python", ".S": "asm", ".kt": "kotlin", ".cxx": "cpp", ".zzz": ""} {
		if got := LanguageOf(ext); got != want {
			t.Errorf("LanguageOf(%s)=%q want %q", ext, got, want)
		}
	}
}

const plan9Sample = `#include "textflag.h"

// func blockAVX2(dig *Digest, p []byte)
TEXT ·blockAVX2(SB), NOSPLIT, $536-32
	MOVQ dig+0(FP), DI
avx2_loop0:
	CALL runtime·memmove(SB)
	CALL ·helper(SB)
	JNE  avx2_loop0
	JMP  ·blockSHANI(SB)
	RET

TEXT ·blockSHANI(SB), $0-32
roundLoop:
	RET
`

func TestAsmPlan9(t *testing.T) {
	fs := extractFunctionInfos(plan9Sample, ".s")
	if got := funcNames(plan9Sample, ".s"); !reflect.DeepEqual(got, []string{"blockAVX2", "blockSHANI"}) {
		t.Fatalf("got %v (labels must not become functions)", got)
	}
	if !contains(fs[0].calls, "memmove") || !contains(fs[0].calls, "helper") || !contains(fs[0].calls, "blockSHANI") {
		t.Errorf("calls: %v", fs[0].calls)
	}
	if fs[0].end >= fs[1].start {
		t.Errorf("ranges overlap: %d-%d / %d", fs[0].start, fs[0].end, fs[1].start)
	}
}

func TestAsmFirstLabelFallbackOnlyWithoutGlobals(t *testing.T) {
	// No globals: first label is the entry point.
	if got := funcNames("entry:\n  ret\n", ".s"); !reflect.DeepEqual(got, []string{"entry"}) {
		t.Errorf("got %v", got)
	}
	// With a declared function, a preceding jump label is not a function.
	if got := funcNames("stray:\n  nop\n.globl real\nreal:\n  ret\n", ".s"); !reflect.DeepEqual(got, []string{"real"}) {
		t.Errorf("got %v", got)
	}
}

func TestLineIndexMatchesLineSpan(t *testing.T) {
	for _, src := range []string{"", "one", "a\nb\nc", "a\nb\nc\n", "\n\n", "x\r\ny\r\n"} {
		lx := newLineIndex(src)
		real := strings.Count(src, "\n") + 1 // lines that can hold a definition
		if strings.HasSuffix(src, "\n") {
			real-- // the empty "line" after a trailing newline is not a real line
		}
		if got := lx.span(real+1, real+1); strings.HasSuffix(src, "\n") && (got.startByte != len(src) || got.endByte != len(src)) {
			t.Errorf("%q phantom line span = %v, want empty span at EOF", src, got)
		}
		n := real + 1
		for s := 1; s <= real; s++ {
			for e := s; e <= n; e++ {
				if got, want := lx.span(s, e), lineSpan(src, s, e); got != want {
					t.Errorf("%q span(%d,%d)=%v want %v", src, s, e, got, want)
				}
			}
		}
		for off := 0; off < len(src); off++ {
			if got, want := lx.lineOf(off), strings.Count(src[:off], "\n")+1; got != want {
				t.Errorf("%q lineOf(%d)=%d want %d", src, off, got, want)
			}
		}
	}
}

// Quadratic behaviour shows up as growth well beyond the input growth, which is
// independent of machine speed (and of the race detector's slowdown).
func TestExtractorsStayLinear(t *testing.T) {
	gen := map[string]func(n int) string{
		".rb": func(n int) string { return strings.Repeat("def f(x)\nend\n", n) },
		".s":  func(n int) string { return strings.Repeat(".globl x\nx:\n", n) },
		".cs": func(n int) string { return strings.Repeat("public int F() {}\n", n) },
	}
	run := func(ext, src string) time.Duration {
		best := time.Duration(1<<63 - 1)
		for i := 0; i < 3; i++ { // best of 3 damps scheduler noise
			start := time.Now()
			extractFunctionInfos(src, ext)
			extractTypes(src, ext)
			if d := time.Since(start); d < best {
				best = d
			}
		}
		return best
	}
	const small, factor = 5000, 4
	for ext, g := range gen {
		a, b := run(ext, g(small)), run(ext, g(small*factor))
		// linear ~4x, quadratic ~16x
		if ratio := float64(b) / float64(a+1); ratio > 10 {
			t.Errorf("%s: %dx the input took %.1fx as long (%v -> %v); extractor is not linear", ext, factor, ratio, a, b)
		}
	}
}
