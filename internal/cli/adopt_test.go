package cli_test

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/worldtree-industries/ivaldi/internal/cli"
)

// An existing repo's go.mod at the go line mjolnir needs, so the go line
// question isn't asked.
const adoptedGoMod = "module example.com/legacy\n\ngo 1.27\n"

func mainSource(name string) string {
	return "package main\n\n// " + name + " is mine.\nfunc main() {}\n"
}

func TestAdoptionRecordsDiscoveredBinariesAndLeavesTheirMainAlone(t *testing.T) {
	p := newProject(t)
	p.write("go.mod", adoptedGoMod)
	p.write("cmd/a/main.go", mainSource("a"))
	p.write("cmd/b/main.go", mainSource("b"))

	// a: http, default port, no client. b: job, daily. No new binaries.
	if err := p.run("http\n\nn\njob\ndaily\n\n", "init"); err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, p.out)
	}

	want := "\nbinaries:\n  a:\n    kind: http\n    port: 8080\n  b:\n    kind: job\n    schedule: daily\n"
	if got := p.read(".forge.yaml"); !strings.Contains(got, want) {
		t.Errorf("expected a and b recorded with their settings, got:\n%s", got)
	}
	for _, name := range []string{"a", "b"} {
		if got := p.read("cmd/" + name + "/main.go"); got != mainSource(name) {
			t.Errorf("cmd/%s/main.go changed:\n%s", name, got)
		}
	}
	if p.runner.requestedName("go") {
		t.Errorf("expected no go commands for discovered binaries, got %+v", p.runner.requested)
	}
}

func TestAdoptionWarnsAboutPackageMainOutsideCmdAndLeavesItAlone(t *testing.T) {
	p := newProject(t)
	p.write("go.mod", adoptedGoMod)
	p.write("main.go", mainSource("root"))
	p.write("tools/gen/main.go", mainSource("gen"))
	p.write("cmd/a/sub/main.go", mainSource("sub"))
	p.write("cmd/a/main.go", mainSource("a"))
	p.write("internal/lib/lib.go", "package lib\n")

	if err := p.run("tool\n\n", "init"); err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, p.out)
	}

	out := p.out.String()
	for _, dir := range []string{"the root directory", "tools/gen", "cmd/a/sub"} {
		if !strings.Contains(out, "Warning: "+dir+" holds package main, but Ivaldi manages only cmd/<name>/") {
			t.Errorf("expected a warning about %s, got:\n%s", dir, out)
		}
	}
	if strings.Contains(out, "internal/lib holds") {
		t.Errorf("expected no warning about a non-main package, got:\n%s", out)
	}
	if got := p.read(".forge.yaml"); !strings.Contains(got, "\nbinaries:\n  a:\n    kind: tool\n") ||
		strings.Contains(got, "gen") || strings.Contains(got, "sub") {
		t.Errorf("expected only a recorded, got:\n%s", got)
	}
	for rel, name := range map[string]string{"main.go": "root", "tools/gen/main.go": "gen", "cmd/a/sub/main.go": "sub"} {
		if got := p.read(rel); got != mainSource(name) {
			t.Errorf("%s changed:\n%s", rel, got)
		}
	}
}

func TestAdoptionErrorsOnAnInvalidDiscoveredNameWithNothingWritten(t *testing.T) {
	p := newProject(t)
	p.write("go.mod", adoptedGoMod)
	p.write("cmd/Legacy/main.go", mainSource("Legacy"))
	before := snapshot(t, p.dir)

	err := p.run("tool\n\n", "init")
	if err == nil || !strings.Contains(err.Error(), `"Legacy"`) || !strings.Contains(err.Error(), "cmd/Legacy") {
		t.Fatalf("expected an error naming cmd/Legacy, got %v", err)
	}
	if !maps.Equal(before, snapshot(t, p.dir)) {
		t.Errorf("expected nothing written")
	}
	if strings.Contains(p.out.String(), "Kind of") {
		t.Errorf("expected nothing asked, got:\n%s", p.out)
	}
	if p.runner.requestedLine(p.dir, "git init") {
		t.Errorf("expected no git init")
	}
}

func TestAdoptionRecordsBuildDockerfileForABinarysOwnDockerfile(t *testing.T) {
	p := newProject(t)
	p.write("go.mod", adoptedGoMod)
	p.write("cmd/a/main.go", mainSource("a"))
	p.write("cmd/a/Dockerfile", "FROM scratch\n")
	p.write("cmd/b/main.go", mainSource("b"))

	if err := p.run("job\n\njob\n\n\n", "init"); err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, p.out)
	}

	want := "\n  a:\n    kind: job\n    schedule: none\n    build: dockerfile\n  b:\n    kind: job\n    schedule: none\n"
	if got := p.read(".forge.yaml"); !strings.Contains(got, want) {
		t.Errorf("expected build: dockerfile for a only, got:\n%s", got)
	}
	if !strings.Contains(p.out.String(), "cmd/a/Dockerfile exists, so a is recorded with build: dockerfile") {
		t.Errorf("expected init to say so, got:\n%s", p.out)
	}
	if got := p.read("cmd/a/Dockerfile"); got != "FROM scratch\n" {
		t.Errorf("cmd/a/Dockerfile changed: %q", got)
	}
}

func TestAdoptionWarnsThatAToolsDockerfileIsntUsed(t *testing.T) {
	p := newProject(t)
	p.write("go.mod", adoptedGoMod)
	p.write("cmd/cli/main.go", mainSource("cli"))
	p.write("cmd/cli/Dockerfile", "FROM scratch\n")

	if err := p.run("tool\n\n", "init"); err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, p.out)
	}

	if got := p.read(
		".forge.yaml",
	); !strings.Contains(got, "\n  cli:\n    kind: tool\n") ||
		strings.Contains(got, "build:") {
		t.Errorf("expected cli recorded as a tool without build, got:\n%s", got)
	}
	out := p.out.String()
	if !strings.Contains(
		out,
		"Warning: cmd/cli/Dockerfile isn't used: cli is a tool, and Tools aren't built into images.",
	) {
		t.Errorf("expected a warning about cmd/cli/Dockerfile, got:\n%s", out)
	}
	if strings.Contains(out, "is recorded with build") {
		t.Errorf("expected no build: dockerfile message, got:\n%s", out)
	}
}

func TestAdoptionWarnsAboutARootDockerfile(t *testing.T) {
	p := newProject(t)
	p.write("go.mod", adoptedGoMod)
	p.write("Dockerfile", "FROM scratch\n")

	if err := p.run("\n", "init"); err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, p.out)
	}

	if !strings.Contains(p.out.String(),
		"Warning: Ivaldi doesn't use the root Dockerfile: move it to cmd/<name>/Dockerfile") {
		t.Errorf("expected a warning about the root Dockerfile, got:\n%s", p.out)
	}
	if got := p.read("Dockerfile"); got != "FROM scratch\n" {
		t.Errorf("Dockerfile changed: %q", got)
	}
}

// The files init asks about overwriting, in the order it asks, with whether
// each is Managed and the warning for keeping it.
//
//nolint:gochecknoglobals // Constant table.
var overwritable = []struct {
	rel     string
	label   string
	warning string
}{
	{"Makefile", "Unmanaged", "add include ivaldi.mk to it yourself"},
	{"ivaldi.mk", "Managed", "the shared make targets may be missing or out of date"},
	{".github/workflows/ci.yml", "Managed", "no shared CI"},
	{hookPath, "Managed", "no pre-commit hook, and core.hooksPath isn't set"},
	{".golangci.yml", "Unmanaged", "the Layer rules aren't enforced"},
}

func TestAdoptionListsExistingFilesAndOverwritesOnlyOnYes(t *testing.T) {
	p := newProject(t)
	p.inOwnRepo("")
	p.write("go.mod", adoptedGoMod)
	for _, f := range overwritable {
		p.write(f.rel, "mine\n")
	}
	fresh := newProject(t)
	if err := fresh.run("example.com/legacy\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// No new binaries, then yes to Makefile and .golangci.yml only.
	if err := p.run("\ny\nn\n\nno\nyes\n", "init"); err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, p.out)
	}

	out := p.out.String()
	var listing strings.Builder
	listing.WriteString("These files already exist:\n")
	for _, f := range overwritable {
		listing.WriteString("  " + f.rel + " (" + f.label + ")\n")
	}
	if !strings.Contains(out, listing.String()) {
		t.Errorf("expected the listing\n%s\ngot:\n%s", listing.String(), out)
	}
	for _, f := range overwritable {
		if got := strings.Count(out, "Overwrite "+f.rel+"? [y/N]: "); got != 1 {
			t.Errorf("expected one question for %s, got %d", f.rel, got)
		}
		overwritten := f.rel == "Makefile" || f.rel == ".golangci.yml"
		want := "mine\n"
		if overwritten {
			want = fresh.read(f.rel)
		}
		if got := p.read(f.rel); got != want {
			t.Errorf("%s: overwritten = %v, want %v", f.rel, got != "mine\n", overwritten)
		}
		if warned := strings.Contains(out, f.warning); warned == overwritten {
			t.Errorf("%s: warned %v about %q, want %v", f.rel, warned, f.warning, !overwritten)
		}
	}
	if p.runner.requestedLine(p.dir, setHooksPath) {
		t.Errorf("expected core.hooksPath not set when the hook was kept")
	}
}

func TestAdoptionKeepsEveryExistingFileAtEndOfInput(t *testing.T) {
	p := newProject(t)
	p.write("go.mod", adoptedGoMod)
	for _, f := range overwritable {
		p.write(f.rel, "mine\n")
	}

	if err := p.run("", "init"); err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, p.out)
	}

	for _, f := range overwritable {
		if got := p.read(f.rel); got != "mine\n" {
			t.Errorf("%s was overwritten: %q", f.rel, got)
		}
		if !strings.Contains(p.out.String(), f.warning) {
			t.Errorf("expected the warning %q, got:\n%s", f.warning, p.out)
		}
	}
	p.read(".forge.yaml")
}

func TestAdoptionAsksEveryQuestionBeforeWritingAnything(t *testing.T) {
	p := newProject(t)
	p.write("go.mod", adoptedGoMod)
	p.write("Makefile", "mine\n")
	before := snapshot(t, p.dir)

	// A new binary whose Kind is never answered: the overwrite questions come
	// after it, and nothing may be written.
	if err := p.run("web\n", "init"); err == nil {
		t.Fatalf("expected init to fail")
	}
	if !maps.Equal(before, snapshot(t, p.dir)) {
		t.Errorf("expected nothing written")
	}
	if len(p.runner.requested) != 1 {
		t.Errorf("expected only the repository check, got %+v", p.runner.requested)
	}
}

func TestAdoptionAppendsOnlyTheMissingGitignoreLines(t *testing.T) {
	for name, tc := range map[string]struct{ existing, want, said string }{
		"both missing":    {"/vendor/\n", "/vendor/\n/bin/\n/logs/\n", "Appending /bin/, /logs/ to .gitignore"},
		"one missing":     {"/logs/\n/tmp/", "/logs/\n/tmp/\n/bin/\n", "Appending /bin/ to .gitignore"},
		"neither missing": {"/bin/\n# mine\n/logs/\n", "/bin/\n# mine\n/logs/\n", ""},
	} {
		t.Run(name, func(t *testing.T) {
			p := newProject(t)
			p.write("go.mod", adoptedGoMod)
			p.write(".gitignore", tc.existing)

			if err := p.run("\n", "init"); err != nil {
				t.Fatalf("init failed: %v\noutput:\n%s", err, p.out)
			}

			if got := p.read(".gitignore"); got != tc.want {
				t.Errorf(".gitignore = %q, want %q", got, tc.want)
			}
			if tc.said != "" && !strings.Contains(p.out.String(), tc.said) {
				t.Errorf("expected %q, got:\n%s", tc.said, p.out)
			}
			if strings.Contains(p.out.String(), "Overwrite .gitignore") {
				t.Errorf("expected no question about .gitignore, got:\n%s", p.out)
			}
		})
	}
}

func TestAdoptionNeverOverwritesTheReadmeOrLayerDocs(t *testing.T) {
	p := newProject(t)
	p.write("go.mod", adoptedGoMod)
	mine := map[string]string{"README.md": "# mine\n", "api/doc.go": "package api // mine\n"}
	for rel, content := range mine {
		p.write(rel, content)
	}

	if err := p.run("\ny\ny\n", "init"); err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, p.out)
	}

	for rel, want := range mine {
		if got := p.read(rel); got != want {
			t.Errorf("%s was overwritten: %q", rel, got)
		}
	}
	if strings.Contains(p.out.String(), "Overwrite") || strings.Contains(p.out.String(), "already exist:") {
		t.Errorf("expected no overwrite questions, got:\n%s", p.out)
	}
}

const (
	raiseGoLine = "Raise the go line to 1.27? [Y/n]: "
	goModEdit   = "go mod edit -go=1.27"
)

func TestAdoptionOffersToRaiseAGoLineBelow127(t *testing.T) {
	for name, tc := range map[string]struct {
		goMod     string
		answer    string
		wantAsked bool
		wantEdit  bool
	}{
		"below, yes":          {goMod: "go 1.26\n", answer: "y\n", wantAsked: true, wantEdit: true},
		"below, default":      {goMod: "go 1.21.3\n", answer: "\n", wantAsked: true, wantEdit: true},
		"below, end of input": {goMod: "go 1.26\n", answer: "", wantAsked: true, wantEdit: true},
		"below, no":           {goMod: "go 1.26\n", answer: "n\n", wantAsked: true, wantEdit: false},
		"no go line":          {goMod: "", answer: "y\n", wantAsked: true, wantEdit: true},
		"at 1.27":             {goMod: "go 1.27\n", answer: "", wantAsked: false},
		"at 1.27.2":           {goMod: "go 1.27.2\n", answer: "", wantAsked: false},
		"above":               {goMod: "go 1.28\n", answer: "", wantAsked: false},
	} {
		t.Run(name, func(t *testing.T) {
			p := newProject(t)
			goMod := "module example.com/legacy\n\n" + tc.goMod
			p.write("go.mod", goMod)

			if err := p.run("\n"+tc.answer, "init"); err != nil {
				t.Fatalf("init failed: %v\noutput:\n%s", err, p.out)
			}

			if asked := strings.Contains(p.out.String(), raiseGoLine); asked != tc.wantAsked {
				t.Errorf("asked = %v, want %v:\n%s", asked, tc.wantAsked, p.out)
			}
			if edited := p.runner.requestedLine(p.dir, goModEdit); edited != tc.wantEdit {
				t.Errorf("%s requested = %v, want %v", goModEdit, edited, tc.wantEdit)
			}
			declined := tc.wantAsked && !tc.wantEdit
			if warned := strings.Contains(p.out.String(), "Warning: keeping the go line"); warned != declined {
				t.Errorf("warned = %v, want %v:\n%s", warned, declined, p.out)
			}
			if got := p.read("go.mod"); got != goMod {
				t.Errorf("expected go.mod left to the go command, got %q", got)
			}
		})
	}
}

func TestInitDoesNotAskAboutTheGoLineOfAGoModItWrites(t *testing.T) {
	p := newProject(t)

	if err := p.run("github.com/acme/widget\n\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if strings.Contains(p.out.String(), raiseGoLine) || p.runner.requestedLine(p.dir, goModEdit) {
		t.Errorf("expected no go line question for a new go.mod, got:\n%s", p.out)
	}
}

func TestAdoptionRaisesTheGoLineBeforeFetchingModules(t *testing.T) {
	p := newProject(t)
	p.write("go.mod", "module example.com/legacy\n\ngo 1.26\n")
	p.write("Makefile", "mine\n")

	// A new tool, keep the Makefile, raise the go line.
	if err := p.run("cli\ntool\nn\ny\n", "init"); err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, p.out)
	}

	out := p.out.String()
	if strings.Index(out, "Overwrite Makefile?") > strings.Index(out, raiseGoLine) {
		t.Errorf("expected the go line question last, got:\n%s", out)
	}
	edit := slices.IndexFunc(p.runner.requested, func(c cli.Command) bool { return c.String() == goModEdit })
	tidy := slices.IndexFunc(p.runner.requested, func(c cli.Command) bool { return c.String() == "go mod tidy" })
	if edit < 0 || tidy < 0 || edit > tidy {
		t.Errorf("expected %s before go mod tidy, got %+v", goModEdit, p.runner.requested)
	}
}

const outsideNote = "the Layer rules cover only the Layer packages"

func TestAdoptionSaysTheLayerRulesCoverOnlyTheLayers(t *testing.T) {
	p := newProject(t)
	p.write("go.mod", adoptedGoMod)
	p.write("internal/lib/lib.go", "package lib\n")
	p.write("pkg/util/util.go", "package util\n")
	p.write("api/handler.go", "package api\n")

	if err := p.run("\n", "init"); err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, p.out)
	}

	out := p.out.String()
	if got := strings.Count(out, outsideNote); got != 1 {
		t.Fatalf("expected the note once, got %d times:\n%s", got, out)
	}
	var line string
	for l := range strings.SplitSeq(out, "\n") {
		if strings.Contains(l, outsideNote) {
			line = l
		}
	}
	if !strings.Contains(line, "internal/, pkg/") || strings.Contains(line, "api/") {
		t.Errorf("expected the note to name internal/ and pkg/ only, got %q", line)
	}
	if got := p.read("internal/lib/lib.go"); got != "package lib\n" {
		t.Errorf("internal/lib/lib.go changed: %q", got)
	}
}

func TestInitSaysNothingAboutTheLayerRulesWithoutCodeOutsideThem(t *testing.T) {
	p := newProject(t)
	p.write("go.mod", adoptedGoMod)
	p.write("cmd/a/main.go", mainSource("a"))
	p.write("models/user.go", "package models\n")

	if err := p.run("tool\n\n", "init"); err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, p.out)
	}
	if strings.Contains(p.out.String(), outsideNote) {
		t.Errorf("expected no note, got:\n%s", p.out)
	}
}

func TestAdoptionTurnsOnAnOverwrittenHookAsExecutable(t *testing.T) {
	p := newProject(t)
	p.inOwnRepo("")
	p.write("go.mod", adoptedGoMod)
	p.write(hookPath, "mine\n") // written 0600

	if err := p.run("\ny\n", "init"); err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, p.out)
	}

	if !strings.Contains(p.read(hookPath), "Managed by Ivaldi") {
		t.Errorf("expected the hook overwritten, got:\n%s", p.read(hookPath))
	}
	info, err := os.Stat(filepath.Join(p.dir, hookPath))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 != 0o111 {
		t.Errorf("expected the overwritten hook to be executable, got %v", info.Mode())
	}
	if !p.runner.requestedLine(p.dir, setHooksPath) {
		t.Errorf("expected core.hooksPath set, requested %+v", p.runner.requested)
	}
}
