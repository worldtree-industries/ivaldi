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

const (
	choiceHTTP        = 0
	choiceWorker      = 1
	choiceCLI         = 2
	choiceInteractive = 3
)

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

func runInit(ctx context.Context, env Env) error {
	s := session{ctx: ctx, env: env}
	p := prompt.New(env.In, env.Out)
	baseDirName := filepath.Base(env.Dir)

	projectCfg := &scaffold.ProjectConfig{GoVersion: "1.26"}

	s.sayf("=== Ivaldi Project Scaffold ===")
	projectCfg.ModulePath = p.String("Module Path", baseDirName)

	s.sayf("\nConfigure binaries (comma separated names). Leave blank to finish.")
	projectCfg.Binaries = askBinaries(p, p.String("Binaries", baseDirName))

	projectCfg.SetupCI = p.Bool("Setup GitHub Actions CI?", false)

	s.sayf("\nInitializing go.mod...")
	hasGoMod, err := scaffold.Exists(env.Dir, "go.mod")
	if err != nil {
		return err
	}
	if hasGoMod {
		s.sayf("go.mod already exists, skipping init")
	} else if err = s.run("go", "mod", "init", projectCfg.ModulePath); err != nil {
		return err
	}

	s.sayf("Creating directories...")
	if err = scaffold.CreateDirectories(env.Dir, projectCfg.Binaries); err != nil {
		return err
	}

	s.sayf("Writing main.go files...")
	if err = scaffold.WriteMainFiles(env.Dir, env.Out, projectCfg); err != nil {
		return err
	}

	s.sayf("Running go mod tidy to fetch dependencies...")
	if err = s.run("go", "mod", "tidy"); err != nil {
		s.sayf("Warning: go mod tidy failed: %v", err)
	}

	s.sayf("Writing tooling files...")
	if err = scaffold.WriteTooling(env.Dir, env.Out, projectCfg); err != nil {
		return err
	}

	s.sayf("Done!")
	return nil
}

func askBinaries(p *prompt.Prompter, binariesInput string) []scaffold.Binary {
	opts := []string{
		"HTTP Server (mjolnir + zerolog)",
		"Background Worker (zerolog)",
		"CLI Tool (standard library)",
		"Interactive CLI",
	}

	var binaries []scaffold.Binary
	for name := range strings.SplitSeq(binariesInput, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		var binType string
		switch p.Select(fmt.Sprintf("Select type for binary '%s':", name), opts) {
		case choiceHTTP:
			binType = "http"
		case choiceWorker:
			binType = "worker"
		case choiceCLI:
			binType = "cli"
		case choiceInteractive:
			binType = "interactive"
		}

		binaries = append(binaries, scaffold.Binary{Name: name, Type: binType})
	}
	return binaries
}
