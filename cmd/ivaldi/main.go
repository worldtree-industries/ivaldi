// Command ivaldi sets up and grows opinionated Go Projects.
//
// This is the only place that touches the process's working directory,
// standard streams and exec; everything else goes through cli.Env.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/worldtree-industries/ivaldi/internal/cli"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	err = cli.Run(ctx, cli.Env{
		Dir:    dir,
		In:     os.Stdin,
		Out:    os.Stdout,
		Runner: execRunner{},
	}, os.Args[1:])
	if err != nil {
		if !errors.Is(err, cli.ErrUsage) {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		return 1
	}
	return 0
}

// execRunner runs commands as real processes. On failure, stderr is included
// in the returned error.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, c cli.Command) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...) //nolint:gosec // Commands come from Ivaldi itself.
	cmd.Dir = c.Dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return stdout.Bytes(), fmt.Errorf("%w: %s", err, msg)
		}
		return stdout.Bytes(), err
	}
	return stdout.Bytes(), nil
}
