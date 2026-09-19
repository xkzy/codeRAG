package services

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// funcInfo is a function or method definition together with the names it calls.
type funcInfo struct {
	name   string
	owner  string // enclosing class, struct, impl or receiver type ("" for free functions)
	params int
	start  int
	end    int
	calls  []string
	refs   []string // types and classes mentioned without being called
	span   span     // exact source range of the definition
}

// span is the exact source range of a definition (1-based lines, 0-based
// columns, byte offsets into the file).
type span struct {
	startByte, endByte int
	startCol, endCol   int
}

func spanOf(n *sitter.Node) span {
	return span{int(n.StartByte()), int(n.EndByte()), int(n.StartPoint().Column), int(n.EndPoint().Column)}
}

func (f funcInfo) qualifiedName() string {
	if f.owner == "" {
		return f.name
	}
	return f.owner + "." + f.name
}

// ownerNodeTypes are grammar nodes that give methods an enclosing type name.
var ownerNodeTypes = map[string]bool{
	"class_definition": true, "class_declaration": true, "abstract_class_declaration": true,
	"class_specifier": true, "struct_specifier": true, "interface_declaration": true,
	"enum_declaration": true, "trait_item": true, "impl_item": true, "class": true,
}

var funcNodeTypes = map[string]bool{
	"function_definition": true, "function_declaration": true, "method_declaration": true,
	"function_item": true, "method_definition": true, "constructor_declaration": true,
	"generator_function_declaration": true,
}

var callNodeTypes = map[string]bool{
	"call": true, "call_expression": true, "method_invocation": true,
}

// extractFunctionsTreeSitter returns function definitions and their calls.
// ok is false when the language is unsupported or parsing failed.
func extractFunctionsTreeSitter(content, suffix string) (funcs []funcInfo, ok bool) {
	lang := languageFor(suffix)
	if lang == nil {
		return nil, false
	}
	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(lang)
	src := []byte(content)
	tree, err := parser.ParseCtx(context.Background(), nil, src)
	if err != nil || tree == nil {
		return nil, false
	}
	defer tree.Close()

	var owners []string
	var stack []int // indexes into funcs of the enclosing function definitions

	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		t := n.Type()
		pushedOwner, pushedFunc := false, false

		if ownerNodeTypes[t] {
			if name := ownerName(n, src); name != "" {
				owners = append(owners, name)
				pushedOwner = true
			}
		}
		if fi, isFunc := funcFromNode(n, src, owners); isFunc {
			funcs = append(funcs, fi)
			stack = append(stack, len(funcs)-1)
			pushedFunc = true
		}
		if len(stack) > 0 {
			if name := refFromNode(n, src); name != "" {
				cur := &funcs[stack[len(stack)-1]]
				cur.refs = append(cur.refs, name)
			}
		}
		if callNodeTypes[t] && len(stack) > 0 {
			if qname := calleeQualifiedName(n, src); qname != "" {
				cur := &funcs[stack[len(stack)-1]]
				cur.calls = append(cur.calls, qname)
				// If the qualified form differs from the simple name (i.e. has a
				// qualifier), also record the bare name so name-only resolution still
				// works when the receiver type is not indexed.
				if strings.Contains(qname, ".") {
					if _, bare := calleeInfo(n, src); bare != "" && bare != qname {
						cur.calls = append(cur.calls, bare)
					}
				}
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
		if pushedFunc {
			stack = stack[:len(stack)-1]
		}
		if pushedOwner {
			owners = owners[:len(owners)-1]
		}
	}
	walk(tree.RootNode())
	return funcs, true
}

// refFromNode returns the type name a node mentions, if any: type positions,
// `new X`, and the qualifier of `X.member` / `X::member` when it looks like a type.
func refFromNode(n *sitter.Node, src []byte) string {
	switch n.Type() {
	case "type_identifier", "identifier":
		return typeNameOf(n, src)
	case "new_expression":
		if c := n.ChildByFieldName("constructor"); c != nil {
			return baseName(c, src)
		}
	case "scoped_identifier", "qualified_identifier": // Rust Type::new, C++ Type::member
		q := n.ChildByFieldName("path")
		if q == nil {
			q = n.ChildByFieldName("scope")
		}
		if q != nil && (q.Type() == "identifier" || q.Type() == "namespace_identifier") && isExported(q.Content(src)) {
			return q.Content(src)
		}
	case "member_expression", "attribute", "selector_expression", "method_invocation", "field_expression":
		for _, f := range []string{"object", "operand", "value"} {
			if q := n.ChildByFieldName(f); q != nil && q.Type() == "identifier" && isExported(q.Content(src)) {
				return q.Content(src)
			}
		}
	}
	return ""
}

func ownerName(n *sitter.Node, src []byte) string {
	field := "name"
	if n.Type() == "impl_item" {
		field = "type"
	}
	nm := n.ChildByFieldName(field)
	if nm == nil {
		return ""
	}
	return stripTypeDecorations(nm.Content(src))
}

func stripTypeDecorations(s string) string {
	s = strings.TrimLeft(strings.TrimSpace(s), "*&")
	if i := strings.IndexAny(s, "<["); i > 0 {
		s = s[:i]
	}
	return s
}

func funcFromNode(n *sitter.Node, src []byte, owners []string) (funcInfo, bool) {
	t := n.Type()
	var nameNode, paramsNode *sitter.Node
	owner := ""
	if len(owners) > 0 {
		owner = owners[len(owners)-1]
	}

	switch {
	case funcNodeTypes[t]:
		nameNode = n.ChildByFieldName("name")
		paramsNode = n.ChildByFieldName("parameters")
		if nameNode == nil {
			// C / C++: the name lives inside the declarator chain.
			decl := n.ChildByFieldName("declarator")
			for decl != nil && decl.Type() != "function_declarator" {
				decl = decl.ChildByFieldName("declarator")
			}
			if decl == nil {
				return funcInfo{}, false
			}
			nameNode = decl.ChildByFieldName("declarator")
			paramsNode = decl.ChildByFieldName("parameters")
		}
		if t == "method_declaration" && n.ChildByFieldName("receiver") != nil { // Go
			owner = goReceiverType(n.ChildByFieldName("receiver"), src)
		}
	case t == "variable_declarator" || t == "public_field_definition" || t == "pair":
		// const f = () => {...}
		val := n.ChildByFieldName("value")
		if val == nil {
			return funcInfo{}, false
		}
		if vt := val.Type(); vt != "arrow_function" && vt != "function_expression" && vt != "function" {
			return funcInfo{}, false
		}
		nameNode = n.ChildByFieldName("name")
		if nameNode == nil {
			nameNode = n.ChildByFieldName("key")
		}
		paramsNode = val.ChildByFieldName("parameters")
		if paramsNode == nil {
			paramsNode = val.ChildByFieldName("parameter")
		}
	default:
		return funcInfo{}, false
	}
	if nameNode == nil {
		return funcInfo{}, false
	}

	name := nameNode.Content(src)
	if nameNode.Type() == "qualified_identifier" { // C++ out-of-line definition: A::f
		if scope := nameNode.ChildByFieldName("scope"); scope != nil {
			owner = stripTypeDecorations(scope.Content(src))
		}
		if nm := nameNode.ChildByFieldName("name"); nm != nil {
			name = nm.Content(src)
		}
	}
	if name == "" || strings.ContainsAny(name, " \t\n(") {
		return funcInfo{}, false
	}
	return funcInfo{
		name:   name,
		owner:  owner,
		params: countParamNodes(paramsNode, src),
		start:  int(n.StartPoint().Row) + 1,
		end:    int(n.EndPoint().Row) + 1,
		span:   spanOf(n),
	}, true
}

func goReceiverType(recv *sitter.Node, src []byte) string {
	for i := 0; i < int(recv.NamedChildCount()); i++ {
		if tn := recv.NamedChild(i).ChildByFieldName("type"); tn != nil {
			return stripTypeDecorations(tn.Content(src))
		}
	}
	return ""
}

func countParamNodes(params *sitter.Node, src []byte) int {
	if params == nil {
		return 0
	}
	if params.Type() == "identifier" { // single-parameter arrow function: x => ...
		return 1
	}
	count := 0
	for i := 0; i < int(params.NamedChildCount()); i++ {
		c := params.NamedChild(i)
		if strings.Contains(c.Type(), "comment") {
			continue
		}
		if c.Type() == "parameter_declaration" { // Go: `a, b int` declares two
			names := 0
			for j := 0; j < int(c.ChildCount()); j++ {
				if c.FieldNameForChild(j) == "name" {
					names++
				}
			}
			if names > 1 {
				count += names
				continue
			}
		}
		count++
	}
	// C: `void f(void)` has one parameter_declaration and zero real parameters.
	if count == 1 && strings.TrimSpace(params.Content(src)) == "(void)" {
		return 0
	}
	return count
}

// calleeInfo extracts the (optional receiver qualifier, method name) from a call
// node. When the receiver looks like a type name (exported identifier or
// self/this/super), qualifier is set so ResolveCalls can prefer matching callees
// whose owner matches. Returns ("", "") when nothing useful is found.
func calleeInfo(n *sitter.Node, src []byte) (qualifier, name string) {
	if n.Type() == "method_invocation" { // Java: obj.method(args)
		if obj := n.ChildByFieldName("object"); obj != nil {
			qualifier = simpleReceiver(obj, src)
		}
		if nm := n.ChildByFieldName("name"); nm != nil {
			name = nm.Content(src)
		}
		return
	}
	fn := n.ChildByFieldName("function")
	for fn != nil {
		switch fn.Type() {
		case "identifier", "field_identifier", "property_identifier":
			name = fn.Content(src)
			return
		case "attribute": // Python obj.method
			if obj := fn.ChildByFieldName("value"); obj != nil {
				qualifier = simpleReceiver(obj, src)
			}
			fn = fn.ChildByFieldName("attribute")
		case "selector_expression": // Go pkg.Func / obj.Method
			if obj := fn.ChildByFieldName("operand"); obj != nil {
				qualifier = simpleReceiver(obj, src)
			}
			fn = fn.ChildByFieldName("field")
		case "field_expression": // Rust / C / C++
			if obj := fn.ChildByFieldName("value"); obj != nil {
				qualifier = simpleReceiver(obj, src)
			}
			if f := fn.ChildByFieldName("field"); f != nil {
				fn = f
			} else {
				fn = fn.ChildByFieldName("value")
			}
		case "member_expression": // JS / TS
			if obj := fn.ChildByFieldName("object"); obj != nil {
				qualifier = simpleReceiver(obj, src)
			}
			fn = fn.ChildByFieldName("property")
		case "scoped_identifier", "qualified_identifier": // Rust path / C++ scope
			if scope := fn.ChildByFieldName("scope"); scope != nil {
				qualifier = scope.Content(src)
			}
			if path := fn.ChildByFieldName("path"); path != nil {
				qualifier = path.Content(src)
			}
			fn = fn.ChildByFieldName("name")
		case "generic_function", "generic_type":
			fn = fn.ChildByFieldName("function")
		case "parenthesized_expression":
			return "", ""
		default:
			return "", ""
		}
	}
	return
}

// simpleReceiver returns the identifier text when a node is a simple identifier
// (i.e. a variable or type name). Self/this/super receivers are excluded because
// they don't constrain the type in the cross-function resolution we do.
func simpleReceiver(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	t := n.Type()
	if t != "identifier" && t != "self" && t != "this" {
		return ""
	}
	name := n.Content(src)
	// Exclude self/this/super — they reference the current receiver, not a named type.
	switch name {
	case "self", "this", "super", "cls", "class":
		return ""
	}
	return name
}

// calleeName extracts the simple name being invoked by a call node (backward
// compat shim over calleeInfo).
func calleeName(n *sitter.Node, src []byte) string {
	_, name := calleeInfo(n, src)
	return name
}

// calleeQualifiedName returns "Qualifier.method" when a qualifier was found,
// otherwise just "method". An empty string means the call is unresolvable.
func calleeQualifiedName(n *sitter.Node, src []byte) string {
	q, name := calleeInfo(n, src)
	if name == "" {
		return ""
	}
	if q != "" && isExported(q) { // only use exported qualifiers — they look like types
		return q + "." + name
	}
	return name
}

// extractFunctionInfos prefers tree-sitter and falls back to regex, attributing
// regex-matched calls to a bounded window after each definition.
func extractFunctionInfos(content, suffix string) []funcInfo {
	if fs, ok := extractFunctionsTreeSitter(content, suffix); ok {
		return fs
	}
	if sp := specFor(suffix); sp != nil {
		return specFunctionInfos(sp, content)
	}
	lines := strings.Split(content, "\n")
	var out []funcInfo
	for _, fm := range extractFunctions(content, suffix) {
		start := fm.start - 1
		if start < 0 {
			start = 0
		}
		end := start + 80
		if end > len(lines) {
			end = len(lines)
		}
		fi := funcInfo{name: fm.name, params: countParams(fm.params), start: fm.start, end: end,
			span: lineSpan(content, fm.start, end)}
		for _, cm := range callRe.FindAllStringSubmatch(strings.Join(lines[start:end], "\n"), -1) {
			if cm[1] != fm.name {
				fi.calls = append(fi.calls, cm[1])
			}
		}
		out = append(out, fi)
	}
	return out
}

// mergeFuncInfos folds definitions sharing a qualified name (overloads) into one.
func mergeFuncInfos(in []funcInfo) []funcInfo {
	idx := map[string]int{}
	var out []funcInfo
	for _, f := range in {
		key := f.qualifiedName()
		if i, ok := idx[key]; ok {
			out[i].calls = append(out[i].calls, f.calls...)
			out[i].refs = append(out[i].refs, f.refs...)
			if f.end > out[i].end {
				out[i].end = f.end
			}
			continue
		}
		idx[key] = len(out)
		out = append(out, f)
	}
	return out
}

func uniqueStrings(in []string, skip string) []string {
	seen := map[string]bool{skip: true}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// lineSpan approximates a span for regex-extracted definitions: from the start
// of startLine to the end of endLine (1-based, inclusive).
func lineSpan(content string, startLine, endLine int) span {
	start, end, line := 0, len(content), 1
	for i := 0; i < len(content); i++ {
		if line == startLine && (i == 0 || content[i-1] == '\n') {
			start = i
		}
		if content[i] == '\n' {
			if line == endLine {
				end = i
				break
			}
			line++
		}
	}
	return span{startByte: start, endByte: end}
}
