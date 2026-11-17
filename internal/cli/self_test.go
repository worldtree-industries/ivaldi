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
