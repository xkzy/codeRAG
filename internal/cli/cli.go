package cli

import (
	"encoding/json"
	"io"
	"os"
)

// PrintJSON writes v as indented JSON to w. It is the single shared helper for
// every codergag subcommand that supports --json, so formatting cannot drift
// between commands.
func PrintJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		// Fall back to stderr; the caller's exit code is unaffected.
		os.Stderr.WriteString("json: " + err.Error() + "\n")
	}
}

// Stdout is a convenience for the common case of writing to stdout.
func Stdout(v any) {
	PrintJSON(os.Stdout, v)
}