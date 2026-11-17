package cli_test

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/worldtree-industries/ivaldi/internal/scaffold"
)

const ciPath = ".github/workflows/ci.yml"

func TestInitWritesCICallerWorkflowIdenticalInEveryProject(t *testing.T) {
	widget, gadget := newProject(t), newProject(t)
	if err := widget.run("github.com/acme/widget\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if err := gadget.run("example.com/gadget\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	ci := widget.read(ciPath)
	if ci != gadget.read(ciPath) {
		t.Errorf("expected the CI workflow to be byte-identical in every Project")
	}
	if !strings.Contains(ci, "Managed by Ivaldi") {
		t.Errorf("expected the CI workflow to say it is Managed, got:\n%s", ci)
	}
}

func TestInitCIWorkflowPinsSharedWorkflowBySHAWithVersionComment(t *testing.T) {
	p := newProject(t)
	if err := p.run("github.com/acme/widget\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	ci := p.read(ciPath)

	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(scaffold.SharedWorkflowSHA) {
		t.Fatalf("SharedWorkflowSHA must be a full commit SHA, got %q", scaffold.SharedWorkflowSHA)
	}
	want := "uses: worldtree-industries/workflows/.github/workflows/go.yml@" +
		scaffold.SharedWorkflowSHA + " # " + scaffold.SharedWorkflowVersion + "\n"
	if !strings.Contains(ci, want) {
		t.Errorf("expected the CI workflow to contain %q, got:\n%s", want, ci)
	}
	if n := strings.Count(ci, "uses:"); n != 1 {
		t.Errorf("expected exactly one uses:, got %d", n)
	}
}

func TestInitCIWorkflowPassesOnlyHarborSecretsWithReadOnlyPermissions(t *testing.T) {
	p := newProject(t)
	if err := p.run("github.com/acme/widget\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	ci := p.read(ciPath)

	if strings.Contains(ci, "inherit") {
		t.Errorf("expected no secrets: inherit, got:\n%s", ci)
	}
	secrets := regexp.MustCompile(`\$\{\{ secrets\.(\w+) \}\}`).FindAllStringSubmatch(ci, -1)
	var names []string
	for _, m := range secrets {
		names = append(names, m[1])
	}
	if strings.Join(names, ",") != "HARBOR_USERNAME,HARBOR_PASSWORD" {
		t.Errorf("expected only HARBOR_USERNAME and HARBOR_PASSWORD to be passed, got %v", names)
	}
	for _, line := range []string{
		"      HARBOR_USERNAME: ${{ secrets.HARBOR_USERNAME }}\n",
		"      HARBOR_PASSWORD: ${{ secrets.HARBOR_PASSWORD }}\n",
	} {
		if !strings.Contains(ci, line) {
			t.Errorf("expected secret passed by name %q, got:\n%s", line, ci)
		}
	}
	if !strings.Contains(ci, "\npermissions:\n  contents: read\n\n") {
		t.Errorf("expected top-level permissions to be only contents: read, got:\n%s", ci)
	}
	if strings.Contains(ci, "with:") {
		t.Errorf("expected no inputs to be set on the shared workflow, got:\n%s", ci)
	}
}

func TestInitCIWorkflowTriggersOnMainPullRequestsPushesAndReleaseTags(t *testing.T) {
	p := newProject(t)
	if err := p.run("github.com/acme/widget\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	ci := p.read(ciPath)

	want := "on:\n" +
		"  pull_request:\n" +
		"    branches: [main]\n" +
		"  push:\n" +
		"    branches: [main]\n" +
		"    tags: ['v*.*.*']\n"
	if !strings.Contains(ci, want) {
		t.Errorf("expected triggers\n%s\ngot:\n%s", want, ci)
	}
}

func TestInitCIWorkflowPassesActionlint(t *testing.T) {
	actionlint, err := exec.LookPath("actionlint")
	if err != nil {
		t.Skip("actionlint not on PATH")
	}
	p := newProject(t)
	if err = p.run("github.com/acme/widget\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	cmd := exec.CommandContext(t.Context(), actionlint, "-no-color", filepath.FromSlash(ciPath))
	cmd.Dir = p.dir
	if out, lintErr := cmd.CombinedOutput(); lintErr != nil {
		t.Errorf("actionlint failed: %v\n%s", lintErr, out)
	}
}
