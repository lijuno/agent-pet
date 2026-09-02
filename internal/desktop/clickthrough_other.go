//go:build !darwin

package desktop

// Click-through is a macOS feature and this file is why the rest of the package
// still compiles without it. `internal/desktop` builds on Linux — a cloud
// session runs the whole Go suite there — only because every line of
// Objective-C sits behind a _darwin file. See CLAUDE.md.
//
// The geometry in hitregion.go is not stubbed and is tested everywhere: it is
// arithmetic, and the point of ADR 0009's split is that the part worth testing
// does not need a Mac to run.

func startClickThrough()  {}
func stopClickThrough()   {}
func setHitRegion([]rect) {}

func isIgnoringMouse() bool { return false }
