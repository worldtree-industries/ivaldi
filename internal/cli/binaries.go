package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/worldtree-industries/ivaldi/internal/kind"
	"github.com/worldtree-industries/ivaldi/internal/manifest"
	"github.com/worldtree-industries/ivaldi/internal/prompt"
	"github.com/worldtree-industries/ivaldi/internal/scaffold"
)

const (
	namesQuestion        = "Binary names (comma-separated)"
	namesOrBlankQuestion = "Binary names (comma-separated, blank for none)"
)

var binaryName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// binaryPlan is everything the binaries prompt decided, before anything is
// written.
type binaryPlan struct {
	prompt     *prompt.Prompter
	dir        string
	modulePath string
	// manifest gets each planned binary added to it.
	manifest *manifest.Manifest
	// planned are files that will be written in the same run, and count as
	// existing.
	planned []scaffold.File

	names []string
	// files are each binary's main and any extra files its Kind needs.
	files []scaffold.File
	// modules are the module queries to go get, without duplicates.
	modules []string
}

func (b *binaryPlan) empty() bool {
	return len(b.names) == 0
}

// planBinaries runs the binaries prompt: it asks for names, validates all of
// them, then asks each one's Kind and the Kind's follow-up questions, and adds
// each binary to m. It writes nothing. planned are files that will be written
// in the same run, and count as existing. When allowNone is true, a blank
// answer means no binaries.
func planBinaries(
	p *prompt.Prompter, dir, modulePath string, m *manifest.Manifest, planned []scaffold.File, allowNone bool,
) (*binaryPlan, error) {
	names, err := askNames(p, allowNone)
	if err != nil {
		return nil, err
	}
	if err = validateNames(dir, names, m.Binaries()); err != nil {
		return nil, err
	}

	plan := &binaryPlan{prompt: p, dir: dir, modulePath: modulePath, manifest: m, planned: planned}
	for _, name := range names {
		if err = plan.add(name); err != nil {
			return nil, err
		}
	}
	return plan, nil
}

// exists reports whether rel is in the Project, or planned in this run.
func (b *binaryPlan) exists(rel string) (bool, error) {
	isRel := func(f scaffold.File) bool { return f.Path == rel }
	if slices.ContainsFunc(b.planned, isRel) || slices.ContainsFunc(b.files, isRel) {
		return true, nil
	}
	return scaffold.Exists(b.dir, rel)
}

// add asks name's Kind and the Kind's follow-up questions, adds it to the
// Manifest, and plans its files.
func (b *binaryPlan) add(name string) error {
	kindName, err := b.prompt.Choice("Kind of "+name, kind.Names())
	if err != nil {
		return err
	}
	k, _ := kind.Lookup(kindName)

	var answers kind.Answers
	if k.Ask != nil {
		q := &kind.Questions{Prompt: b.prompt, Binaries: b.manifest.Binaries(), Exists: b.exists}
		if answers, err = k.Ask(q, name); err != nil {
			return err
		}
	}
	main, err := k.Main(name, b.modulePath, answers.Settings)
	if err != nil {
		return err
	}
	if err = b.manifest.Add(name, k.Name, answers.Settings); err != nil {
		return err
	}
	b.names = append(b.names, name)
	b.files = append(b.files, main)
	b.files = append(b.files, answers.Files...)
	for _, mod := range k.Modules {
		if !slices.Contains(b.modules, mod) {
			b.modules = append(b.modules, mod)
		}
	}
	return nil
}

func askNames(p *prompt.Prompter, allowNone bool) ([]string, error) {
	var answer string
	if allowNone {
		answer = p.String(namesOrBlankQuestion, "")
	} else {
		var err error
		if answer, err = p.Required(namesQuestion, ""); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(answer) == "" {
		return nil, nil
	}
	var names []string
	for name := range strings.SplitSeq(answer, ",") {
		names = append(names, strings.TrimSpace(name))
	}
	return names, nil
}

// validateNames checks every name before anything is asked or written, and
// names the first bad entry.
func validateNames(dir string, names []string, existing []manifest.Binary) error {
	for i, name := range names {
		switch {
		case !binaryName.MatchString(name):
			return fmt.Errorf("invalid binary name %q: use a lowercase letter followed by up to 62 "+
				"lowercase letters, digits or hyphens", name)
		case slices.Contains(names[:i], name):
			return fmt.Errorf("binary %q is listed more than once", name)
		case slices.ContainsFunc(existing, func(b manifest.Binary) bool { return b.Name == name }):
			return fmt.Errorf("binary %q is already in %s", name, manifest.FileName)
		}
		_, err := os.Stat(filepath.Join(dir, "cmd", name))
		if err == nil {
			return fmt.Errorf("binary %q: cmd/%s already exists", name, name)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// fetchModules fetches each module the new binaries' Kinds need, then tidies
// the module. A failure doesn't fail the command, since the files are already
// written: it warns and prints the commands still to run.
func (s session) fetchModules(plan *binaryPlan) {
	var cmds []Command
	for _, mod := range plan.modules {
		cmds = append(cmds, s.command("go", "get", mod))
	}
	cmds = append(cmds, s.command("go", "mod", "tidy"))
	for i, cmd := range cmds {
		if _, err := s.exec(cmd, true); err != nil {
			s.sayf("Warning: %s failed (%v). To finish, run:", cmd, err)
			for _, rest := range cmds[i:] {
				s.sayf("  %s", shellQuote(append([]string{rest.Name}, rest.Args...)))
			}
			break
		}
	}
	s.sayf("Added %s.", strings.Join(plan.names, ", "))
}
