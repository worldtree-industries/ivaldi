package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/worldtree-industries/ivaldi/internal/manifest"
)

// ivaldiRoot is Ivaldi's own repository, which is itself a Project.
const ivaldiRoot = "../.."

func TestIvaldiCIIsTheCallerWorkflowInitWrites(t *testing.T) {
	p := newProject(t)
	if err := p.run("github.com/worldtree-industries/ivaldi\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	own, err := os.ReadFile(filepath.Join(ivaldiRoot, filepath.FromSlash(ciPath)))
	if err != nil {
		t.Fatalf("expected Ivaldi to have its own %s: %v", ciPath, err)
	}
	if string(own) != p.read(ciPath) {
		t.Errorf("expected Ivaldi's %s to be exactly the one init writes; copy it from a fresh init", ciPath)
	}
}

func TestIvaldiSharedMakeFileIsTheOneInitWrites(t *testing.T) {
	p := newProject(t)
	if err := p.run("github.com/worldtree-industries/ivaldi\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	own, err := os.ReadFile(filepath.Join(ivaldiRoot, "ivaldi.mk"))
	if err != nil {
		t.Fatalf("expected Ivaldi to have its own ivaldi.mk: %v", err)
	}
	if string(own) != p.read("ivaldi.mk") {
		t.Errorf("expected Ivaldi's ivaldi.mk to be exactly the one init writes; copy it from a fresh init")
	}
}

func TestIvaldiPreCommitHookIsTheOneInitWrites(t *testing.T) {
	p := newProject(t)
	if err := p.run("github.com/worldtree-industries/ivaldi\n", "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	ownPath := filepath.Join(ivaldiRoot, filepath.FromSlash(hookPath))
	own, err := os.ReadFile(ownPath)
	if err != nil {
		t.Fatalf("expected Ivaldi to have its own %s: %v", hookPath, err)
	}
	if string(own) != p.read(hookPath) {
		t.Errorf("expected Ivaldi's %s to be exactly the one init writes; copy it from a fresh init", hookPath)
	}
	if info, statErr := os.Stat(ownPath); statErr != nil || info.Mode().Perm()&0o111 != 0o111 {
		t.Errorf("expected Ivaldi's %s to be executable, got %v (%v)", hookPath, info.Mode(), statErr)
	}
}

func TestIvaldiMakefileLeavesSetupHooksToTheSharedMakeFile(t *testing.T) {
	own, err := os.ReadFile(filepath.Join(ivaldiRoot, "Makefile"))
	if err != nil {
		t.Fatalf("expected Ivaldi to have its own Makefile: %v", err)
	}
	if strings.Contains(string(own), "\nsetup-hooks:") {
		t.Errorf("expected setup-hooks to come only from ivaldi.mk, got Ivaldi's Makefile:\n%s", own)
	}
}

func TestIvaldiMakefileIncludesTheSharedMakeFileAndKeepsE2E(t *testing.T) {
	own, err := os.ReadFile(filepath.Join(ivaldiRoot, "Makefile"))
	if err != nil {
		t.Fatalf("expected Ivaldi to have its own Makefile: %v", err)
	}
	for _, want := range []string{"\ninclude ivaldi.mk\n", "\ne2e:\n"} {
		if !strings.Contains(string(own), want) {
			t.Errorf("expected Ivaldi's Makefile to contain %q, got:\n%s", want, own)
		}
	}
}

func TestIvaldiManifestDeclaresIvaldiAsATool(t *testing.T) {
	m, err := manifest.Load(ivaldiRoot)
	if err != nil {
		t.Fatalf("expected Ivaldi to have its own Manifest: %v", err)
	}
	bins := m.Binaries()
	if len(bins) != 1 || bins[0].Name != "ivaldi" || bins[0].Kind != "tool" {
		t.Errorf("expected exactly ivaldi as a tool, got %+v", bins)
	}
}
