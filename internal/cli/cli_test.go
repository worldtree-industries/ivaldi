package cli_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/worldtree-industries/ivaldi/internal/cli"
)

// fakeRunner records every command requested and fails the ones scripted to.
type fakeRunner struct {
	requested []cli.Command
	fail      map[string]error // keyed by Command.String()
}

func (f *fakeRunner) Run(_ context.Context, cmd cli.Command) ([]byte, error) {
	f.requested = append(f.requested, cmd)
	return nil, f.fail[cmd.String()]
}

func (f *fakeRunner) requestedLine(dir, line string) bool {
	return slices.ContainsFunc(f.requested, func(c cli.Command) bool {
		return c.Dir == dir && c.String() == line
	})
}

type project struct {
	t      *testing.T
	dir    string
	runner *fakeRunner
	out    *bytes.Buffer
}

func newProject(t *testing.T) *project {
	t.Helper()
	return &project{t: t, dir: t.TempDir(), runner: &fakeRunner{}, out: &bytes.Buffer{}}
}

func (p *project) run(input string, args ...string) error {
	p.t.Helper()
	return cli.Run(context.Background(), cli.Env{
		Dir:    p.dir,
		In:     strings.NewReader(input),
		Out:    p.out,
		Runner: p.runner,
	}, args)
}

func (p *project) read(rel string) string {
	p.t.Helper()
	b, err := os.ReadFile(filepath.Join(p.dir, rel))
	if err != nil {
		p.t.Fatalf("expected %s to exist: %v", rel, err)
	}
	return string(b)
}

func (p *project) write(rel, content string) {
	p.t.Helper()
	path := filepath.Join(p.dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		p.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		p.t.Fatal(err)
	}
}

func TestInitScaffoldsProjectInGivenDirectory(t *testing.T) {
	p := newProject(t)

	// module path, binaries, kind for "api" (1 = HTTP), set up CI
	err := p.run("github.com/acme/widget\napi\n1\ny\n", "init")
	if err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, p.out)
	}

	if !strings.Contains(p.read("cmd/api/main.go"), "github.com/dfryer1193/mjolnir/router") {
		t.Errorf("expected http main to use mjolnir router")
	}
	p.read("Makefile")
	p.read(".golangci.yml")
	p.read(".github/workflows/ci.yml")

	if !p.runner.requestedLine(p.dir, "go mod init github.com/acme/widget") {
		t.Errorf("expected go mod init in project dir, got %+v", p.runner.requested)
	}
	if !p.runner.requestedLine(p.dir, "go mod tidy") {
		t.Errorf("expected go mod tidy in project dir, got %+v", p.runner.requested)
	}
	if !strings.Contains(p.out.String(), "Module Path") {
		t.Errorf("expected module path prompt on output, got:\n%s", p.out)
	}
}

func TestOnlyInitIsOffered(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"update"},
		{"clobber"},
		{"-p", "github.com/acme", "init"},
		{"--module-prefix", "github.com/acme", "init"},
		{"init", "extra"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			p := newProject(t)

			err := p.run("", args...)
			if !errors.Is(err, cli.ErrUsage) {
				t.Fatalf("expected usage error, got %v", err)
			}
			if !strings.Contains(p.out.String(), "init") || strings.Contains(p.out.String(), "update") {
				t.Errorf("expected usage offering only init, got:\n%s", p.out)
			}
			if entries, _ := os.ReadDir(p.dir); len(entries) != 0 {
				t.Errorf("expected nothing written, found %d entries", len(entries))
			}
			if len(p.runner.requested) != 0 {
				t.Errorf("expected no commands, got %+v", p.runner.requested)
			}
		})
	}
}

func TestInitDefaultsModulePathToProjectDirectoryName(t *testing.T) {
	p := newProject(t)

	if err := p.run("\n\n\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	if !p.runner.requestedLine(p.dir, "go mod init "+filepath.Base(p.dir)) {
		t.Errorf("expected go mod init with dir name, got %+v", p.runner.requested)
	}
}

func TestInitNeverOverwritesExistingFiles(t *testing.T) {
	p := newProject(t)
	p.write("go.mod", "module example.com/mine\n")
	p.write("Makefile", "mine\n")
	p.write(".golangci.yml", "mine\n")
	p.write("cmd/api/main.go", "package main // mine\n")

	if err := p.run("github.com/acme/widget\napi\n1\nn\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	for rel, want := range map[string]string{
		"go.mod":          "module example.com/mine\n",
		"Makefile":        "mine\n",
		".golangci.yml":   "mine\n",
		"cmd/api/main.go": "package main // mine\n",
	} {
		if got := p.read(rel); got != want {
			t.Errorf("%s was overwritten: %q", rel, got)
		}
	}
	if slices.ContainsFunc(p.runner.requested, func(c cli.Command) bool {
		return c.String() == "go mod init github.com/acme/widget"
	}) {
		t.Errorf("expected no go mod init when go.mod exists")
	}
}

func TestInitWithoutCISkipsWorkflow(t *testing.T) {
	p := newProject(t)

	if err := p.run("github.com/acme/widget\n\n1\nn\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(p.dir, ".github")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected no CI workflow, stat err = %v", err)
	}
}

func TestInitWarnsAndSucceedsWhenTidyFails(t *testing.T) {
	p := newProject(t)
	p.runner.fail = map[string]error{"go mod tidy": errors.New("network down")}

	if err := p.run("github.com/acme/widget\n\n1\nn\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	if !strings.Contains(p.out.String(), "network down") {
		t.Errorf("expected tidy warning, got:\n%s", p.out)
	}
}

func TestInitFailsWhenGoModInitFails(t *testing.T) {
	p := newProject(t)
	p.runner.fail = map[string]error{"go mod init github.com/acme/widget": errors.New("boom")}

	if err := p.run("github.com/acme/widget\n\n1\nn\n", "init"); err == nil {
		t.Fatalf("expected init to fail")
	}
}
