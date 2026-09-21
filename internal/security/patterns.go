package security

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
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
	"CWE-798":  {"CWE-798", "Use of Hardcoded Credentials", "Hardcoded credentials.", "HIGH"},
	"CWE-259":  {"CWE-259", "Use of Hardcoded Password", "Hardcoded password.", "MEDIUM"},
	"CWE-918":  {"CWE-918", "Server-Side Request Forgery", "SSRF.", "HIGH"},
	"CWE-327":  {"CWE-327", "Use of a Broken or Risky Cryptographic Algorithm", "Weak crypto.", "MEDIUM"},
	"CWE-326":  {"CWE-326", "Inadequate Encryption Strength", "Weak encryption.", "MEDIUM"},
	"CWE-639":  {"CWE-639", "Insecure Direct Object Reference", "IDOR.", "MEDIUM"},
	"CWE-611":  {"CWE-611", "Improper Restriction of XML External Entity Reference", "XXE.", "MEDIUM"},
	"CWE-1021": {"CWE-1021", "Improper Restriction of Rendered UI Layers or Frames", "Clickjacking.", "MEDIUM"},
	"CWE-476":  {"CWE-476", "NULL Pointer Dereference", "NULL pointer dereference.", "MEDIUM"},
	"CWE-703":  {"CWE-703", "Improper Check for Unusual or Exceptional Conditions", "Error handling.", "LOW"},
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
	// ---- OS Command Injection (CWE-78) ----
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
		CWE:        "CWE-78",
		Language:   "go",
		Name:       "exec.Command with shell",
		Pattern:    regexp.MustCompile(`(?i)exec\.Command\s*\(\s*["'](/bin/sh|/bin/bash|cmd)["']`),
		Confidence: 0.7,
	},
	{
		CWE:        "CWE-78",
		Language:   "go",
		Name:       "os.system equivalent",
		Pattern:    regexp.MustCompile(`(?i)exec\.Command\s*\(\s*["']sh["']\s*,\s*["']-c`),
		Confidence: 0.8,
	},
	{
		CWE:        "CWE-78",
		Language:   "javascript",
		Name:       "child_process exec with shell",
		Pattern:    regexp.MustCompile(`(?i)require\s*\(\s*["']child_process["']\s*\)\.exec\s*\(`),
		Confidence: 0.7,
	},

	// ---- SQL Injection (CWE-89) ----
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
		Pattern:    regexp.MustCompile(`(?i)\b(Exec|Query)(.*Context)?\s*\(\s*.*fmt\.Sprintf`),
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
		CWE:        "CWE-89",
		Language:   "java",
		Name:       "string concatenation in SQL",
		Pattern:    regexp.MustCompile(`(?i)(Statement|PreparedStatement)\s*\.\s*(execute|prepare)\s*\([^)]*\+`),
		Confidence: 0.6,
	},
	{
		CWE:        "CWE-89",
		Language:   "javascript",
		Name:       "SQL string concatenation",
		Pattern:    regexp.MustCompile(`(?i)(query|execute)\s*\(\s*['"].*\+.*[+\s].*['"]`),
		Confidence: 0.5,
	},
	{
		CWE:        "CWE-89",
		Language:   "php",
		Name:       "SQL injection in PHP",
		Pattern:    regexp.MustCompile(`(?i)(mysql_query|mysqli::query)\s*\(\s*['"].*\$_(GET|POST|REQUEST)`),
		Confidence: 0.9,
	},

	// ---- Cross-site Scripting (CWE-79) ----
	{
		CWE:        "CWE-79",
		Language:   "python",
		Name:       "mark_safe on user input",
		Pattern:    regexp.MustCompile(`(?i)mark_safe\s*\(`),
		Confidence: 0.7,
	},
	{
		CWE:        "CWE-79",
		Language:   "python",
		Name:       "render_to_string with request",
		Pattern:    regexp.MustCompile(`(?i)render_to_string\s*\([^)]*request`),
		Confidence: 0.6,
	},
	{
		CWE:        "CWE-79",
		Language:   "javascript",
		Name:       "dangerouslySetInnerHTML",
		Pattern:    regexp.MustCompile(`(?i)dangerouslySetInnerHTML`),
		Confidence: 0.4,
	},
	{
		CWE:        "CWE-79",
		Language:   "javascript",
		Name:       "innerHTML assignment",
		Pattern:    regexp.MustCompile(`(?i)\.innerHTML\s*=`),
		Confidence: 0.5,
	},
	{
		CWE:        "CWE-79",
		Language:   "java",
		Name:       "unescaped output in JSP",
		Pattern:    regexp.MustCompile(`<%[^>]*<%=.*request\.getParameter`),
		Confidence: 0.6,
	},

	// ---- Buffer Overflows (CWE-119, CWE-125, CWE-787) ----
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

	// ---- Path Traversal (CWE-22) ----
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
		CWE:        "CWE-22",
		Language:   "go",
		Name:       "path traversal with user input",
		Pattern:    regexp.MustCompile(`(?i)os\.Open\s*\(\s*.*request\.|os\.OpenFile\s*\(\s*.*user`),
		Confidence: 0.5,
	},
	{
		CWE:        "CWE-22",
		Language:   "javascript",
		Name:       "path traversal in require",
		Pattern:    regexp.MustCompile(`(?i)require\s*\(\s*.*req\.params|require\s*\(\s*.*req\.query`),
		Confidence: 0.4,
	},
	{
		CWE:        "CWE-22",
		Language:   "php",
		Name:       "path traversal include",
		Pattern:    regexp.MustCompile(`(?i)(include|require)\s*\(\s*\$_(GET|POST|REQUEST|COOKIE)`),
		Confidence: 0.7,
	},

	// ---- Deserialization of Untrusted Data (CWE-502) ----
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
		CWE:        "CWE-502",
		Language:   "python",
		Name:       "yaml unsafe load_all",
		Pattern:    regexp.MustCompile(`(?i)yaml\.load_all\s*\(`),
		Confidence: 0.7,
	},
	{
		CWE:        "CWE-502",
		Language:   "java",
		Name:       "Java deserialization",
		Pattern:    regexp.MustCompile(`(?i)(ObjectInputStream|readObject)\s*\(`),
		Confidence: 0.7,
	},
	{
		CWE:        "CWE-502",
		Language:   "javascript",
		Name:       "unsafe deserialization",
		Pattern:    regexp.MustCompile(`(?i)(eval\(|Function\(|deserialize)`),
		Confidence: 0.6,
	},

	// ---- Integer Overflow (CWE-190) ----
	{
		CWE:        "CWE-190",
		Language:   "c",
		Name:       "integer overflow potential",
		Pattern:    regexp.MustCompile(`(?i)\bmalloc\s*\(\s*[^)]*[\+\*]`),
		Confidence: 0.5,
	},
	{
		CWE:        "CWE-190",
		Language:   "rust",
		Name:       "unsafe arithmetic",
		Pattern:    regexp.MustCompile(`(?i)unchecked_(add|sub|mul|div|rem)`),
		Confidence: 0.6,
	},

	// ---- Improper Input Validation (CWE-20) ----
	{
		CWE:        "CWE-20",
		Language:   "python",
		Name:       "eval() call",
		Pattern:    regexp.MustCompile(`(?i)\beval\s*\(`),
		Confidence: 0.9,
	},
	{
		CWE:        "CWE-20",
		Language:   "python",
		Name:       "exec() call",
		Pattern:    regexp.MustCompile(`(?i)\bexec\s*\(`),
		Confidence: 0.8,
	},
	{
		CWE:        "CWE-20",
		Language:   "javascript",
		Name:       "eval() call",
		Pattern:    regexp.MustCompile(`(?i)\beval\s*\(`),
		Confidence: 0.7,
	},
	{
		CWE:        "CWE-20",
		Language:   "php",
		Name:       "eval() call",
		Pattern:    regexp.MustCompile(`(?i)\beval\s*\(`),
		Confidence: 0.8,
	},

	// ---- ReDoS (CWE-1333) ----
	{
		CWE:        "CWE-1333",
		Language:   "all",
		Name:       "potentially evil regex",
		Pattern:    regexp.MustCompile(`\([^)]*[+*][^)]*\)[+*]`),
		Confidence: 0.4,
	},
	{
		CWE:        "CWE-1333",
		Language:   "javascript",
		Name:       "evil regex in JavaScript",
		Pattern:    regexp.MustCompile(`(?i)(RegExp|\.match|\.replace|\.split)\s*\(\s*["'][^"']*[^\]"']*[+*].*[+*].*["']`),
		Confidence: 0.5,
	},

	// ---- Missing Authentication / CSRF (CWE-306, CWE-862, CWE-352) ----
	{
		CWE:        "CWE-306",
		Language:   "c",
		Name:       "main without argument validation",
		Pattern:    regexp.MustCompile(`(?i)\bint\s+main\s*\(\s*int\s+argc`),
		Confidence: 0.2,
	},
	{
		CWE:        "CWE-352",
		Language:   "python",
		Name:       "missing CSRF token",
		Pattern:    regexp.MustCompile(`(?i)@app\.route\([^)]*\)\s*\n\s*def\s+\w+\([^)]*\)\s*:\s*\n\s*[^@]*\b(request\.(post|get|put|delete|patch))`),
		Confidence: 0.5,
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
	{
		CWE:        "CWE-862",
		Language:   "go",
		Name:       "HTTP handler without auth",
		Pattern:    regexp.MustCompile(`(?i)http\.HandleFunc\s*\(\s*["'][^"']*["']\s*,\s*func\s*\(`),
		Confidence: 0.4,
	},

	// ---- Hardcoded Credentials (CWE-798, CWE-259) ----
	{
		CWE:        "CWE-798",
		Language:   "all",
		Name:       "hardcoded password string",
		Pattern:    regexp.MustCompile(`(?i)(password|passwd|pwd)\s*[:=]\s*["'][^"'<>]{3,}["']`),
		Confidence: 0.9,
	},
	{
		CWE:        "CWE-798",
		Language:   "all",
		Name:       "hardcoded API key",
		Pattern:    regexp.MustCompile(`(?i)(api_key|apikey|secret|access_token)\s*[:=]\s*["'][^"'<>]{8,}["']`),
		Confidence: 0.8,
	},
	{
		CWE:        "CWE-798",
		Language:   "python",
		Name:       "django secret key hardcoded",
		Pattern:    regexp.MustCompile(`(?i)SECRET_KEY\s*=\s*["'].*["']`),
		Confidence: 0.9,
	},
	{
		CWE:        "CWE-798",
		Language:   "go",
		Name:       "hardcoded credential",
		Pattern:    regexp.MustCompile(`(?i)("password"|"secret")\s*:\s*["'][^"'<>]{3,}["']`),
		Confidence: 0.7,
	},
	{
		CWE:        "CWE-259",
		Language:   "all",
		Name:       "hardcoded username/password",
		Pattern:    regexp.MustCompile(`(?i)(username|user)\s*[:=]\s*["'](admin|root|test|guest)["']`),
		Confidence: 0.6,
	},

	// ---- SSRF (CWE-918) ----
	{
		CWE:        "CWE-918",
		Language:   "python",
		Name:       "requests to user-controlled URL",
		Pattern:    regexp.MustCompile(`(?i)requests\.(get|post|put|delete)\s*\(\s*.*request\.`),
		Confidence: 0.7,
	},
	{
		CWE:        "CWE-918",
		Language:   "go",
		Name:       "HTTP request to user URL",
		Pattern:    regexp.MustCompile(`(?i)http\.Get\s*\(\s*.*request\.|http\.Post\s*\(\s*.*request\.`),
		Confidence: 0.6,
	},
	{
		CWE:        "CWE-918",
		Language:   "javascript",
		Name:       "fetch to user-controlled URL",
		Pattern:    regexp.MustCompile(`(?i)fetch\s*\(\s*.*\$\{|fetch\s*\(\s*.*req\.`),
		Confidence: 0.6,
	},
	{
		CWE:        "CWE-918",
		Language:   "java",
		Name:       "URL open from user input",
		Pattern:    regexp.MustCompile(`(?i)new\s+(URL|HttpURLConnection)\s*\(\s*.*(request|param)`),
		Confidence: 0.6,
	},

	// ---- Weak Cryptography (CWE-327, CWE-326) ----
	{
		CWE:        "CWE-327",
		Language:   "python",
		Name:       "MD5 hash usage",
		Pattern:    regexp.MustCompile(`(?i)\bmd5\s*\(`),
		Confidence: 0.6,
	},
	{
		CWE:        "CWE-327",
		Language:   "python",
		Name:       "SHA1 hash usage",
		Pattern:    regexp.MustCompile(`(?i)\bsha1\s*\(`),
		Confidence: 0.5,
	},
	{
		CWE:        "CWE-327",
		Language:   "python",
		Name:       "DES/RC4 cipher usage",
		Pattern:    regexp.MustCompile(`(?i)\b(des|rc4|arc4|ecb)\b`),
		Confidence: 0.7,
	},
	{
		CWE:        "CWE-327",
		Language:   "java",
		Name:       "weak cipher usage",
		Pattern:    regexp.MustCompile(`(?i)(MessageDigest|Cipher)\s*\.\s*(getInstance)\s*\(\s*["'](md5|des|rc4|aes/ecb)`),
		Confidence: 0.8,
	},
	{
		CWE:        "CWE-327",
		Language:   "go",
		Name:       "MD5/SHA1 hash usage",
		Pattern:    regexp.MustCompile(`(?i)md5\.Sum|sha1\.Sum|md5\.New|sha1\.New`),
		Confidence: 0.5,
	},
	{
		CWE:        "CWE-327",
		Language:   "javascript",
		Name:       "crypto.createCipher",
		Pattern:    regexp.MustCompile(`(?i)createCipher\s*\(`),
		Confidence: 0.6,
	},
	{
		CWE:        "CWE-326",
		Language:   "c",
		Name:       "insecure random",
		Pattern:    regexp.MustCompile(`(?i)\brand\s*\(`),
		Confidence: 0.4,
	},

	// ---- Insecure Direct Object Reference (CWE-639) ----
	{
		CWE:        "CWE-639",
		Language:   "python",
		Name:       "user ID without authorization",
		Pattern:    regexp.MustCompile(`(?i)User\.objects\.get\s*\(\s*id\s*=\s*request\.`),
		Confidence: 0.6,
	},
	{
		CWE:        "CWE-639",
		Language:   "javascript",
		Name:       "direct DB query with user ID",
		Pattern:    regexp.MustCompile(`(?i)findById\s*\(\s*req\.params`),
		Confidence: 0.5,
	},

	// ---- Uncontrolled Resource Consumption (CWE-400) ----
	{
		CWE:        "CWE-400",
		Language:   "javascript",
		Name:       "large JSON parse",
		Pattern:    regexp.MustCompile(`(?i)JSON\.parse\s*\(\s*.*req\.(body|query)`),
		Confidence: 0.4,
	},

	// ---- C/C++ Memory Safety ----
	{
		CWE:        "CWE-476",
		Language:   "c",
		Name:       "NULL pointer dereference",
		Pattern:    regexp.MustCompile(`(?i)\*\s*\(?\w+\s*\)`),
		Confidence: 0.3,
	},

	// ---- Rust ----
	{
		CWE:        "CWE-119",
		Language:   "rust",
		Name:       "unsafe block",
		Pattern:    regexp.MustCompile(`(?i)\bunsafe\b\s*\{`),
		Confidence: 0.4,
	},

	// ---- Cross-pattern (all languages) ----
	{
		CWE:        "CWE-326",
		Language:   "all",
		Name:       "insecure random number generator",
		Pattern:    regexp.MustCompile(`(?i)\brand\s*\(\s*\)\s*%\s*\d+`),
		Confidence: 0.5,
	},
	{
		CWE:        "CWE-1004",
		Language:   "all",
		Name:       "insecure SSL/TLS",
		Pattern:    regexp.MustCompile(`(?i)(ssl_verifypeer|verify_ssl|sslversion)\s*=\s*(false|0|SSLv2|SSLv3)`),
		Confidence: 0.8,
	},
	{
		CWE:        "CWE-611",
		Language:   "all",
		Name:       "XXE: XML external entity",
		Pattern:    regexp.MustCompile(`(?i)(XMLParser|SimpleXMLElement|DOMDocument)\s*.*(LOAD_DTD|loadXml).*entity`),
		Confidence: 0.6,
	},
	{
		CWE:        "CWE-1021",
		Language:   "all",
		Name:       "insecure redirect",
		Pattern:    regexp.MustCompile(`(?i)(redirect|location)\s*[:=]\s*.*req(uest|\.params)`),
		Confidence: 0.5,
	},
}

func MatchPatterns(content, language string) []*Pattern {
	return matchPatterns(content, language, Patterns)
}

func matchPatterns(content, language string, patterns []Pattern) []*Pattern {
	var matched []*Pattern
	for i := range patterns {
		p := &patterns[i]
		if p.Language != "all" && p.Language != language {
			continue
		}
		if p.Pattern.MatchString(content) {
			matched = append(matched, p)
		}
	}
	return matched
}

// SerializablePattern is the JSON representation of a Pattern, used for
// online pattern database updates.
type SerializablePattern struct {
	CWE        string  `json:"cwe"`
	Language   string  `json:"language"`
	Name       string  `json:"name"`
	Regex      string  `json:"regex"`
	Confidence float64 `json:"confidence"`
}

// PatternSource is the online URL for the pattern database.
// This can be independently updated without rebuilding codeRAG.
var PatternSource = "https://raw.githubusercontent.com/kilocode-org/codergag/main/security/patterns.json"

// PatternCacheDir is where downloaded patterns are cached locally.
var PatternCacheDir = filepath.Join(os.TempDir(), "codergag-patterns")

// PatternUpdateTime tracks when patterns were last fetched.
var patternUpdateTime time.Time

// PatternUpdateMu protects concurrent pattern updates.
var PatternUpdateMu sync.RWMutex

// PatternUpdateInterval is the TTL for cached patterns.
// After this duration, patterns will be re-fetched from the server.
var PatternUpdateInterval = 24 * time.Hour

// patternsFetched tracks whether patterns have been fetched at least once.
var patternsFetched bool

// ShouldUpdate checks if patterns are stale and need re-fetching.
func ShouldUpdate() bool {
	PatternUpdateMu.RLock()
	defer PatternUpdateMu.RUnlock()
	if patternUpdateTime.IsZero() {
		return true
	}
	return time.Since(patternUpdateTime) > PatternUpdateInterval
}

// CheckPatternUpdate fetches new patterns if the cache is stale.
// Safe to call from any goroutine. Does nothing if fetch fails.
func CheckPatternUpdate() bool {
	if !ShouldUpdate() {
		return false
	}
	count, err := UpdatePatternsOnline()
	if err != nil {
		// Fallback to cached patterns
		if cached, err := LoadCachedPatterns(); err == nil && len(cached) > 0 {
			PatternUpdateMu.Lock()
			Patterns = cached
			PatternUpdateMu.Unlock()
		}
		return false
	}
	return count > 0
}

// StartAutoUpdater starts a background goroutine that periodically checks
// for pattern updates. Call this once at application startup.
// The goroutine runs until the application exits.
func StartAutoUpdater(interval time.Duration) {
	if interval <= 0 {
		interval = PatternUpdateInterval
	}
	go func() {
		// Initial fetch with a short delay
		time.Sleep(1 * time.Second)
		CheckPatternUpdate()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			CheckPatternUpdate()
		}
	}()
}

// UpdatePatternsOnline fetches the latest pattern database from the configured
// URL. The downloaded patterns replace the built-in Patterns. Returns the
// number of new patterns loaded.
func UpdatePatternsOnline() (int, error) {
	return UpdatePatternsOnlineWithClient(http.DefaultClient)
}

// UpdatePatternsOnlineWithClient allows injecting a custom HTTP client
// (useful for testing with httptest).
func UpdatePatternsOnlineWithClient(client *http.Client) (int, error) {
	PatternUpdateMu.Lock()
	defer PatternUpdateMu.Unlock()

	resp, err := client.Get(PatternSource)
	if err != nil {
		return 0, fmt.Errorf("fetch patterns: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("pattern server returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("read patterns: %w", err)
	}

	var serialPatterns []SerializablePattern
	if err := json.Unmarshal(body, &serialPatterns); err != nil {
		return 0, fmt.Errorf("parse patterns: %w", err)
	}

	var newPatterns []Pattern
	for _, sp := range serialPatterns {
		re, err := regexp.Compile(sp.Regex)
		if err != nil {
			continue
		}
		newPatterns = append(newPatterns, Pattern{
			CWE:        sp.CWE,
			Language:   sp.Language,
			Name:       sp.Name,
			Pattern:    re,
			Confidence: sp.Confidence,
		})
	}

	Patterns = newPatterns
	patternUpdateTime = time.Now().UTC()
	patternsFetched = true

	// Cache locally
	if err := os.MkdirAll(PatternCacheDir, 0o755); err == nil {
		cachePath := filepath.Join(PatternCacheDir, "patterns.json")
		os.WriteFile(cachePath, body, 0o644)
	}

	return len(newPatterns), nil
}

// LoadCachedPatterns loads patterns from the local cache file.
func LoadCachedPatterns() ([]Pattern, error) {
	cachePath := filepath.Join(PatternCacheDir, "patterns.json")
	body, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, err
	}
	var serialPatterns []SerializablePattern
	if err := json.Unmarshal(body, &serialPatterns); err != nil {
		return nil, err
	}
	var patterns []Pattern
	for _, sp := range serialPatterns {
		re, err := regexp.Compile(sp.Regex)
		if err != nil {
			continue
		}
		patterns = append(patterns, Pattern{
			CWE:        sp.CWE,
			Language:   sp.Language,
			Name:       sp.Name,
			Pattern:    re,
			Confidence: sp.Confidence,
		})
	}
	return patterns, nil
}

// PatternsUpdateTime returns when patterns were last updated online.
func PatternsUpdateTime() time.Time {
	PatternUpdateMu.RLock()
	defer PatternUpdateMu.RUnlock()
	return patternUpdateTime
}

// SetPatternUpdateTime is exported for testing purposes.
func SetPatternUpdateTime(t time.Time) {
	patternUpdateTime = t
	patternsFetched = !t.IsZero()
}

// ExportPatternsJSON serializes all current patterns to JSON.
func ExportPatternsJSON() ([]byte, error) {
	PatternUpdateMu.RLock()
	defer PatternUpdateMu.RUnlock()

	var out []SerializablePattern
	for _, p := range Patterns {
		out = append(out, SerializablePattern{
			CWE:        p.CWE,
			Language:   p.Language,
			Name:       p.Name,
			Regex:      p.Pattern.String(),
			Confidence: p.Confidence,
		})
	}
	return json.MarshalIndent(out, "", "  ")
}
