package cli

import (
	"errors"
	"fmt"
	"io"
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
	out        io.Writer
	dir        string
	modulePath string
	// manifest gets each planned binary added to it.
	manifest *manifest.Manifest
	// planned are files that will be written in the same run, and count as
	// existing.
	planned []scaffold.File

	names []string
	// discovered are the binaries already in cmd/ that were recorded.
	discovered []string
	// files are each binary's main and any extra files its Kind needs.
	files []scaffold.File
	// modules are the module queries to go get, without duplicates.
	modules []string
}

// empty reports whether no new binaries are planned.
func (b *binaryPlan) empty() bool {
	return len(b.names) == 0
}

// newBinaryPlan starts a plan that adds binaries to m. planned are files that
// will be written in the same run, and count as existing.
func newBinaryPlan(
	p *prompt.Prompter, out io.Writer, dir, modulePath string, m *manifest.Manifest, planned []scaffold.File,
) *binaryPlan {
	return &binaryPlan{prompt: p, out: out, dir: dir, modulePath: modulePath, manifest: m, planned: planned}
}

// planBinaries runs the binaries prompt: it asks for names, validates all of
// them, then asks each one's Kind and the Kind's follow-up questions, and adds
// each binary to m. It writes nothing. planned are files that will be written
// in the same run, and count as existing. When allowNone is true, a blank
// answer means no binaries.
func planBinaries(
	p *prompt.Prompter, out io.Writer, dir, modulePath string, m *manifest.Manifest, planned []scaffold.File,
	allowNone bool,
) (*binaryPlan, error) {
	plan := newBinaryPlan(p, out, dir, modulePath, m, planned)
	if err := plan.askNew(allowNone); err != nil {
		return nil, err
	}
	return plan, nil
}

// askNew asks for new binaries' names, validates all of them, then plans each.
func (b *binaryPlan) askNew(allowNone bool) error {
	names, err := askNames(b.prompt, allowNone)
	if err != nil {
		return err
	}
	if err = validateNames(b.dir, names, b.manifest.Binaries()); err != nil {
		return err
	}
	for _, name := range names {
		if err = b.add(name); err != nil {
			return err
		}
	}
	return nil
}

// exists reports whether rel is in the Project, or planned in this run.
func (b *binaryPlan) exists(rel string) (bool, error) {
	isRel := func(f scaffold.File) bool { return f.Path == rel }
	if slices.ContainsFunc(b.planned, isRel) || slices.ContainsFunc(b.files, isRel) {
		return true, nil
	}
	return scaffold.Exists(b.dir, rel)
}

// ask asks name's Kind and the Kind's follow-up questions, and plans any extra
// files the answers need.
func (b *binaryPlan) ask(name string) (kind.Kind, []manifest.Setting, error) {
	kindName, err := b.prompt.Choice("Kind of "+name, kind.Names())
	if err != nil {
		return kind.Kind{}, nil, err
	}
	k, _ := kind.Lookup(kindName)

	var answers kind.Answers
	if k.Ask != nil {
		q := &kind.Questions{Prompt: b.prompt, Binaries: b.manifest.Binaries(), Exists: b.exists}
		if answers, err = k.Ask(q, name); err != nil {
			return kind.Kind{}, nil, err
		}
	}
	b.files = append(b.files, answers.Files...)
	return k, answers.Settings, nil
}

// add asks a new binary's Kind and the Kind's follow-up questions, adds it to
// the Manifest, and plans its files.
func (b *binaryPlan) add(name string) error {
	k, settings, err := b.ask(name)
	if err != nil {
		return err
	}
	main, err := k.Main(name, b.modulePath, settings)
	if err != nil {
		return err
	}
	if err = b.manifest.Add(name, k.Name, settings); err != nil {
		return err
	}
	b.names = append(b.names, name)
	b.files = append(b.files, main)
	for _, mod := range k.Modules {
		if !slices.Contains(b.modules, mod) {
			b.modules = append(b.modules, mod)
		}
	}
	return nil
}

// record asks a discovered binary's Kind and the Kind's follow-up questions
// and adds it to the Manifest. Its main is never regenerated and its Kind's
// modules aren't fetched: the code is the user's.
func (b *binaryPlan) record(d discoveredBinary) error {
	k, settings, err := b.ask(d.name)
	if err != nil {
		return err
	}
	if d.dockerfile {
		settings = append(settings, manifest.Setting{Key: manifest.SettingBuild, Value: manifest.BuildDockerfile})
		_, _ = fmt.Fprintf(b.out, "cmd/%s/Dockerfile exists, so %s is recorded with %s: %s.\n",
			d.name, d.name, manifest.SettingBuild, manifest.BuildDockerfile)
	}
	if err = b.manifest.Add(d.name, k.Name, settings); err != nil {
		return err
	}
	b.discovered = append(b.discovered, d.name)
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

// invalidNameError is the error for a name that fails the binary-name rules.
func invalidNameError(name string) error {
	return fmt.Errorf("invalid binary name %q: use a lowercase letter followed by up to 62 "+
		"lowercase letters, digits or hyphens", name)
}

// validateNames checks every name before anything is asked or written, and
// names the first bad entry.
func validateNames(dir string, names []string, existing []manifest.Binary) error {
	for i, name := range names {
		switch {
		case !binaryName.MatchString(name):
			return invalidNameError(name)
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
