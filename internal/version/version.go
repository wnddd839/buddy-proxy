// Package version holds release metadata injected at link time.
package version

// Version is the semantic release tag (e.g. v4.6).
var Version = "dev"

// Commit is the git revision when built with -ldflags.
var Commit = ""

// BuiltAt is an RFC3339 UTC timestamp when the binary was built.
var BuiltAt = ""

// Info returns a small map for JSON admin APIs.
func Info() map[string]string {
	out := map[string]string{"version": Version}
	if Commit != "" {
		out["commit"] = Commit
	}
	if BuiltAt != "" {
		out["builtAt"] = BuiltAt
	}
	return out
}
