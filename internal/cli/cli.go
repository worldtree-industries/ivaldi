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
  init     Initialize a new Go project interactively
`

// Run dispatches args (without the program name) to a command.
func Run(ctx context.Context, env Env, args []string) error {
	if len(args) != 1 || args[0] != "init" {
		if _, err := io.WriteString(env.Out, usage); err != nil {
			return err
		}
		if len(args) == 0 {
			return fmt.Errorf("%w: no command given", ErrUsage)
		}
		return fmt.Errorf("%w: unknown command %q", ErrUsage, strings.Join(args, " "))
	}
	return runInit(ctx, env)
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
	modulePath, err := p.Required("Module path", s.originModulePath())
	if err != nil {
		return err
	}

	if err = s.ensureGitRepository(); err != nil {
		return err
	}

	files, err := scaffold.Skeleton(modulePath)
	if err != nil {
		return err
	}
	// The Manifest goes last: once it exists, init refuses to run again.
	if err = scaffold.Write(env.Dir, env.Out, append(files, manifest.New())); err != nil {
		return err
	}

	s.sayf("Done! Nothing has been committed: review the files, then commit them.")
	return nil
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
