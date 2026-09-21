---
name: security-audit
description: Use this skill whenever the user asks to audit code for security vulnerabilities, find dangerous patterns, check for injection flaws, or verify security controls. This skill leverages codeRAG's CWE-based vulnerability detection, data flow tracing for taint analysis, and evidence recording to build a comprehensive security report. Use this skill when the user says "security audit", "find vulnerabilities", "check for injection", "security review", "CWE scan", or "security check".
---

# Security Audit Skill

## Overview

This skill provides a structured approach to security auditing using codeRAG's CWE-based vulnerability detection and taint analysis. Instead of manually reviewing every line, use targeted CWE searches and data flow tracing to find security issues efficiently.

## Workflow

### Step 1: Index the project

```
codergag_index_repository --json '{"project_id": "<project_id>", "path": "<repo_path>"}'
```

### Step 2: Run CWE-based scans

```
codergag_find_vulnerabilities(project_id="<project_id>", cwe="CWE-78")
```
→ OS Command Injection

```
codergag_find_vulnerabilities(project_id="<project_id>", cwe="CWE-89")
```
→ SQL Injection

```
codergag_find_vulnerabilities(project_id="<project_id>", cwe="CWE-79")
```
→ Cross-Site Scripting (XSS)

```
codergag_find_vulnerabilities(project_id="<project_id>", cwe="CWE-73")
```
→ External Control of File Name or Path

```
codergag_find_vulnerabilities(project_id="<project_id>", cwe="CWE-352")
```
→ CSRF

```
codergag_find_vulnerabilities(project_id="<project_id>", cwe="CWE-409")
```
→ Improper Handling of Unicode

```
codergag_audit_security(project_id="<project_id>")
```
→ Full security audit

### Step 3: Taint analysis for critical paths

```
codergag_trace_data_flow(project_id="<project_id>", source_id="<user_input_func>", target_id="<db_query_func>")
```
→ Trace from user input to database query

```
codergag_trace_data_flow(project_id="<project_id>", source_id="<http_handler>", target_id="<file_write>")
```
→ Trace from HTTP handler to file write

### Step 4: Record findings

```
codergag_record_evidence(project_id="<project_id>", subject_id="<node_id>", description="<finding>", kind="security", confidence=0.9, method="static")
```
→ Record security finding as evidence

```
codergag_record_re_hypothesis(project_id="<project_id>", id="<id>", subject_id="<node_id>", claim="<description>", confidence=0.8, status="active", evidence_for=[], evidence_against=[])
```
→ Record security hypothesis

## Common CWE Patterns

| CWE | Description | Severity |
|-----|-------------|----------|
| CWE-78 | OS Command Injection | Critical |
| CWE-89 | SQL Injection | Critical |
| CWE-79 | Cross-Site Scripting | High |
| CWE-73 | External Control of File Path | High |
| CWE-352 | CSRF | Medium |
| CWE-409 | Unicode Handling | Medium |
| CWE-22 | Path Traversal | Critical |
| CWE-502 | Deserialization | Critical |
| CWE-601 | Open Redirect | Medium |
| CWE-327 | Broken Crypto | High |

## Available codeRAG Tools

| Tool | Use Case |
|------|----------|
| `codergag_find_vulnerabilities` | Search by CWE |
| `codergag_audit_security` | Full security audit |
| `codergag_trace_data_flow` | Taint analysis |
| `codergag_get_related_code` | Find related code |
| `codergag_record_evidence` | Record findings |
| `codergag_get_hypotheses` | Review hypotheses |
| `codergag_get_evidence` | Review evidence |

## Output Template

```markdown
# Security Audit Report: <project_name>

## Summary
- **Project**: <project_id>
- **Critical issues**: <count>
- **High issues**: <count>
- **Medium issues**: <count>
- **Files scanned**: <count>

## Critical Findings
### CWE-78: OS Command Injection
- **Location**: <file:line>
- **Function**: <name>
- **Trace**: <data flow path>
- **Remediation**: <suggestion>

## High Findings
### CWE-89: SQL Injection
- ...

## Taint Analysis
| Source | Sink | Path | Risk |
|--------|------|------|------|
| user_input | db_query | <path> | Critical |

## Recommendations
1. <...>
2. <...>
```

## Best Practices

1. **Start with critical CWEs** — CWE-78, CWE-89, CWE-22 first
2. **Trace data flow** — verify user input reaches sinks
3. **Record evidence** — make findings traceable in the graph
4. **Check after fixes** — re-run CWE scans to verify fixes
5. **Cross-reference** — `get_related_code` shows all affected locations
