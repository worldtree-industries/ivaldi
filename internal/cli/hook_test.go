package cli_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const hookPath = ".githooks/pre-commit"

func TestInitWritesAnExecutableManagedPreCommitHookThatRunsLintAndTest(t *testing.T) {
	widget, gadget := newProject(t), newProject(t)
	if err := widget.run("github.com/acme/widget\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if err := gadget.run("example.com/gadget\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	hook := widget.read(hookPath)
	if hook != gadget.read(hookPath) {
		t.Errorf("expected the hook to be identical in every Project")
	}
	if !strings.HasPrefix(hook, "#!/bin/sh\n") || !strings.Contains(hook, "Managed by Ivaldi") {
		t.Errorf("expected a Managed sh script, got:\n%s", hook)
	}
	if !strings.Contains(hook, "\nmake lint test\n") {
		t.Errorf("expected the hook to run make lint test, got:\n%s", hook)
	}
	info, err := os.Stat(filepath.Join(widget.dir, hookPath))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 != 0o111 {
		t.Errorf("expected the hook to be executable, got mode %v", info.Mode())
	}
}

func TestTheHookFailsWithTheInstallMessageWhenGolangciLintIsMissing(t *testing.T) {
	p := newProject(t)
	if err := p.run("github.com/acme/widget\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("no sh: %v", err)
	}

	// An empty PATH has no golangci-lint (and no make, so nothing else runs).
	cmd := exec.CommandContext(t.Context(), sh, filepath.FromSlash(hookPath))
	cmd.Dir = p.dir
	cmd.Env = []string{"PATH=" + t.TempDir()}
	out, err := cmd.CombinedOutput()

	if err == nil {
		t.Fatalf("expected the hook to fail without golangci-lint, got:\n%s", out)
	}
	for _, want := range []string{
		"golangci-lint v2",
		"https://golangci-lint.run/welcome/install/",
		"git commit --no-verify",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("expected the hook's message to contain %q, got:\n%s", want, out)
		}
	}
}

const setHooksPath = "git config core.hooksPath .githooks"

func TestInitTurnsOnTheHookInTheProjectsOwnRepository(t *testing.T) {
	for name, tc := range map[string]struct {
		// toplevel returns what git rev-parse --show-toplevel prints, or ""
		// for no repository.
		toplevel func(p *project) string
	}{
		"not a repository":           {toplevel: func(*project) string { return "" }},
		"top level of a repository":  {toplevel: func(p *project) string { return p.dir }},
		"inside a parent repository": {toplevel: func(p *project) string { return filepath.Dir(p.dir) }},
	} {
		t.Run(name, func(t *testing.T) {
			p := newProject(t)
			if toplevel := tc.toplevel(p); toplevel == "" {
				p.runner.fail = map[string]error{showToplevel: errors.New("not a git repository")}
			} else {
				p.script(showToplevel, toplevel+"\n")
			}

			if err := p.run("github.com/acme/widget\n\n", "init"); err != nil {
				t.Fatalf("init failed: %v\noutput:\n%s", err, p.out)
			}

			assertHooksPathSetInOwnRepository(t, p)
			if strings.Contains(p.out.String(), "Warning: "+setHooksPath) {
				t.Errorf("expected no warning, got:\n%s", p.out)
			}
		})
	}
}

// assertHooksPathSetInOwnRepository checks that core.hooksPath was set in the
// Project directory only once it was its own repository's top level: after
// git init, or because it already was.
func assertHooksPathSetInOwnRepository(t *testing.T, p *project) {
	t.Helper()
	lines := make([]string, len(p.runner.requested))
	for i, c := range p.runner.requested {
		if c.Name == "git" && c.Args[0] == "config" && c.Dir != p.dir {
			t.Errorf("expected git config only in the Project directory, got %q in %s", c, c.Dir)
		}
		lines[i] = c.String()
	}
	at := slices.Index(lines, setHooksPath)
	if at < 0 || !p.runner.requestedLine(p.dir, setHooksPath) {
		t.Fatalf("expected %q in the Project directory; requested %+v", setHooksPath, p.runner.requested)
	}
	if gitInit := slices.Index(lines, "git init"); gitInit > at {
		t.Errorf("expected git init before %q, requested %q", setHooksPath, lines)
	}
}

func TestInitWarnsWithTheCommandWhenTurningOnTheHookFails(t *testing.T) {
	p := newProject(t)
	p.inOwnRepo("git@github.com:acme/widget.git\n")
	p.script(adminCheck, "true\n")
	p.runner.fail = map[string]error{setHooksPath: errors.New("could not lock config file")}

	if err := p.run("\nweb\nhttp\n\n\n", "init"); err != nil {
		t.Fatalf("expected init to succeed, got %v\noutput:\n%s", err, p.out)
	}

	out := p.out.String()
	want := "Warning: " + setHooksPath + " failed (could not lock config file). " +
		"To turn on the pre-commit hook, run:\n  " + setHooksPath + "\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected the warning\n%s\ngot:\n%s", want, out)
	}
	p.read(hookPath)
	p.read("cmd/web/main.go")
	if !p.runner.requestedLine(p.dir, modTidy) || !p.runner.requestedLine(p.dir, githubSettings()[0]) {
		t.Errorf("expected init to carry on after the warning, requested %+v", p.runner.requested)
	}
	if !strings.Contains(out, "Done! Nothing has been committed") {
		t.Errorf("expected init to finish, got:\n%s", out)
	}
}
