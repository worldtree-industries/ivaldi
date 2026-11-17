// Package testtool finds the external linters (golangci-lint, actionlint) that
// some tests run. Locally a missing linter skips the test; CI sets RequireEnv
// so a missing linter fails it instead of passing silently.
package testtool

import (
	"os"
	"os/exec"
	"testing"
)

// RequireEnv is the environment variable that, set to 1, makes a missing
// linter fail the test instead of skipping it.
const RequireEnv = "IVALDI_REQUIRE_LINTERS"

// Path returns the path to the named tool on PATH. If it isn't there, it skips
// the test, or fails it when RequireEnv is 1.
func Path(t testing.TB, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err == nil {
		return path
	}
	if os.Getenv(RequireEnv) == "1" {
		t.Fatalf("%s not on PATH, and %s=1 requires it: %v", name, RequireEnv, err)
	}
	t.Skipf("%s not on PATH (set %s=1 to fail instead)", name, RequireEnv)
	return ""
}
