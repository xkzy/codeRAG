package services

import (
	"regexp"
	"strings"
)

// Assembly (GAS/AT&T and Intel, NASM, MASM, ARM, AArch64, RISC-V, MIPS) has no
// function syntax, so functions are recovered from labels:
//
//   - a label is a function when it is declared global (.globl/global/public),
//     typed as one (.type x,@function / .func), opened with PROC, or is the
//     target of a call (call/bl/jal/tail);
//   - every other label is a local jump target inside the enclosing function;
//   - labels in data sections or followed by data directives are not functions.
var (
	asmLabelRe    = regexp.MustCompile(`^\s*([A-Za-z_$][\w.$@]*)\s*:(.*)$`)
	asmProcRe     = regexp.MustCompile(`(?i)^\s*([A-Za-z_$@?][\w$@?]*)\s+proc\b`)
	asmGlobalRe   = regexp.MustCompile(`(?i)^\s*(?:\.globl|\.global|global|public|\.public|\.weak|\.type|\.func|\.thumb_func|\.proc)\s+([A-Za-z_$][\w.$@]*)(?:\s*,\s*(?:@|%)?(function|object|notype))?`)
	asmSectionRe  = regexp.MustCompile(`(?i)^\s*(?:\.section|section|segment)\s+([^\s,;]+)|^\s*(\.text|\.data|\.bss|\.rodata|\.code|\.const|\.data\?)\b`)
	asmDataDirRe  = regexp.MustCompile(`(?i)^\s*(?:\.?(?:db|dw|dd|dq|dt|resb|resw|resd|resq|equ|byte|word|long|quad|ascii|asciz|string|zero|space|skip|int|short|float|double|octa|fill|comm|lcomm)\b|=)`)
	asmCallRe     = regexp.MustCompile(`(?i)^\s*(?:[A-Za-z_.$][\w.$@]*\s*:\s*)?(?:rep\w*\s+)?(call[lq]?|bl|blx|jal|tail)\s+(?:(?:ra|x1|\$ra)\s*,\s*)?(?:(?:near|far|dword|qword|ptr)\s+)*\*?\s*([A-Za-z_][\w.$@]*)`)
	asmJumpRe     = regexp.MustCompile(`(?i)^\s*(?:[A-Za-z_.$][\w.$@]*\s*:\s*)?(?:jmp|b|j|bra)\s+(?:(?:near|far|short)\s+)*([A-Za-z_][\w.$@]*)\s*$`)
	asmLocalLabel = regexp.MustCompile(`^(?:L(?:BB|tmp|CPI|JTI|str|C|\.)\w*|L\d+|\.L\w*|\d+)$`)
	// Go/Plan 9 assembler: TEXT ·name(SB), flags, $frame-args ; CALL ·f(SB) ; JMP f(SB)
	asmPlan9Text = regexp.MustCompile(`^\s*TEXT\s+([^\s(,]+)\(SB\)`)
	asmPlan9Call = regexp.MustCompile(`^\s*(?:[A-Za-z_]\w*:\s*)?(CALL|JMP|BL|B)\s+([^\s(,$]+)\(SB\)`)
	asmCommentRe = regexp.MustCompile(`(?:;|//|\s@).*$`)
)

func asmClean(line string) string {
	if t := strings.TrimSpace(line); strings.HasPrefix(t, "#") || strings.HasPrefix(t, "*") {
		return ""
	}
	return strings.TrimRight(asmCommentRe.ReplaceAllString(line, ""), " \t\r")
}

func asmSectionIsText(name string) (isText, known bool) {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "text"), strings.Contains(n, "code"), strings.HasPrefix(n, ".init"), strings.HasPrefix(n, ".fini"):
		return true, true
	case strings.Contains(n, "data"), strings.Contains(n, "bss"), strings.Contains(n, "rodata"),
		strings.Contains(n, "const"), strings.Contains(n, "cstring"), strings.Contains(n, "literal"), strings.Contains(n, "stack"):
		return false, true
	}
	return false, false
}

func asmTarget(name string) string {
	if i := strings.Index(name, "@"); i > 0 { // foo@plt, foo@PLT, foo@GOTPCREL
		name = name[:i]
	}
	return name
}

// plan9Name strips the package qualifier: ·blockAVX2 -> blockAVX2, runtime·memmove -> memmove.
func plan9Name(sym string) string {
	if i := strings.LastIndex(sym, "·"); i >= 0 {
		sym = sym[i+len("·"):]
	}
	return sym
}

// extractPlan9Functions handles Go assembler files, where functions are TEXT
// symbols and every label is a local jump target.
func extractPlan9Functions(content string, cleaned []string) []funcInfo {
	lx := newLineIndex(content)
	var out []funcInfo
	var cur *funcInfo
	closeCur := func(endLine int) {
		if cur == nil {
			return
		}
		if endLine < cur.start {
			endLine = cur.start
		}
		cur.end = endLine
		cur.span = lx.span(cur.start, endLine)
		out = append(out, *cur)
		cur = nil
	}
	for i, l := range cleaned {
		if m := asmPlan9Text.FindStringSubmatch(l); m != nil {
			closeCur(i)
			cur = &funcInfo{name: plan9Name(m[1]), start: i + 1}
			continue
		}
		if cur == nil {
			continue
		}
		if m := asmPlan9Call.FindStringSubmatch(l); m != nil {
			t := plan9Name(m[2])
			if t != cur.name && (m[1] == "CALL" || m[1] == "BL" || !strings.HasPrefix(m[2], ".")) {
				cur.calls = append(cur.calls, t)
			}
		}
	}
	closeCur(len(cleaned))
	return out
}

func extractAsmFunctions(content string) []funcInfo {
	lines := strings.Split(content, "\n")
	lx := newLineIndex(content)
	cleaned := make([]string, len(lines))
	for i, l := range lines {
		cleaned[i] = asmClean(l)
	}
	for _, l := range cleaned {
		if asmPlan9Text.MatchString(l) {
			return extractPlan9Functions(content, cleaned)
		}
	}

	// Pass 1: which labels are functions.
	funcSet := map[string]bool{}
	for _, l := range cleaned {
		if m := asmGlobalRe.FindStringSubmatch(l); m != nil {
			// `.type x, @object` is data; `.globl x` and `.type x, @function` are candidates.
			if !strings.EqualFold(m[2], "object") {
				funcSet[asmTarget(m[1])] = true
			}
		}
		if m := asmProcRe.FindStringSubmatch(l); m != nil {
			funcSet[m[1]] = true
		}
		if m := asmCallRe.FindStringSubmatch(l); m != nil {
			funcSet[asmTarget(m[2])] = true
		}
	}

	// Pass 2: walk labels, tracking sections.
	var out []funcInfo
	var cur *funcInfo
	inText := true
	closeCur := func(endLine int) {
		if cur == nil {
			return
		}
		if endLine < cur.start {
			endLine = cur.start
		}
		cur.end = endLine
		cur.span = lx.span(cur.start, endLine)
		out = append(out, *cur)
		cur = nil
	}

	for i, l := range cleaned {
		lineNo := i + 1
		if l == "" {
			continue
		}
		if m := asmSectionRe.FindStringSubmatch(l); m != nil {
			name := m[1]
			if name == "" {
				name = m[2]
			}
			if isText, known := asmSectionIsText(name); known {
				if !isText {
					closeCur(lineNo - 1)
				}
				inText = isText
			}
			continue
		}
		if !inText {
			continue
		}
		name, rest := "", ""
		if m := asmProcRe.FindStringSubmatch(l); m != nil {
			name = m[1]
		} else if m := asmLabelRe.FindStringSubmatch(l); m != nil {
			name, rest = m[1], m[2]
		}
		if name != "" {
			isFunc := false
			switch {
			case strings.HasPrefix(name, ".") || asmLocalLabel.MatchString(name):
			case asmDataDirRe.MatchString(rest):
			case funcSet[name]:
				isFunc = true
			case cur == nil && len(funcSet) == 0:
				// A file that declares nothing global still has its first label as an entry point.
				isFunc = true
			}
			if isFunc {
				closeCur(lineNo - 1)
				cur = &funcInfo{name: name, start: lineNo}
			}
		}
		if strings.HasSuffix(strings.ToLower(strings.TrimSpace(l)), "endp") && cur != nil {
			closeCur(lineNo)
			continue
		}
		if cur == nil {
			continue
		}
		if m := asmCallRe.FindStringSubmatch(l); m != nil {
			t := asmTarget(m[2])
			if t != cur.name {
				cur.calls = append(cur.calls, t)
				if strings.HasPrefix(t, "_") && len(t) > 1 { // Mach-O prefixes C symbols with _
					cur.calls = append(cur.calls, t[1:])
				}
			}
		} else if m := asmJumpRe.FindStringSubmatch(l); m != nil {
			// A jump to another function label is a tail call.
			if t := asmTarget(m[1]); t != cur.name && funcSet[t] {
				cur.calls = append(cur.calls, t)
			}
		}
	}
	closeCur(len(lines))
	return out
}
