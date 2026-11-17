package cli

import (
	"bytes"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/worldtree-industries/ivaldi/internal/manifest"
	"github.com/worldtree-industries/ivaldi/internal/scaffold"
)

// layersAndCmd are the top-level directories the Layer rules and the
// Manifest cover.
//
//nolint:gochecknoglobals // Constant list.
var layersAndCmd = []string{
	scaffold.LayerAPI, scaffold.LayerService, scaffold.LayerData, scaffold.LayerModels, scaffold.LayerClient, "cmd",
}

// discoveredBinary is a cmd/<name>/ directory that already holds package main.
type discoveredBinary struct {
	name string
	// dockerfile is set when cmd/<name>/Dockerfile exists.
	dockerfile bool
}

// discovery is what Adoption found in an existing repo's code.
type discovery struct {
	// binaries are the cmd/<name>/ directories holding package main, in name
	// order.
	binaries []discoveredBinary
	// strays are the other directories holding package main (slash-relative,
	// "." for the root), which Ivaldi leaves alone.
	strays []string
	// outside are the top-level directories outside the Layers and cmd/ that
	// hold Go code, which Ivaldi leaves alone.
	outside []string
	// rootDockerfile is set when the root directory has a Dockerfile, which
	// Ivaldi doesn't use.
	rootDockerfile bool
}

// discover finds the existing binaries in dir, and checks each one's name
// against the binary-name rules. It reads package clauses from disk, so it
// runs no commands.
func discover(dir string) (*discovery, error) {
	d := &discovery{}
	err := filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
		if err != nil || !e.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel != "." && skippedDir(path, e.Name()) {
			return fs.SkipDir
		}
		return d.visit(path, rel)
	})
	if err != nil {
		return nil, err
	}
	if d.rootDockerfile, err = scaffold.Exists(dir, "Dockerfile"); err != nil {
		return nil, err
	}
	return d, nil
}

// visit records what the directory at path (rel, slash-relative) holds.
func (d *discovery) visit(path, rel string) error {
	hasGo, isMain, err := readPackages(path)
	if err != nil || !hasGo {
		return err
	}
	if top, _, _ := strings.Cut(rel, "/"); rel != "." && !slices.Contains(layersAndCmd, top) &&
		!slices.Contains(d.outside, top) {
		d.outside = append(d.outside, top)
	}
	if !isMain {
		return nil
	}
	name, inCmd := strings.CutPrefix(rel, "cmd/")
	if !inCmd || strings.Contains(name, "/") {
		d.strays = append(d.strays, rel)
		return nil
	}
	if !binaryName.MatchString(name) {
		return fmt.Errorf("cmd/%s: %w", name, invalidNameError(name))
	}
	hasDockerfile, err := scaffold.Exists(path, "Dockerfile")
	if err != nil {
		return err
	}
	d.binaries = append(d.binaries, discoveredBinary{name: name, dockerfile: hasDockerfile})
	return nil
}

// names returns the discovered binaries' names, comma-separated.
func (d *discovery) names() string {
	var names []string
	for _, b := range d.binaries {
		names = append(names, b.name)
	}
	return strings.Join(names, ", ")
}

// skippedDir reports whether the go tool ignores the directory at path
// (named name), or it's another module's.
func skippedDir(path, name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "vendor" || name == "testdata" {
		return true
	}
	_, err := os.Stat(filepath.Join(path, scaffold.GoModPath))
	return err == nil
}

// warn warns about what Adoption found but leaves alone.
func (s session) warn(d *discovery) {
	for _, dir := range d.strays {
		if dir == "." {
			dir = "the root directory"
		}
		s.sayf("Warning: %s holds package main, but Ivaldi manages only cmd/<name>/ binaries, so it's left alone.", dir)
	}
	if len(d.outside) > 0 {
		s.sayf("Note: %s/ hold code outside the Layers, which is left alone: the Layer rules cover only the "+
			"Layer packages (%s).", strings.Join(d.outside, "/, "), strings.Join(layersAndCmd[:len(layersAndCmd)-1], ", "))
	}
	if d.rootDockerfile {
		s.sayf("Warning: Ivaldi doesn't use the root Dockerfile: move it to cmd/<name>/Dockerfile " +
			"for the binary it builds, and set build: dockerfile on that binary in " + manifest.FileName + ".")
	}
}

// readPackages reports whether dir directly holds any non-test Go files, and
// whether any of them declares package main.
func readPackages(dir string) (bool, bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, false, err
	}
	hasGo := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		hasGo = true
		f, parseErr := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, parser.PackageClauseOnly)
		if parseErr != nil {
			return false, false, parseErr
		}
		if f.Name.Name == "main" {
			return true, true, nil
		}
	}
	return hasGo, false, nil
}

// askOverwrites lists each of files that already exists and that init would
// otherwise skip, then asks whether to overwrite each one (default no, and end
// of input is no), marking those to overwrite. It returns whether the
// pre-commit hook will be Ivaldi's.
func (s session) askOverwrites(files []scaffold.File) (bool, error) {
	var existing []int
	for i, f := range files {
		if f.IfExists != scaffold.AskToOverwrite {
			continue
		}
		exists, err := scaffold.Exists(s.env.Dir, f.Path)
		if err != nil {
			return false, err
		}
		if exists {
			existing = append(existing, i)
		}
	}
	if len(existing) == 0 {
		return true, nil
	}

	s.sayf("These files already exist:")
	for _, i := range existing {
		label := "Unmanaged"
		if files[i].Managed {
			label = "Managed"
		}
		s.sayf("  %s (%s)", files[i].Path, label)
	}
	ourHook := true
	for _, i := range existing {
		f := &files[i]
		if f.Overwrite = s.prompt.Bool("Overwrite "+f.Path+"?", false); f.Overwrite {
			continue
		}
		if f.Path == scaffold.PreCommitHookPath {
			ourHook = false
		}
		warning := f.KeptWarning
		if warning == "" {
			warning = "it may not match what Ivaldi expects"
		}
		s.sayf("Warning: keeping your %s: %s.", f.Path, warning)
	}
	return ourHook, nil
}

// planAppends plans appending to each existing file that takes it the lines
// of Ivaldi's that it lacks, without asking.
func (s session) planAppends(files []scaffold.File) error {
	for i := range files {
		if files[i].IfExists != scaffold.AppendMissingLines {
			continue
		}
		if err := s.planAppend(&files[i]); err != nil {
			return err
		}
	}
	return nil
}

// planAppend plans appending to f, if it exists, the lines of f's content
// that it lacks.
func (s session) planAppend(f *scaffold.File) error {
	content, err := os.ReadFile(filepath.Join(s.env.Dir, filepath.FromSlash(f.Path)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for line := range strings.SplitSeq(string(content), "\n") {
		have[strings.TrimSpace(line)] = true
	}
	var missing []string
	for line := range strings.SplitSeq(string(f.Content), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") && !have[line] {
			missing = append(missing, line)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if len(content) > 0 && !bytes.HasSuffix(content, []byte("\n")) {
		content = append(content, '\n')
	}
	content = append(content, strings.Join(missing, "\n")+"\n"...)
	f.Content = content
	f.Overwrite = true
	s.sayf("Appending %s to %s.", strings.Join(missing, ", "), f.Path)
	return nil
}
