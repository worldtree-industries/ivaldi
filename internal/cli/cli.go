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
  init     Set up a Project in the current directory, new or with existing code
  add      Add binaries to the Project in the current directory
`

// Run dispatches args (without the program name) to a command.
func Run(ctx context.Context, env Env, args []string) error {
	if len(args) == 1 {
		switch args[0] {
		case "init":
			return initCommand.execute(ctx, env)
		case "add":
			return addCommand.execute(ctx, env)
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

// command is init or add: what they share around their own steps.
type command struct {
	// title is shown when the command starts.
	title string
	// needsManifest is whether the Project must already have a Manifest. If
	// it doesn't match, the command refuses with refusal before doing anything.
	needsManifest bool
	refusal       string
	run           func(s *session) error
}

//nolint:gochecknoglobals // Constant command table.
var (
	initCommand = command{
		title:         "new Project",
		needsManifest: false,
		refusal: manifest.FileName + " already exists, so this is already an Ivaldi Project: " +
			"use ivaldi add to add binaries",
		run: runInit,
	}
	addCommand = command{
		title:         "add binaries",
		needsManifest: true,
		refusal:       "no " + manifest.FileName + " here, so this isn't an Ivaldi Project yet: run ivaldi init first",
		run:           runAdd,
	}
)

func (c command) execute(ctx context.Context, env Env) error {
	hasManifest, err := manifest.Exists(env.Dir)
	if err != nil {
		return err
	}
	if hasManifest != c.needsManifest {
		return errors.New(c.refusal)
	}

	s := &session{ctx: ctx, env: env, prompt: prompt.New(env.In, env.Out)}
	s.sayf("=== Ivaldi: %s ===", c.title)
	if err = c.run(s); err != nil {
		return err
	}
	s.sayf("Done! Nothing has been committed: review the files, then commit them.")
	return nil
}

type session struct {
	ctx    context.Context //nolint:containedctx // Scoped to a single command run.
	env    Env
	prompt *prompt.Prompter
	// ownRepo is set when the Project directory was already the top level of
	// a git work tree when the command started. Otherwise any repository it's
	// in belongs to something else, and its origin isn't the Project's.
	ownRepo bool
}

func (s session) sayf(format string, a ...any) {
	_, _ = fmt.Fprintf(s.env.Out, format+"\n", a...)
}

// command returns the Command for name and args, run in the Project directory.
func (s session) command(name string, args ...string) Command {
	return Command{Dir: s.env.Dir, Name: name, Args: args}
}

// exec runs cmd and returns its standard output, also echoing it to the user
// when echo is set. The error is the runner's, unwrapped.
func (s session) exec(cmd Command, echo bool) ([]byte, error) {
	output, err := s.env.Runner.Run(s.ctx, cmd)
	if echo && len(output) > 0 {
		_, _ = s.env.Out.Write(output)
	}
	return output, err
}

// run runs a command, echoing its output to the user.
func (s session) run(name string, args ...string) error {
	cmd := s.command(name, args...)
	if _, err := s.exec(cmd, true); err != nil {
		return fmt.Errorf("%s: %w", cmd, err)
	}
	return nil
}

// output runs a command and returns its trimmed standard output.
func (s session) output(name string, args ...string) (string, error) {
	cmd := s.command(name, args...)
	output, err := s.exec(cmd, false)
	if err != nil {
		return "", fmt.Errorf("%s: %w", cmd, err)
	}
	return strings.TrimSpace(string(output)), nil
}

func runInit(s *session) error {
	s.ownRepo = s.isRepositoryTopLevel()
	found, err := discover(s.env.Dir)
	if err != nil {
		return err
	}
	s.warn(found)
	// Ask everything before changing anything, so a failed answer leaves the
	// directory as it was.
	modulePath, err := s.askModulePath()
	if err != nil {
		return err
	}

	files, err := scaffold.Skeleton(modulePath)
	if err != nil {
		return err
	}
	m := manifest.Empty()
	plan := newBinaryPlan(s.prompt, s.env.Out, s.env.Dir, modulePath, m, files)
	if len(found.binaries) > 0 {
		s.sayf("Found existing binaries in cmd/: %s. Their code is left as it is.", found.names())
	}
	for _, d := range found.binaries {
		if err = plan.record(d); err != nil {
			return err
		}
	}
	if err = plan.askNew(true); err != nil {
		return err
	}
	ourHook, err := s.askOverwrites(files)
	if err != nil {
		return err
	}
	if err = s.planGitignore(files); err != nil {
		return err
	}
	raiseGoLine, err := s.askGoLine()
	if err != nil {
		return err
	}

	if !s.ownRepo {
		if err = s.run("git", "init"); err != nil {
			return err
		}
	}
	manifestFile, err := m.File()
	if err != nil {
		return err
	}
	// The Manifest goes last: once it exists, init refuses to run again.
	files = append(append(files, plan.files...), manifestFile)
	if err = scaffold.Write(s.env.Dir, s.env.Out, files); err != nil {
		return err
	}
	if raiseGoLine {
		s.raiseGoLine()
	}
	// The Project directory is its own repository's top level by now, so this
	// never touches the config of a repository it's merely inside.
	if ourHook {
		s.turnOnHooks()
	}
	if !plan.empty() {
		s.fetchModules(plan)
	}

	s.applyGitHubSettings()
	return nil
}

// turnOnHooks points the Project's repository at its Managed hooks. A failure
// is a warning with the command to run by hand, never an error.
func (s session) turnOnHooks() {
	cmd := s.command("git", "config", "core.hooksPath", scaffold.HooksDir)
	if _, err := s.exec(cmd, true); err != nil {
		s.sayf("Warning: %s failed (%v). To turn on the pre-commit hook, run:", cmd, err)
		s.sayf("  %s", shellQuote(append([]string{cmd.Name}, cmd.Args...)))
	}
}

// isRepositoryTopLevel reports whether the Project directory is the top level
// of a git work tree, not just somewhere inside one.
func (s session) isRepositoryTopLevel() bool {
	top, err := s.output("git", "rev-parse", "--show-toplevel")
	if err != nil || top == "" {
		return false
	}
	return sameDir(top, s.env.Dir)
}

// sameDir reports whether a and b are the same directory, after resolving
// symlinks.
func sameDir(a, b string) bool {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		return false
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		return false
	}
	return ra == rb
}

// origin returns the URL of the Project's origin remote, or "" when it has
// none. A repository the Project is merely inside isn't the Project's, and one
// init creates has no remotes, so neither has an origin.
func (s session) origin() string {
	if !s.ownRepo {
		return ""
	}
	remote, err := s.output("git", "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return remote
}

func runAdd(s *session) error {
	m, err := manifest.Load(s.env.Dir)
	if err != nil {
		return err
	}
	modulePath, err := readModulePath(s.env.Dir)
	if err != nil {
		return err
	}

	plan, err := planBinaries(s.prompt, s.env.Out, s.env.Dir, modulePath, m, nil, false)
	if err != nil {
		return err
	}

	if err = scaffold.Write(s.env.Dir, s.env.Out, plan.files); err != nil {
		return err
	}
	if err = m.Save(s.env.Dir); err != nil {
		return err
	}
	s.fetchModules(plan)
	return nil
}
