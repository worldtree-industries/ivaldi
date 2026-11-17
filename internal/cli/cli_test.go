package cli_test

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"os"
	"os/exec"
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

func (f *fakeRunner) requestedName(name string) bool {
	return slices.ContainsFunc(f.requested, func(c cli.Command) bool { return c.Name == name })
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

func TestOnlyInitAndAddAreOffered(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"update"},
		{"clobber"},
		{"-p", "github.com/acme", "init"},
		{"--module-prefix", "github.com/acme", "init"},
		{"init", "extra"},
		{"add", "a"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			p := newProject(t)

			err := p.run("", args...)
			if !errors.Is(err, cli.ErrUsage) {
				t.Fatalf("expected usage error, got %v", err)
			}
			usage := p.out.String()
			if !strings.Contains(usage, "  init ") || !strings.Contains(usage, "  add ") ||
				strings.Contains(usage, "update") || strings.Contains(usage, "clobber") {
				t.Errorf("expected usage offering only init and add, got:\n%s", usage)
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
		".gitignore":    "mine\n",
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

func TestAddRefusesWithoutManifest(t *testing.T) {
	p := newProject(t)

	err := p.run("a\ntool\n", "add")
	if err == nil || !strings.Contains(err.Error(), "ivaldi init") {
		t.Fatalf("expected refusal pointing at ivaldi init, got %v", err)
	}
	if entries, _ := os.ReadDir(p.dir); len(entries) != 0 {
		t.Errorf("expected nothing written, found %d entries", len(entries))
	}
	if len(p.runner.requested) != 0 {
		t.Errorf("expected no commands, got %+v", p.runner.requested)
	}
}

// initProject runs init with module path github.com/acme/widget and no
// binaries, then clears the output and requested commands.
func (p *project) initProject() {
	p.t.Helper()
	if err := p.run("github.com/acme/widget\n\n", "init"); err != nil {
		p.t.Fatalf("init failed: %v\noutput:\n%s", err, p.out)
	}
	p.out.Reset()
	p.runner.requested = nil
}

// goBuild builds ./cmd/<name> in the Project with the real go toolchain and
// returns the path of the binary.
func (p *project) goBuild(name string) string {
	p.t.Helper()
	bin := filepath.Join(p.t.TempDir(), name)
	cmd := exec.CommandContext(p.t.Context(), "go", "build", "-o", bin, "./cmd/"+name)
	cmd.Dir = p.dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod")
	if out, err := cmd.CombinedOutput(); err != nil {
		p.t.Fatalf("go build ./cmd/%s failed: %v\n%s", name, err, out)
	}
	return bin
}

func TestAddCreatesToolsThatBuildAndPrintAVersion(t *testing.T) {
	p := newProject(t)
	p.initProject()

	if err := p.run("a, b\ntool\ntool\n", "add"); err != nil {
		t.Fatalf("add failed: %v\noutput:\n%s", err, p.out)
	}

	for _, name := range []string{"a", "b"} {
		main := p.read("cmd/" + name + "/main.go")
		if !strings.Contains(main, `"flag"`) || !strings.Contains(main, "TODO") ||
			!strings.Contains(main, "github.com/acme/widget/service") {
			t.Errorf("cmd/%s/main.go isn't a flag-based Tool with a TODO pointing at service:\n%s", name, main)
		}
		out, err := exec.CommandContext(t.Context(), p.goBuild(name), "-version").CombinedOutput()
		if err != nil {
			t.Fatalf("%s -version failed: %v\n%s", name, err, out)
		}
		if strings.TrimSpace(string(out)) == "" {
			t.Errorf("%s -version printed nothing", name)
		}
	}
}

func TestAddAppendsToolsToManifestKeepingHandEdits(t *testing.T) {
	p := newProject(t)
	p.initProject()
	p.write(".forge.yaml", `# My Project's Manifest.
version: 1
binaries:
  # The public API.
  web:
    kind: http
    port: 9090 # fixed by the load balancer
    base_image: example.com/base:1
  nightly:
    kind: job
    schedule: daily
owner: team-a # not something Ivaldi knows about
`)

	if err := p.run("a, b\ntool\ntool\n", "add"); err != nil {
		t.Fatalf("add failed: %v\noutput:\n%s", err, p.out)
	}

	want := `# My Project's Manifest.
version: 1
binaries:
  # The public API.
  web:
    kind: http
    port: 9090 # fixed by the load balancer
    base_image: example.com/base:1
  nightly:
    kind: job
    schedule: daily
  a:
    kind: tool
  b:
    kind: tool
owner: team-a # not something Ivaldi knows about
`
	if got := p.read(".forge.yaml"); got != want {
		t.Errorf("manifest after add:\n%s\nwant:\n%s", got, want)
	}
}

func TestAddToEmptyManifestWritesBlockStyleEntries(t *testing.T) {
	p := newProject(t)
	p.initProject()

	if err := p.run("a\ntool\n", "add"); err != nil {
		t.Fatalf("add failed: %v\noutput:\n%s", err, p.out)
	}

	manifest := p.read(".forge.yaml")
	if !strings.HasPrefix(manifest, "# The Ivaldi Manifest") {
		t.Errorf("expected the header comment kept, got:\n%s", manifest)
	}
	if !strings.Contains(manifest, "\nbinaries:\n  a:\n    kind: tool\n") {
		t.Errorf("expected a block-style entry for a, got:\n%s", manifest)
	}
}

func TestAddAbortsOnABadNameWithNothingWritten(t *testing.T) {
	for name, tc := range map[string]struct {
		names   string
		wantErr string
	}{
		"invalid name":        {names: "a, Bad_Name", wantErr: `"Bad_Name"`},
		"starts with digit":   {names: "a, 9lives", wantErr: `"9lives"`},
		"too long":            {names: "a, " + strings.Repeat("x", 64), wantErr: strings.Repeat("x", 64)},
		"empty entry":         {names: "a,,b", wantErr: `""`},
		"duplicate in list":   {names: "a, b, a", wantErr: `"a"`},
		"already in Manifest": {names: "a, web", wantErr: `"web"`},
		"cmd dir exists":      {names: "a, legacy", wantErr: `"legacy"`},
	} {
		t.Run(name, func(t *testing.T) {
			p := newProject(t)
			p.initProject()
			p.write(".forge.yaml", "version: 1\nbinaries:\n  web:\n    kind: http\n")
			p.write("cmd/legacy/README", "mine\n")
			before := snapshot(t, p.dir)

			err := p.run(tc.names+"\ntool\ntool\ntool\n", "add")
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected an error naming %s, got %v", tc.wantErr, err)
			}
			if after := snapshot(t, p.dir); !maps.Equal(before, after) {
				t.Errorf("expected nothing written, files went from %v to %v", slices.Sorted(maps.Keys(before)),
					slices.Sorted(maps.Keys(after)))
			}
			if strings.Contains(p.out.String(), "Kind of") {
				t.Errorf("expected no Kind asked before every name is valid, got:\n%s", p.out)
			}
			if len(p.runner.requested) != 0 {
				t.Errorf("expected no commands, got %+v", p.runner.requested)
			}
		})
	}
}

func TestAddAcceptsAMaximumLengthName(t *testing.T) {
	p := newProject(t)
	p.initProject()
	name := "a" + strings.Repeat("-9", 31)

	if err := p.run(name+"\ntool\n", "add"); err != nil {
		t.Fatalf("add failed: %v\noutput:\n%s", err, p.out)
	}
	p.read("cmd/" + name + "/main.go")
}

// snapshot returns every file under dir with its content.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		files[path] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestAddReasksAnUnknownKind(t *testing.T) {
	p := newProject(t)
	p.initProject()

	if err := p.run("a\nworker\ntool\n", "add"); err != nil {
		t.Fatalf("add failed: %v\noutput:\n%s", err, p.out)
	}

	if got := strings.Count(p.out.String(), "Kind of a (http, job, tool): "); got != 2 {
		t.Errorf("expected the Kind of a asked twice, got %d times:\n%s", got, p.out)
	}
	p.read("cmd/a/main.go")
}

func TestAddFailsWhenInputEndsBeforeAKindWithNothingWritten(t *testing.T) {
	p := newProject(t)
	p.initProject()
	before := snapshot(t, p.dir)

	if err := p.run("a, b\ntool\n", "add"); err == nil {
		t.Fatalf("expected add to fail")
	}
	if !maps.Equal(before, snapshot(t, p.dir)) {
		t.Errorf("expected nothing written")
	}
}

func TestAddRequiresAtLeastOneName(t *testing.T) {
	p := newProject(t)
	p.initProject()

	if err := p.run("\n", "add"); err == nil {
		t.Fatalf("expected add to fail when input ends without a name")
	}
	if got := strings.Count(p.out.String(), "Binary names (comma-separated): "); got != 2 {
		t.Errorf("expected blank names re-asked, asked %d times:\n%s", got, p.out)
	}
}

func TestAddTidiesTheModuleAfterWritingTools(t *testing.T) {
	p := newProject(t)
	p.initProject()

	if err := p.run("a\ntool\n", "add"); err != nil {
		t.Fatalf("add failed: %v\noutput:\n%s", err, p.out)
	}

	if !p.runner.requestedLine(p.dir, "go mod tidy") {
		t.Errorf("expected go mod tidy, requested %+v", p.runner.requested)
	}
	for _, c := range p.runner.requested {
		if c.Name == "go" && len(c.Args) > 0 && c.Args[0] == "get" {
			t.Errorf("expected nothing fetched for a Tool, got %q", c)
		}
	}
}

func TestInitAsksForBinariesLastAndAcceptsBlank(t *testing.T) {
	p := newProject(t)

	if err := p.run("github.com/acme/widget\n\n", "init"); err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, p.out)
	}

	out := p.out.String()
	if !strings.Contains(out, "Binary names (comma-separated, blank for none): ") ||
		strings.Index(out, "Binary names") < strings.Index(out, "Module path") {
		t.Errorf("expected init to ask for binaries after the module path, got:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(p.dir, "cmd")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected no cmd directory, stat err = %v", err)
	}
	if p.runner.requestedLine(p.dir, "go mod tidy") {
		t.Errorf("expected no go mod tidy without binaries")
	}
}

func TestInitCanAddToolsAndRecordsThemInTheManifest(t *testing.T) {
	p := newProject(t)

	if err := p.run("github.com/acme/widget\na\ntool\n", "init"); err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, p.out)
	}

	if !strings.Contains(p.read(".forge.yaml"), "\nbinaries:\n  a:\n    kind: tool\n") {
		t.Errorf("expected a recorded as a tool, got:\n%s", p.read(".forge.yaml"))
	}
	p.goBuild("a")
	if !p.runner.requestedLine(p.dir, "go mod tidy") {
		t.Errorf("expected go mod tidy, requested %+v", p.runner.requested)
	}
}

func TestInitAbortsOnABadBinaryNameWithNothingWritten(t *testing.T) {
	p := newProject(t)

	err := p.run("github.com/acme/widget\na, A\ntool\n", "init")
	if err == nil || !strings.Contains(err.Error(), `"A"`) {
		t.Fatalf("expected an error naming A, got %v", err)
	}
	if entries, _ := os.ReadDir(p.dir); len(entries) != 0 {
		t.Errorf("expected nothing written, found %d entries", len(entries))
	}
}

func TestInitWritesGitignoreForBuildOutput(t *testing.T) {
	p := newProject(t)

	if err := p.run("github.com/acme/widget\n\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	if !regexp.MustCompile(`(?m)^/bin/$`).MatchString(p.read(".gitignore")) {
		t.Errorf("expected .gitignore to ignore bin/, got:\n%s", p.read(".gitignore"))
	}
}

func TestInitWritesGitignoreForDeployableLogs(t *testing.T) {
	p := newProject(t)

	if err := p.run("github.com/acme/widget\n\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// mjolnir's logger writes logs/mjolnir.jsonl under the working directory,
	// so make run-<name> would otherwise leave it untracked.
	if !regexp.MustCompile(`(?m)^/logs/$`).MatchString(p.read(".gitignore")) {
		t.Errorf("expected .gitignore to ignore logs/, got:\n%s", p.read(".gitignore"))
	}
}

// The GitHub settings init applies, as the exact gh commands requested.
func githubSettings() []string {
	return []string{
		"gh api --method PUT repos/acme/widget/actions/permissions -F enabled=true -f allowed_actions=selected",
		"gh api --method PUT repos/acme/widget/actions/permissions/selected-actions" +
			" -F github_owned_allowed=true -F verified_allowed=false" +
			" -f patterns_allowed[]=worldtree-industries/*" +
			" -f patterns_allowed[]=golangci/golangci-lint-action@*" +
			" -f patterns_allowed[]=docker/login-action@*" +
			" -f patterns_allowed[]=docker/setup-buildx-action@*" +
			" -f patterns_allowed[]=docker/build-push-action@*",
		"gh api --method PUT repos/acme/widget/actions/permissions/workflow" +
			" -f default_workflow_permissions=read -F can_approve_pull_request_reviews=false",
	}
}

const adminCheck = "gh api repos/acme/widget --jq .permissions.admin"

func TestInitAppliesGitHubSettingsWhenOriginIsGitHub(t *testing.T) {
	p := newProject(t)
	p.runner.out = map[string]string{
		"git remote get-url origin": "git@github.com:acme/widget.git\n",
		adminCheck:                  "true\n",
	}

	if err := p.run("\n", "init"); err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, p.out)
	}

	for _, line := range githubSettings() {
		if !p.runner.requestedLine(p.dir, line) {
			t.Errorf("expected %q to be requested; requested %+v", line, p.runner.requested)
		}
	}
	if strings.Contains(p.out.String(), "Warning") {
		t.Errorf("expected no warning, got:\n%s", p.out)
	}
}

func TestInitWarnsWithCommandsWhenGitHubSettingsCannotBeApplied(t *testing.T) {
	const repoCommands = `  gh api --method PUT repos/acme/widget/actions/permissions -F enabled=true -f allowed_actions=selected
  gh api --method PUT repos/acme/widget/actions/permissions/selected-actions -F github_owned_allowed=true -F verified_allowed=false -f 'patterns_allowed[]=worldtree-industries/*' -f 'patterns_allowed[]=golangci/golangci-lint-action@*' -f 'patterns_allowed[]=docker/login-action@*' -f 'patterns_allowed[]=docker/setup-buildx-action@*' -f 'patterns_allowed[]=docker/build-push-action@*'
  gh api --method PUT repos/acme/widget/actions/permissions/workflow -f default_workflow_permissions=read -F can_approve_pull_request_reviews=false
`
	const placeholderCommands = `  gh api --method PUT repos/OWNER/REPO/actions/permissions -F enabled=true -f allowed_actions=selected
`
	for name, tc := range map[string]struct {
		origin   string
		admin    string
		fail     map[string]error
		commands string
		noGH     bool
	}{
		"no origin": {
			fail:     map[string]error{"git remote get-url origin": errors.New("no such remote 'origin'")},
			commands: placeholderCommands,
			noGH:     true,
		},
		"origin not on GitHub": {
			origin:   "git@gitlab.com:acme/widget.git",
			commands: placeholderCommands,
			noGH:     true,
		},
		"gh missing or unauthenticated": {
			origin:   "git@github.com:acme/widget.git",
			fail:     map[string]error{adminCheck: errors.New("gh: not logged in")},
			commands: repoCommands,
		},
		"gh lacks admin rights": {
			origin:   "git@github.com:acme/widget.git",
			admin:    "false",
			commands: repoCommands,
		},
		"API call fails": {
			origin:   "git@github.com:acme/widget.git",
			admin:    "true",
			fail:     map[string]error{githubSettings()[1]: errors.New("HTTP 409")},
			commands: repoCommands,
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := newProject(t)
			p.runner.out = map[string]string{"git remote get-url origin": tc.origin, adminCheck: tc.admin}
			p.runner.fail = tc.fail

			if err := p.run("github.com/acme/widget\n", "init"); err != nil {
				t.Fatalf("expected init to succeed, got %v\noutput:\n%s", err, p.out)
			}

			out := p.out.String()
			if !strings.Contains(out, "Warning: GitHub settings were not applied") {
				t.Errorf("expected a warning, got:\n%s", out)
			}
			if !strings.Contains(out, tc.commands) {
				t.Errorf("expected the commands to apply the settings:\n%s\ngot:\n%s", tc.commands, out)
			}
			p.read(".forge.yaml")
			if tc.noGH && p.runner.requestedName("gh") {
				t.Errorf("expected no gh calls without a GitHub origin, got %+v", p.runner.requested)
			}
		})
	}
}

func TestAddNeverTouchesGitHub(t *testing.T) {
	p := newProject(t)
	p.runner.out = map[string]string{
		"git remote get-url origin": "git@github.com:acme/widget.git\n",
		adminCheck:                  "true\n",
	}
	p.initProject()

	if err := p.run("a\ntool\n", "add"); err != nil {
		t.Fatalf("add failed: %v\noutput:\n%s", err, p.out)
	}

	if p.runner.requestedName("gh") {
		t.Errorf("expected add to make no GitHub calls, got %+v", p.runner.requested)
	}
	if strings.Contains(p.out.String(), "GitHub") {
		t.Errorf("expected add to say nothing about GitHub, got:\n%s", p.out)
	}
}

func TestAddHTTPRecordsKindAndDefaultPortAndFetchesMjolnir(t *testing.T) {
	p := newProject(t)
	p.initProject()

	if err := p.run("web\nhttp\n\n\n", "add"); err != nil {
		t.Fatalf("add failed: %v\noutput:\n%s", err, p.out)
	}

	if !strings.Contains(p.out.String(), "Port for web [8080]: ") {
		t.Errorf("expected a port prompt defaulting to 8080, got:\n%s", p.out)
	}
	if !strings.Contains(p.read(".forge.yaml"), "\nbinaries:\n  web:\n    kind: http\n    port: 8080\n") {
		t.Errorf("expected web recorded as http on port 8080, got:\n%s", p.read(".forge.yaml"))
	}
	main := p.read("cmd/web/main.go")
	for _, want := range []string{
		`"github.com/dfryer1193/mjolnir/router"`,
		`"github.com/dfryer1193/mjolnir/health"`,
		`"github.com/dfryer1193/mjolnir/utils/log"`,
		"health.AlwaysStarted",
		"TODO",
		`os.Getenv("PORT")`,
		`"8080"`,
		"10 * time.Second",
		"router.NewServer(",
	} {
		if !strings.Contains(main, want) {
			t.Errorf("cmd/web/main.go doesn't contain %s:\n%s", want, main)
		}
	}

	var goCmds []string
	for _, c := range p.runner.requested {
		if c.Name == "go" && c.Dir == p.dir {
			goCmds = append(goCmds, c.String())
		}
	}
	if want := []string{"go get github.com/dfryer1193/mjolnir@latest", "go mod tidy"}; !slices.Equal(goCmds, want) {
		t.Errorf("go commands = %q, want %q", goCmds, want)
	}
}

func TestAddHTTPDefaultPortSkipsPortsAlreadyUsed(t *testing.T) {
	p := newProject(t)
	p.initProject()
	p.write(".forge.yaml", "version: 1\nbinaries:\n  web:\n    kind: http\n    port: 8080\n"+
		"  admin:\n    kind: http\n    port: 8082\n")

	if err := p.run("a, b\nhttp\n\n\nhttp\n\n\n", "add"); err != nil {
		t.Fatalf("add failed: %v\noutput:\n%s", err, p.out)
	}

	out := p.out.String()
	if !strings.Contains(out, "Port for a [8081]: ") || !strings.Contains(out, "Port for b [8083]: ") {
		t.Errorf("expected defaults 8081 for a and 8083 for b, got:\n%s", out)
	}
	manifest := p.read(".forge.yaml")
	if !strings.Contains(manifest, "  a:\n    kind: http\n    port: 8081\n  b:\n    kind: http\n    port: 8083\n") {
		t.Errorf("expected a on 8081 and b on 8083, got:\n%s", manifest)
	}
	if !strings.Contains(p.read("cmd/b/main.go"), `"8083"`) {
		t.Errorf("expected cmd/b to fall back to 8083:\n%s", p.read("cmd/b/main.go"))
	}
}

func TestAddHTTPReasksAnInvalidOrUsedPort(t *testing.T) {
	p := newProject(t)
	p.initProject()
	p.write(".forge.yaml", "version: 1\nbinaries:\n  web:\n    kind: http\n    port: 9090\n")

	if err := p.run("a\nhttp\nabc\n0\n80\n1023\n65536\n9090\n9091\n\n", "add"); err != nil {
		t.Fatalf("add failed: %v\noutput:\n%s", err, p.out)
	}

	if got := strings.Count(p.out.String(), "Port for a [8080]: "); got != 7 {
		t.Errorf("expected the port asked 7 times, got %d:\n%s", got, p.out)
	}
	if !strings.Contains(p.out.String(), "from 1024 to 65535") {
		t.Errorf("expected the allowed range in the message, got:\n%s", p.out)
	}
	if !strings.Contains(p.out.String(), "9090 is already used") {
		t.Errorf("expected a message that 9090 is taken, got:\n%s", p.out)
	}
	if !strings.Contains(p.read(".forge.yaml"), "  a:\n    kind: http\n    port: 9091\n") {
		t.Errorf("expected a on 9091, got:\n%s", p.read(".forge.yaml"))
	}
}

func TestAddHTTPAcceptsThePortRangeBounds(t *testing.T) {
	p := newProject(t)
	p.initProject()

	if err := p.run("a, b\nhttp\n1024\n\nhttp\n65535\n\n", "add"); err != nil {
		t.Fatalf("add failed: %v\noutput:\n%s", err, p.out)
	}

	want := "  a:\n    kind: http\n    port: 1024\n  b:\n    kind: http\n    port: 65535\n"
	if !strings.Contains(p.read(".forge.yaml"), want) {
		t.Errorf("expected a on 1024 and b on 65535, got:\n%s", p.read(".forge.yaml"))
	}
}

func TestAddHTTPFailsWhenInputEndsBeforeAPortWithNothingWritten(t *testing.T) {
	p := newProject(t)
	p.initProject()
	before := snapshot(t, p.dir)

	if err := p.run("a\nhttp\nabc\n", "add"); err == nil {
		t.Fatalf("expected add to fail")
	}
	if !maps.Equal(before, snapshot(t, p.dir)) {
		t.Errorf("expected nothing written")
	}
	if len(p.runner.requested) != 0 {
		t.Errorf("expected no commands, got %+v", p.runner.requested)
	}
}

const clientQuestion = "Create a client layer"

func TestAddHTTPCreatesClientLayerOnRequest(t *testing.T) {
	p := newProject(t)
	p.initProject()

	if err := p.run("web\nhttp\n\ny\n", "add"); err != nil {
		t.Fatalf("add failed: %v\noutput:\n%s", err, p.out)
	}

	if !strings.Contains(p.out.String(), clientQuestion+" (other Projects may import it)? [y/N]: ") {
		t.Errorf("expected the client question defaulting to no, got:\n%s", p.out)
	}
	doc := p.read("client/doc.go")
	if !strings.Contains(doc, "package client") || !strings.Contains(doc, "may import only models") {
		t.Errorf("client/doc.go isn't the client layer's doc file:\n%s", doc)
	}
	if !strings.Contains(p.read(".golangci.yml"), "client may import only models") {
		t.Errorf("expected the lint config to already cover client")
	}
}

func TestAddHTTPCreatesNoClientLayerByDefault(t *testing.T) {
	p := newProject(t)
	p.initProject()

	if err := p.run("web\nhttp\n\n\n", "add"); err != nil {
		t.Fatalf("add failed: %v\noutput:\n%s", err, p.out)
	}

	if _, err := os.Stat(filepath.Join(p.dir, "client")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected no client directory, stat err = %v", err)
	}
}

func TestAddAsksAboutClientOnlyForHTTPAndOnlyWhileItDoesNotExist(t *testing.T) {
	for name, tc := range map[string]struct {
		setup     func(p *project)
		input     string
		wantAsked int
	}{
		"tool only":                     {input: "a\ntool\n", wantAsked: 0},
		"two http, first says yes":      {input: "a, b\nhttp\n\ny\nhttp\n\n", wantAsked: 1},
		"two http, first says no":       {input: "a, b\nhttp\n\nn\nhttp\n\nn\n", wantAsked: 2},
		"client already exists":         {setup: func(p *project) { p.write("client/doc.go", "package client\n") }, input: "a\nhttp\n\n", wantAsked: 0},
		"client dir exists without doc": {setup: func(p *project) { p.write("client/client.go", "package client\n") }, input: "a\nhttp\n\n", wantAsked: 0},
		"tool then http asks for http":  {input: "a, b\ntool\nhttp\n\n\n", wantAsked: 1},
	} {
		t.Run(name, func(t *testing.T) {
			p := newProject(t)
			p.initProject()
			if tc.setup != nil {
				tc.setup(p)
			}

			if err := p.run(tc.input, "add"); err != nil {
				t.Fatalf("add failed: %v\noutput:\n%s", err, p.out)
			}

			if got := strings.Count(p.out.String(), clientQuestion); got != tc.wantAsked {
				t.Errorf("client question asked %d times, want %d:\n%s", got, tc.wantAsked, p.out)
			}
		})
	}
}

func TestInitCanCreateHTTPWithClientLayer(t *testing.T) {
	p := newProject(t)

	if err := p.run("github.com/acme/widget\nweb\nhttp\n\ny\n", "init"); err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, p.out)
	}

	p.read("client/doc.go")
	if !strings.Contains(p.read(".forge.yaml"), "\nbinaries:\n  web:\n    kind: http\n    port: 8080\n") {
		t.Errorf("expected web recorded as http on 8080, got:\n%s", p.read(".forge.yaml"))
	}
	if !p.runner.requestedLine(p.dir, "go get github.com/dfryer1193/mjolnir@latest") ||
		!p.runner.requestedLine(p.dir, "go mod tidy") {
		t.Errorf("expected mjolnir fetched and the module tidied, requested %+v", p.runner.requested)
	}
}
