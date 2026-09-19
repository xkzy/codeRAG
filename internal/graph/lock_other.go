//go:build !unix

package graph

// lockFile is a no-op where advisory file locks are unavailable; run a single
// process per graph file on those platforms.
func lockFile(path string) (func(), error) { return func() {}, nil }
