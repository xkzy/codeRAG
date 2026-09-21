// Tool: exports security patterns to JSON for online distribution.
// Run: go run cmd/export_patterns.go
package main

import (
	"fmt"
	"os"

	"codergag/internal/security"
)

func main() {
	data, err := security.ExportPatternsJSON()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Println(string(data))
}
