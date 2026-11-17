// Package cli is Ivaldi's command boundary. Everything it needs from the
// outside world (the Project directory, the input and output streams, and a
// runner for external processes) is passed in through Env, so nothing below
// it reads the working directory, stdin or stdout, or runs processes itself.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/worldtree-industries/ivaldi/internal/manifest"
	"github.com/worldtree-industries/ivaldi/internal/prompt"
	"github.com/worldtree-industries/ivaldi/internal/scaffold"
)

// Command is one external process the command layer wants run.
type Command struct {
	// Dir is the directory the process runs in (always the Project directory).
	Dir  string
	Name string
	Args []string
}

func (c Command) String() string {
	return strings.Join(append([]string{c.Name}, c.Args...), " ")
}

// Runner runs external processes on behalf of the command layer.
type Runner interface {
	// Run runs cmd to completion and returns its standard output. A non-nil
	// error means the process could not be started or exited unsuccessfully.
	Run(ctx context.Context, cmd Command) ([]byte, error)
}

// Env is everything a command may use from the outside world.
type Env struct {
	// Dir is the Project directory.
	Dir    string
	In     io.Reader
	Out    io.Writer
	Runner Runner
}

// ErrUsage is returned when the arguments don't name a known command.
var ErrUsage = errors.New("usage")

const usage = `Usage: ivaldi <command>

Commands:
  init     Set up a new Project in the current directory
  add      Add binaries to the Project in the current directory
`

// Run dispatches args (without the program name) to a command.
func Run(ctx context.Context, env Env, args []string) error {
	if len(args) == 1 {
		switch args[0] {
		case "init":
			return runInit(ctx, env)
		case "add":
			return runAdd(ctx, env)
		}
	}
	if _, err := io.WriteString(env.Out, usage); err != nil {
		return err
	}
	if len(args) == 0 {
		return fmt.Errorf("%w: no command given", ErrUsage)
	}
	return fmt.Errorf("%w: unknown command %q", ErrUsage, strings.Join(args, " "))
}

type session struct {
	ctx context.Context //nolint:containedctx // Scoped to a single command run.
	env Env
}

func (s session) sayf(format string, a ...any) {
	_, _ = fmt.Fprintf(s.env.Out, format+"\n", a...)
}

func (s session) run(name string, args ...string) error {
	cmd := Command{Dir: s.env.Dir, Name: name, Args: args}
	output, err := s.env.Runner.Run(s.ctx, cmd)
	if len(output) > 0 {
		_, _ = s.env.Out.Write(output)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", cmd, err)
	}
	return nil
}

func (s session) output(name string, args ...string) (string, error) {
	cmd := Command{Dir: s.env.Dir, Name: name, Args: args}
	output, err := s.env.Runner.Run(s.ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("%s: %w", cmd, err)
	}
	return strings.TrimSpace(string(output)), nil
}

func runInit(ctx context.Context, env Env) error {
	s := session{ctx: ctx, env: env}
	p := prompt.New(env.In, env.Out)

	hasManifest, err := manifest.Exists(env.Dir)
	if err != nil {
		return err
	}
	if hasManifest {
		return fmt.Errorf("%s already exists, so this is already an Ivaldi Project: use ivaldi add to add binaries",
			manifest.FileName)
	}

	s.sayf("=== Ivaldi: new Project ===")
	// Ask everything before changing anything, so a failed answer leaves the
	// directory as it was.
	modulePath, err := s.askModulePath(p)
	if err != nil {
		return err
	}

	files, err := scaffold.Skeleton(modulePath)
	if err != nil {
		return err
	}
	m := manifest.Empty()
	plan, err := planBinaries(p, env.Dir, modulePath, m, files, true)
	if err != nil {
		return err
	}

	if err = s.ensureGitRepository(); err != nil {
		return err
	}
	manifestFile, err := m.File()
	if err != nil {
		return err
	}
	// The Manifest goes last: once it exists, init refuses to run again.
	files = append(append(files, plan.files...), manifestFile)
	if err = scaffold.Write(env.Dir, env.Out, files); err != nil {
		return err
	}
	if !plan.empty() {
		if err = s.fetchModules(plan); err != nil {
			return err
		}
	}

	s.applyGitHubSettings()

	s.sayf("Done! Nothing has been committed: review the files, then commit them.")
	return nil
}

// askModulePath returns the module path of an existing go.mod, or asks for
// one, defaulting from origin.
func (s session) askModulePath(p *prompt.Prompter) (string, error) {
	hasGoMod, err := scaffold.Exists(s.env.Dir, "go.mod")
	if err != nil {
		return "", err
	}
	if hasGoMod {
		return readModulePath(s.env.Dir)
	}
	return p.Required("Module path", s.originModulePath())
}

// ensureGitRepository runs git init unless the Project directory is already
// inside a git work tree. It never commits.
func (s session) ensureGitRepository() error {
	if _, err := s.output("git", "rev-parse", "--is-inside-work-tree"); err == nil {
		return nil
	}
	return s.run("git", "init")
}

// originModulePath derives a module path from the origin remote, or returns ""
// when there is no usable remote.
func (s session) originModulePath() string {
	remote, err := s.output("git", "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return modulePathFromRemote(remote)
}

// modulePathFromRemote turns a git remote URL into a module path, for example
// git@github.com:org/repo.git or https://github.com/org/repo into
// github.com/org/repo. It returns "" for a URL it can't make sense of.
func modulePathFromRemote(remote string) string {
	var host, path string
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil {
			return ""
		}
		host, path = u.Hostname(), u.Path
	} else {
		// scp-like syntax: [user@]host:path
		var ok bool
		if host, path, ok = strings.Cut(remote, ":"); !ok {
			return ""
		}
		if _, h, hasUser := strings.Cut(host, "@"); hasUser {
			host = h
		}
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	if host == "" || path == "" {
		return ""
	}
	return host + "/" + path
}

func runAdd(ctx context.Context, env Env) error {
	s := session{ctx: ctx, env: env}
	p := prompt.New(env.In, env.Out)

	hasManifest, err := manifest.Exists(env.Dir)
	if err != nil {
		return err
	}
	if !hasManifest {
		return fmt.Errorf("no %s here, so this isn't an Ivaldi Project yet: run ivaldi init first", manifest.FileName)
	}
	m, err := manifest.Load(env.Dir)
	if err != nil {
		return err
	}
	modulePath, err := readModulePath(env.Dir)
	if err != nil {
		return err
	}

	s.sayf("=== Ivaldi: add binaries ===")
	plan, err := planBinaries(p, env.Dir, modulePath, m, nil, false)
	if err != nil {
		return err
	}

	if err = scaffold.Write(env.Dir, env.Out, plan.files); err != nil {
		return err
	}
	if err = m.Save(env.Dir); err != nil {
		return err
	}
	if err = s.fetchModules(plan); err != nil {
		return err
	}

	s.sayf("Done! Nothing has been committed: review the files, then commit them.")
	return nil
}

// readModulePath returns the module path declared in the Project's go.mod.
func readModulePath(dir string) (string, error) {
	content, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("reading the module path: %w", err)
	}
	for line := range strings.SplitSeq(string(content), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module"); ok && rest != "" &&
			(rest[0] == ' ' || rest[0] == '\t') {
			if path := strings.Trim(strings.TrimSpace(rest), `"`); path != "" {
				return path, nil
			}
		}
	}
	return "", errors.New("go.mod declares no module path")
}
