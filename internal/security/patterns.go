package security

import (
	"regexp"
)

type CWEEntry struct {
	ID          string
	Name        string
	Description string
	Severity    string
}

var CWEDatabase = map[string]CWEEntry{
	"CWE-78":   {"CWE-78", "OS Command Injection", "Improper neutralization of special elements used in an OS command.", "HIGH"},
	"CWE-89":   {"CWE-89", "SQL Injection", "Improper neutralization of special elements used in an SQL command.", "HIGH"},
	"CWE-79":   {"CWE-79", "Cross-site Scripting", "Improper neutralization of input during webpage generation.", "MEDIUM"},
	"CWE-119":  {"CWE-119", "Buffer Overflow", "Improper restriction of operations within the bounds of a memory buffer.", "HIGH"},
	"CWE-125":  {"CWE-125", "Out-of-bounds Read", "Reading from or writing to a buffer using uninitialized or freed resources.", "HIGH"},
	"CWE-704":  {"CWE-704", "Improper Type Conversion", "The software attempts to convert between incompatible types.", "MEDIUM"},
	"CWE-787":  {"CWE-787", "Out-of-bounds Write", "Writing out-of-bounds memory.", "HIGH"},
	"CWE-416":  {"CWE-416", "Use After Free", "Use of reused or expired memory.", "HIGH"},
	"CWE-352":  {"CWE-352", "CSRF", "Cross-Site Request Forgery.", "MEDIUM"},
	"CWE-287":  {"CWE-287", "Authentication Bypass", "Improper authentication.", "HIGH"},
	"CWE-502":  {"CWE-502", "Deserialization of Untrusted Data", "Deserialization of untrusted data.", "HIGH"},
	"CWE-22":   {"CWE-22", "Path Traversal", "Improper restriction of pathnames.", "MEDIUM"},
	"CWE-732":  {"CWE-732", "Incorrect Permission Assignment", "Weak permissions.", "MEDIUM"},
	"CWE-190":  {"CWE-190", "Integer Overflow", "Integer overflow or wraparound.", "MEDIUM"},
	"CWE-20":   {"CWE-20", "Improper Input Validation", "Improper input validation.", "MEDIUM"},
	"CWE-400":  {"CWE-400", "Uncontrolled Resource Consumption", "Uncontrolled resource consumption.", "MEDIUM"},
	"CWE-306":  {"CWE-306", "Missing Authentication for Critical Function", "Missing authentication.", "HIGH"},
	"CWE-862":  {"CWE-862", "Missing Authorization", "Missing authorization.", "MEDIUM"},
	"CWE-1333": {"CWE-1333", "Inefficient Regular Expression", "ReDoS.", "MEDIUM"},
	"CWE-665":  {"CWE-665", "Improper Initialization", "Improper initialization.", "MEDIUM"},
}

func Lookup(cwe string) (CWEEntry, bool) {
	entry, ok := CWEDatabase[cwe]
	return entry, ok
}

func AllCWEs() []CWEEntry {
	result := make([]CWEEntry, 0, len(CWEDatabase))
	for _, e := range CWEDatabase {
		result = append(result, e)
	}
	return result
}

type Pattern struct {
	CWE        string
	Language   string
	Name       string
	Pattern    *regexp.Regexp
	Confidence float64
}

var Patterns = []Pattern{
	{
		CWE:        "CWE-78",
		Language:   "python",
		Name:       "subprocess with shell=True",
		Pattern:    regexp.MustCompile(`(?i)subprocess\.(call|run|Popen|check_output)\(.*shell\s*=\s*True`),
		Confidence: 0.9,
	},
	{
		CWE:        "CWE-78",
		Language:   "c",
		Name:       "system() call",
		Pattern:    regexp.MustCompile(`(?i)\bsystem\s*\(`),
		Confidence: 0.8,
	},
	{
		CWE:        "CWE-78",
		Language:   "c",
		Name:       "popen() call",
		Pattern:    regexp.MustCompile(`(?i)\bpopen\s*\(`),
		Confidence: 0.8,
	},
	{
		CWE:        "CWE-89",
		Language:   "python",
		Name:       "SQL string formatting",
		Pattern:    regexp.MustCompile(`(?i)(execute|cursor\.(execute|executemany))\s*\(\s*(["'].*%[sdb].*["']|f["'].*SELECT|f["'].*INSERT|f["'].*UPDATE|f["'].*DELETE)`),
		Confidence: 0.85,
	},
	{
		CWE:        "CWE-89",
		Language:   "go",
		Name:       "SQL string formatting",
		Pattern:    regexp.MustCompile(`(?i) ExecContext\s*\(\s*.*fmt\.Sprintf| QueryContext\s*\(\s*.*fmt\.Sprintf`),
		Confidence: 0.85,
	},
	{
		CWE:        "CWE-89",
		Language:   "c",
		Name:       "sprintf in SQL",
		Pattern:    regexp.MustCompile(`(?i)sprintf.*SELECT|sprintf.*INSERT|sprintf.*UPDATE|sprintf.*DELETE`),
		Confidence: 0.7,
	},
	{
		CWE:        "CWE-79",
		Language:   "python",
		Name:       "mark_safe on user input",
		Pattern:    regexp.MustCompile(`(?i)mark_safe\s*\(`),
		Confidence: 0.7,
	},
	{
		CWE:        "CWE-119",
		Language:   "c",
		Name:       "strcpy without length",
		Pattern:    regexp.MustCompile(`(?i)\bstrcpy\s*\(`),
		Confidence: 0.85,
	},
	{
		CWE:        "CWE-119",
		Language:   "c",
		Name:       "strcat without length",
		Pattern:    regexp.MustCompile(`(?i)\bstrcat\s*\(`),
		Confidence: 0.85,
	},
	{
		CWE:        "CWE-119",
		Language:   "c",
		Name:       "sprintf without snprintf",
		Pattern:    regexp.MustCompile(`(?i)\bsprintf\s*\(`),
		Confidence: 0.75,
	},
	{
		CWE:        "CWE-119",
		Language:   "c",
		Name:       "gets() call",
		Pattern:    regexp.MustCompile(`(?i)\bgets\s*\(`),
		Confidence: 0.95,
	},
	{
		CWE:        "CWE-119",
		Language:   "c",
		Name:       "scanf without width limit",
		Pattern:    regexp.MustCompile(`(?i)\bscanf\s*\(\s*["'][^"']*%[^"']*["']`),
		Confidence: 0.7,
	},
	{
		CWE:        "CWE-125",
		Language:   "c",
		Name:       "array access without bounds check",
		Pattern:    regexp.MustCompile(`(?i)\barr\[[^\]]+\]`),
		Confidence: 0.5,
	},
	{
		CWE:        "CWE-787",
		Language:   "c",
		Name:       "memcpy without length check",
		Pattern:    regexp.MustCompile(`(?i)\bmemcpy\s*\(`),
		Confidence: 0.6,
	},
	{
		CWE:        "CWE-416",
		Language:   "c",
		Name:       "free followed by use",
		Pattern:    regexp.MustCompile(`(?i)\bfree\s*\(`),
		Confidence: 0.3,
	},
	{
		CWE:        "CWE-352",
		Language:   "python",
		Name:       "missing CSRF token",
		Pattern:    regexp.MustCompile(`(?i)@app\.route\([^)]*\)\s*\n\s*def\s+\w+\([^)]*\)\s*:\s*\n\s*[^@]*\b(request\.(post|get|put|delete|patch))`),
		Confidence: 0.5,
	},
	{
		CWE:        "CWE-22",
		Language:   "python",
		Name:       "path traversal from user input",
		Pattern:    regexp.MustCompile(`(?i)open\s*\(\s*.*request\.`),
		Confidence: 0.8,
	},
	{
		CWE:        "CWE-22",
		Language:   "c",
		Name:       "path traversal fopen",
		Pattern:    regexp.MustCompile(`(?i)\bfopen\s*\(\s*.*getenv`),
		Confidence: 0.6,
	},
	{
		CWE:        "CWE-502",
		Language:   "python",
		Name:       "pickle load",
		Pattern:    regexp.MustCompile(`(?i)\bpickle\.(loads|load)\s*\(`),
		Confidence: 0.9,
	},
	{
		CWE:        "CWE-502",
		Language:   "python",
		Name:       "yaml load without safe",
		Pattern:    regexp.MustCompile(`(?i)\byaml\.load\s*\(`),
		Confidence: 0.8,
	},
	{
		CWE:        "CWE-190",
		Language:   "c",
		Name:       "integer overflow potential",
		Pattern:    regexp.MustCompile(`(?i)\bmalloc\s*\(\s*[^)]*[\+\*]`),
		Confidence: 0.5,
	},
	{
		CWE:        "CWE-20",
		Language:   "python",
		Name:       "eval of user input",
		Pattern:    regexp.MustCompile(`(?i)\beval\s*\(`),
		Confidence: 0.9,
	},
	{
		CWE:        "CWE-20",
		Language:   "python",
		Name:       "exec of user input",
		Pattern:    regexp.MustCompile(`(?i)\bexec\s*\(`),
		Confidence: 0.8,
	},
	{
		CWE:        "CWE-1333",
		Language:   "all",
		Name:       "potentially evil regex",
		Pattern:    regexp.MustCompile(`\([^)]*[+*][^)]*\)[+*]`),
		Confidence: 0.4,
	},
	{
		CWE:        "CWE-306",
		Language:   "c",
		Name:       "main without argument validation",
		Pattern:    regexp.MustCompile(`(?i)\bint\s+main\s*\(\s*int\s+argc`),
		Confidence: 0.2,
	},
	{
		CWE:        "CWE-352",
		Language:   "javascript",
		Name:       "fetch without credentials mode",
		Pattern:    regexp.MustCompile(`(?i)\bfetch\s*\(`),
		Confidence: 0.3,
	},
	{
		CWE:        "CWE-862",
		Language:   "python",
		Name:       "no authentication decorator",
		Pattern:    regexp.MustCompile(`(?i)@app\.route\([^)]*methods\s*=\s*\["?POST"?\s*,\s*"GET"?\s*\]\)`),
		Confidence: 0.4,
	},
}

func MatchPatterns(content, language string) []*Pattern {
	var matched []*Pattern
	for i := range Patterns {
		p := &Patterns[i]
		if p.Language != "all" && p.Language != language {
			continue
		}
		if p.Pattern.MatchString(content) {
			matched = append(matched, p)
		}
	}
	return matched
}
