//go:build e2e

package cli_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// mjolnirVersion is the mjolnir release the Templates target. Tests that
// build Deployables fetch it, rather than latest, so they don't drift.
const mjolnirVersion = "github.com/dfryer1193/mjolnir@v1.8.2"

// fetch runs a real go get in the Project, standing in for the one the fake
// runner only records.
func (p *project) fetch(query string) {
	p.t.Helper()
	cmd := exec.CommandContext(p.t.Context(), "go", "get", query)
	cmd.Dir = p.dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	if out, err := cmd.CombinedOutput(); err != nil {
		p.t.Fatalf("go get %s failed (it needs the module cache or the network): %v\n%s", query, err, out)
	}
}

// replaceRun swaps the body of the job's run function, standing in for the
// user implementing it.
func (p *project) replaceRun(name, body string) {
	p.t.Helper()
	const signature = "func run(ctx context.Context) error {"
	main := p.read("cmd/" + name + "/main.go")
	before, _, found := strings.Cut(main, signature)
	if !found {
		p.t.Fatalf("expected %q in cmd/%s/main.go:\n%s", signature, name, main)
	}
	p.write("cmd/"+name+"/main.go", before+signature+"\n"+body+"\n}\n")
}

// startJob starts the built job in a scratch directory (mjolnir writes its
// JSON log under the working directory) with its stdout in out.
func startJob(t *testing.T, bin string, out *syncBuffer) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), bin)
	cmd.Dir = t.TempDir()
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return cmd
}

func exitCode(t *testing.T, err error) int {
	t.Helper()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exitErr) && exitErr.Exited():
		return exitErr.ExitCode()
	default:
		t.Fatalf("expected the job to exit by itself, got %v", err)
		return -1
	}
}

func TestAddedJobBuildsAndExitsWithItsOutcome(t *testing.T) {
	p := newProject(t)
	p.initProject()
	if err := p.run("nightly\njob\n\n", "add"); err != nil {
		t.Fatalf("add failed: %v\noutput:\n%s", err, p.out)
	}
	if !p.runner.requestedLine(p.dir, "go get github.com/dfryer1193/mjolnir@latest") ||
		!p.runner.requestedLine(p.dir, "go mod tidy") {
		t.Fatalf("expected mjolnir fetched at latest and the module tidied, requested %+v", p.runner.requested)
	}
	main := p.read("cmd/nightly/main.go")
	if !strings.Contains(main, "TODO") || !strings.Contains(main, "github.com/acme/widget/service") {
		t.Errorf("expected a TODO pointing at service:\n%s", main)
	}
	p.fetch(mjolnirVersion)

	t.Run("success exits 0", func(t *testing.T) {
		var out syncBuffer
		cmd := startJob(t, p.goBuild("nightly"), &out)
		if code := exitCode(t, cmd.Wait()); code != 0 {
			t.Errorf("expected exit 0, got %d:\n%s", code, out.String())
		}
		if !strings.Contains(out.String(), "logger=nightly") {
			t.Errorf("expected mjolnir's logger to name the job, got:\n%s", out.String())
		}
	})

	t.Run("failure exits 1", func(t *testing.T) {
		p.replaceRun("nightly", "\treturn context.DeadlineExceeded")
		var out syncBuffer
		cmd := startJob(t, p.goBuild("nightly"), &out)
		if code := exitCode(t, cmd.Wait()); code != 1 {
			t.Errorf("expected exit 1, got %d:\n%s", code, out.String())
		}
	})

	t.Run("SIGTERM stops it", func(t *testing.T) {
		p.replaceRun("nightly", "\t<-ctx.Done()\n\treturn ctx.Err()")
		var out syncBuffer
		cmd := startJob(t, p.goBuild("nightly"), &out)
		waitFor(t, &out, "job starting")
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			// Stopped before finishing, so the job failed, but it exited by
			// itself rather than being killed by the signal.
			if code := exitCode(t, err); code != 1 {
				t.Errorf("expected exit 1 after SIGTERM, got %d:\n%s", code, out.String())
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("job still running 10s after SIGTERM:\n%s", out.String())
		}
	})
}

// waitFor waits until out contains s.
func waitFor(t *testing.T, out *syncBuffer, s string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), s) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q, got:\n%s", s, out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// syncBuffer is a [bytes.Buffer] safe to read while a process writes to it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
