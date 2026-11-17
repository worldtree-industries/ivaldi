//go:build e2e

package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/worldtree-industries/ivaldi/internal/testtool"
)

// The e2e test drives the real ivaldi binary with real git and go, so it
// needs the network to fetch mjolnir at latest. It catches Templates that
// render but don't compile, or that have drifted from mjolnir's API.

// toolEnv runs the Project's tools with the local toolchain, so a go line
// newer than it fails instead of downloading another.
func toolEnv() []string {
	return append(os.Environ(), "GOTOOLCHAIN=local")
}

// buildIvaldi builds the real ivaldi binary from this package.
func buildIvaldi(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ivaldi")
	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", bin, ".")
	cmd.Env = toolEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building ivaldi failed: %v\n%s", err, out)
	}
	return bin
}

// ivaldi runs the binary in dir with the scripted answers on stdin.
func ivaldi(t *testing.T, bin, dir, input string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), bin, args...)
	cmd.Dir = dir
	cmd.Env = toolEnv()
	cmd.Stdin = strings.NewReader(input)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ivaldi %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// inProject runs a command in the Project and returns its combined output
// and error.
func inProject(t *testing.T, dir, name string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.Dir = dir
	cmd.Env = toolEnv()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestGeneratedProjectWithEveryKindBuildsAndVets(t *testing.T) {
	bin := buildIvaldi(t)
	dir := t.TempDir()

	// No remote, so the module path is asked with no default; no binaries.
	ivaldi(t, bin, dir, "github.com/acme/widget\n\n", "init")
	// web: http, default port, with a client layer. nightly: job, no
	// schedule. cli: tool.
	ivaldi(t, bin, dir, "web, nightly, cli\nhttp\n\ny\njob\n\ntool\n", "add")

	// go build ./... would pass vacuously if a binary were missing.
	for _, name := range []string{"web", "nightly", "cli"} {
		if _, err := os.Stat(filepath.Join(dir, "cmd", name, "main.go")); err != nil {
			t.Fatalf("expected cmd/%s/main.go: %v", name, err)
		}
	}

	for _, args := range [][]string{{"build", "./..."}, {"vet", "./..."}} {
		if out, err := inProject(t, dir, "go", args...); err != nil {
			t.Fatalf("go %s failed in the generated Project: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	// Before the layer-rule check, which leaves a lint failure behind.
	t.Run("the pre-commit hook gates commits", func(t *testing.T) {
		testPreCommitHook(t, dir)
	})

	t.Run("lint enforces the layer rules", func(t *testing.T) {
		lint := testtool.Path(t, "golangci-lint")
		if out, lintErr := inProject(t, dir, lint, "run"); lintErr != nil {
			t.Fatalf("golangci-lint failed on the generated Project: %v\n%s", lintErr, out)
		}

		// A real use, not a blank import: revive reports blank imports on the
		// same line, and golangci-lint keeps one issue per line.
		for path, src := range map[string]string{
			"data/leak.go": "package data\n\n// Rows is a value api must not reach.\nconst Rows = 1\n",
			"api/leak.go": "package api\n\nimport \"github.com/acme/widget/data\"\n\n" +
				"// Rows leaks data into api.\nconst Rows = data.Rows\n",
		} {
			if err := os.WriteFile(filepath.Join(dir, path), []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		out, lintErr := inProject(t, dir, lint, "run")
		if lintErr == nil || !strings.Contains(out, "depguard") ||
			!strings.Contains(out, "github.com/acme/widget/data") {
			t.Errorf("expected depguard to reject api importing data, got %v:\n%s", lintErr, out)
		}
	})
}

// git runs git in the Project and fails the test if it fails.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := inProject(t, dir, "git", args...)
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(out)
}

// testPreCommitHook checks that init turned on the Managed hook, that a clean
// Project commits through it, and that a lint failure blocks the commit. It
// leaves the Project as it found it, apart from the commit.
func testPreCommitHook(t *testing.T, dir string) {
	t.Helper()
	// The hook runs golangci-lint from PATH, so it must be there.
	testtool.Path(t, "golangci-lint")

	if got := git(t, dir, "config", "--local", "core.hooksPath"); got != ".githooks" {
		t.Fatalf("expected init to set core.hooksPath to .githooks, got %q", got)
	}
	// Only this temp repository's config: a commit needs an identity, and the
	// user's own signing setup mustn't get in the way.
	git(t, dir, "config", "--local", "user.name", "Ivaldi e2e")
	git(t, dir, "config", "--local", "user.email", "e2e@example.invalid")
	git(t, dir, "config", "--local", "commit.gpgsign", "false")

	git(t, dir, "add", "-A")
	out, err := inProject(t, dir, "git", "commit", "-q", "-m", "Initial commit")
	if err != nil {
		t.Fatalf("expected the generated Project to commit through the hook: %v\n%s", err, out)
	}
	// make echoes each recipe line, so this shows the hook ran both.
	for _, want := range []string{"golangci-lint run ./...", "go test ./..."} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the commit to run %q through the hook, got:\n%s", want, out)
		}
	}
	head := git(t, dir, "rev-parse", "HEAD")

	// An unused unexported const is a lint failure in every lint config.
	bad := filepath.Join(dir, "models", "unused.go")
	if err = os.WriteFile(bad, []byte("package models\n\nconst unused = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	out, err = inProject(t, dir, "git", "commit", "-q", "-m", "Lint failure")
	if err == nil {
		t.Errorf("expected the hook to block a commit with a lint failure, got:\n%s", out)
	}
	if !strings.Contains(out, "const unused is unused") {
		t.Errorf("expected the lint failure in the commit's output, got:\n%s", out)
	}
	if got := git(t, dir, "rev-parse", "HEAD"); got != head {
		t.Errorf("expected HEAD to stay at %s, got %s", head, got)
	}

	if err = os.Remove(bad); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
}
