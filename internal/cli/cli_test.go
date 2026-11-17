package cli_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/worldtree-industries/ivaldi/internal/cli"
)

// fakeRunner records every command requested, returns scripted stdout, and
// fails the commands scripted to.
// Both maps are keyed by Command.String().
type fakeRunner struct {
	requested []cli.Command
	out       map[string]string // scripted stdout
	fail      map[string]error
}

func (f *fakeRunner) Run(_ context.Context, cmd cli.Command) ([]byte, error) {
	f.requested = append(f.requested, cmd)
	return []byte(f.out[cmd.String()]), f.fail[cmd.String()]
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

func TestInitDefaultsModulePathFromOriginAndWritesGoMod(t *testing.T) {
	p := newProject(t)
	p.runner.out = map[string]string{"git remote get-url origin": "git@github.com:acme/widget.git\n"}

	if err := p.run("\n", "init"); err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, p.out)
	}

	if !strings.Contains(p.out.String(), "Module path [github.com/acme/widget]") {
		t.Errorf("expected module path prompt defaulting from origin, got:\n%s", p.out)
	}
	if got, want := p.read("go.mod"), "module github.com/acme/widget\n\ngo 1.27\n"; got != want {
		t.Errorf("go.mod = %q, want %q", got, want)
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

func TestInitAsksModulePathWithNoDefaultWithoutRemote(t *testing.T) {
	p := newProject(t)
	p.runner.fail = map[string]error{"git remote get-url origin": errors.New("no such remote 'origin'")}

	if err := p.run("\ngithub.com/acme/gadget\n", "init"); err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, p.out)
	}

	if got := strings.Count(p.out.String(), "Module path: "); got != 2 {
		t.Errorf("expected module path asked twice with no default, got %d times:\n%s", got, p.out)
	}
	if got, want := p.read("go.mod"), "module github.com/acme/gadget\n\ngo 1.27\n"; got != want {
		t.Errorf("go.mod = %q, want %q", got, want)
	}
}

func TestInitFailsWhenInputEndsWithoutModulePath(t *testing.T) {
	p := newProject(t)
	p.runner.fail = map[string]error{
		"git rev-parse --is-inside-work-tree": errors.New("not a git repository"),
		"git remote get-url origin":           errors.New("not a git repository"),
	}

	if err := p.run("", "init"); err == nil {
		t.Fatalf("expected init to fail")
	}
	if entries, _ := os.ReadDir(p.dir); len(entries) != 0 {
		t.Errorf("expected nothing written, found %d entries", len(entries))
	}
	if p.runner.requestedLine(p.dir, "git init") {
		t.Errorf("expected no git init before the questions are answered")
	}
}

func TestInitModulePathDefaultHandlesRemoteURLForms(t *testing.T) {
	for remote, want := range map[string]string{
		"git@github.com:acme/widget.git":         "github.com/acme/widget",
		"git@github.com:acme/widget":             "github.com/acme/widget",
		"https://github.com/acme/widget.git":     "github.com/acme/widget",
		"https://github.com/acme/widget/":        "github.com/acme/widget",
		"https://user@gitlab.com/acme/sub/w.git": "gitlab.com/acme/sub/w",
		"ssh://git@github.com/acme/widget.git":   "github.com/acme/widget",
		"ssh://git@github.com:22/acme/widget":    "github.com/acme/widget",
	} {
		t.Run(remote, func(t *testing.T) {
			p := newProject(t)
			p.runner.out = map[string]string{"git remote get-url origin": remote + "\n"}

			if err := p.run("\n", "init"); err != nil {
				t.Fatalf("init failed: %v", err)
			}

			if got := p.read("go.mod"); !strings.HasPrefix(got, "module "+want+"\n") {
				t.Errorf("go.mod = %q, want module %s", got, want)
			}
		})
	}
}

func TestInitRunsGitInitOnlyOutsideARepository(t *testing.T) {
	for name, tc := range map[string]struct {
		insideRepo  bool
		wantGitInit bool
	}{
		"not a repository":     {insideRepo: false, wantGitInit: true},
		"already a repository": {insideRepo: true, wantGitInit: false},
	} {
		t.Run(name, func(t *testing.T) {
			p := newProject(t)
			p.runner.out = map[string]string{"git remote get-url origin": "git@github.com:acme/widget.git"}
			if !tc.insideRepo {
				p.runner.fail = map[string]error{
					"git rev-parse --is-inside-work-tree": errors.New("not a git repository"),
				}
			}

			if err := p.run("\n", "init"); err != nil {
				t.Fatalf("init failed: %v\noutput:\n%s", err, p.out)
			}

			if got := p.runner.requestedLine(p.dir, "git init"); got != tc.wantGitInit {
				t.Errorf("git init requested = %v, want %v; requested %+v", got, tc.wantGitInit, p.runner.requested)
			}
			for _, c := range p.runner.requested {
				if c.Name == "git" && len(c.Args) > 0 && (c.Args[0] == "commit" || c.Args[0] == "add") {
					t.Errorf("expected nothing committed, got %q", c)
				}
			}
		})
	}
}

func TestInitFailsWhenGitInitFails(t *testing.T) {
	p := newProject(t)
	p.runner.fail = map[string]error{
		"git rev-parse --is-inside-work-tree": errors.New("not a git repository"),
		"git init":                            errors.New("boom"),
	}

	if err := p.run("github.com/acme/widget\n", "init"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected git init failure, got %v", err)
	}
}

func TestInitWritesManifestWithNoBinaries(t *testing.T) {
	p := newProject(t)

	if err := p.run("github.com/acme/widget\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	manifest := p.read(".forge.yaml")
	if !regexp.MustCompile(`(?m)^version: 1$`).MatchString(manifest) {
		t.Errorf("expected version: 1 in manifest, got:\n%s", manifest)
	}
	if !regexp.MustCompile(`(?m)^binaries: \{\}$`).MatchString(manifest) {
		t.Errorf("expected empty binaries in manifest, got:\n%s", manifest)
	}
}

func TestInitRefusesWhenManifestExists(t *testing.T) {
	p := newProject(t)
	p.write(".forge.yaml", "version: 1\nbinaries: {}\n")

	err := p.run("github.com/acme/widget\n", "init")
	if err == nil || !strings.Contains(err.Error(), "ivaldi add") {
		t.Fatalf("expected refusal pointing at ivaldi add, got %v", err)
	}
	if entries, _ := os.ReadDir(p.dir); len(entries) != 1 {
		t.Errorf("expected nothing else written, found %d entries", len(entries))
	}
	if len(p.runner.requested) != 0 {
		t.Errorf("expected no commands, got %+v", p.runner.requested)
	}
}

func TestInitCreatesLayerPackagesWithDocFiles(t *testing.T) {
	p := newProject(t)

	if err := p.run("github.com/acme/widget\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	for layer, mayImport := range map[string]string{
		"api":     "may import service and models",
		"service": "may import data and models",
		"data":    "may import models",
		"models":  "must not import any other layer",
	} {
		doc := p.read(layer + "/doc.go")
		if !strings.HasPrefix(doc, "// Package "+layer+" ") || !strings.Contains(doc, "\npackage "+layer+"\n") {
			t.Errorf("%s/doc.go is not a documented package %s:\n%s", layer, layer, doc)
		}
		if !strings.Contains(doc, mayImport) {
			t.Errorf("%s/doc.go should say it %s:\n%s", layer, mayImport, doc)
		}
	}
	for _, absent := range []string{"internal", "client", "cmd"} {
		if _, err := os.Stat(filepath.Join(p.dir, absent)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("expected no %s directory, stat err = %v", absent, err)
		}
	}
}

func TestInitWritesMakefileIncludingManagedSharedMakeFile(t *testing.T) {
	widget, gadget := newProject(t), newProject(t)
	if err := widget.run("github.com/acme/widget\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if err := gadget.run("example.com/gadget\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	if !regexp.MustCompile(`(?m)^include ivaldi\.mk$`).MatchString(widget.read("Makefile")) {
		t.Errorf("expected Makefile to include ivaldi.mk, got:\n%s", widget.read("Makefile"))
	}
	shared := widget.read("ivaldi.mk")
	if shared != gadget.read("ivaldi.mk") {
		t.Errorf("expected the shared make file to be identical in every Project")
	}
	if !strings.Contains(shared, "Managed by Ivaldi") {
		t.Errorf("expected the shared make file to say it is Managed, got:\n%s", shared)
	}
	for _, target := range []string{"lint", "test", "build", "run-%", "install"} {
		if !regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(target) + `:`).MatchString(shared) {
			t.Errorf("expected target %s in the shared make file, got:\n%s", target, shared)
		}
	}
}

func TestInitWritesLintConfigEnforcingLayers(t *testing.T) {
	p := newProject(t)

	if err := p.run("github.com/acme/widget\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	cfg := p.read(".golangci.yml")
	if !strings.Contains(cfg, "local-prefixes:\n        - github.com/acme/widget\n") {
		t.Errorf("expected goimports local prefix for the module, got:\n%s", cfg)
	}
	if strings.Contains(cfg, "github.com/my/project") {
		t.Errorf("expected no placeholder module path left in lint config")
	}
	for layer, denied := range map[string][]string{
		"api":     {"data", "client"},
		"service": {"api", "client"},
		"data":    {"api", "service", "client"},
		"models":  {"api", "service", "data", "client"},
		"client":  {"api", "service", "data"},
	} {
		rule := depguardRule(t, cfg, layer+" layer")
		if !strings.Contains(rule, `"${base-path}/`+layer+`/**"`) {
			t.Errorf("rule for %s doesn't cover the %s directory:\n%s", layer, layer, rule)
		}
		for _, d := range denied {
			for _, pkg := range []string{"github.com/acme/widget/" + d + "$", "github.com/acme/widget/" + d + "/"} {
				if !strings.Contains(rule, "- pkg: "+pkg+"\n") {
					t.Errorf("rule for %s doesn't deny %s:\n%s", layer, pkg, rule)
				}
			}
		}
	}
}

// depguardRule returns the text of the named depguard rule in cfg.
func depguardRule(t *testing.T, cfg, name string) string {
	t.Helper()
	m := regexp.MustCompile(`(?ms)^        "` + regexp.QuoteMeta(name) + `":\n(.*?)^        \S`).FindStringSubmatch(cfg)
	if m == nil {
		t.Fatalf("no depguard rule %q in lint config", name)
	}
	return m[1]
}

func TestInitWritesReadmeSkeleton(t *testing.T) {
	p := newProject(t)

	if err := p.run("github.com/acme/widget\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	readme := p.read("README.md")
	if !strings.HasPrefix(readme, "# widget\n") {
		t.Errorf("expected README titled after the module's last path element, got:\n%s", readme)
	}
	if !strings.Contains(readme, "TODO") {
		t.Errorf("expected a description TODO in README, got:\n%s", readme)
	}
	for _, target := range []string{"make lint", "make test", "make build", "make run-<name>", "make install"} {
		if !strings.Contains(readme, target) {
			t.Errorf("expected README to list %q, got:\n%s", target, readme)
		}
	}
}

func TestInitWritesOnlyMissingFiles(t *testing.T) {
	p := newProject(t)
	mine := map[string]string{
		"go.mod":        "module example.com/mine\n",
		"Makefile":      "mine\n",
		"ivaldi.mk":     "mine\n",
		".golangci.yml": "mine\n",
		"README.md":     "# mine\n",
		"api/doc.go":    "package api // mine\n",
	}
	for rel, content := range mine {
		p.write(rel, content)
	}

	if err := p.run("github.com/acme/widget\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	for rel, want := range mine {
		if got := p.read(rel); got != want {
			t.Errorf("%s was overwritten: %q", rel, got)
		}
		if !strings.Contains(p.out.String(), rel+" already exists") {
			t.Errorf("expected init to report skipping %s, got:\n%s", rel, p.out)
		}
	}
	for _, missing := range []string{".forge.yaml", "service/doc.go", "data/doc.go", "models/doc.go"} {
		p.read(missing)
	}
}
