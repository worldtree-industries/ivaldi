package cli_test

import (
	"bytes"
	"errors"
	"maps"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/worldtree-industries/ivaldi/internal/manifest"
)

// manifestEntry returns the named binary's Manifest entry, as written.
func (p *project) manifestEntry(name string) manifest.Binary {
	p.t.Helper()
	m, err := manifest.Load(p.dir)
	if err != nil {
		p.t.Fatalf("loading the Manifest: %v", err)
	}
	for _, b := range m.Binaries() {
		if b.Name == name {
			return b
		}
	}
	p.t.Fatalf("expected %s in the Manifest, got:\n%s", name, p.read(".forge.yaml"))
	return manifest.Binary{}
}

func TestAddJobDefaultsScheduleToNone(t *testing.T) {
	p := newProject(t)
	p.initProject()

	if err := p.run("nightly\njob\n\n", "add"); err != nil {
		t.Fatalf("add failed: %v\noutput:\n%s", err, p.out)
	}

	entry := p.manifestEntry("nightly")
	if entry.Kind != "job" || entry.Settings["schedule"] != "none" {
		t.Errorf("expected kind job with schedule none, got %+v", entry)
	}
	p.read("cmd/nightly/main.go")
}

func TestAddJobStoresKeywordsAndCronAsEntered(t *testing.T) {
	for _, schedule := range []string{
		"hourly", "daily", "weekly", "monthly", "none",
		"*/15 * * * *", "0 3 * * 1-5", "30 2 1,15 * *", "0 0 * JAN,JUL SUN",
	} {
		t.Run(schedule, func(t *testing.T) {
			p := newProject(t)
			p.initProject()

			if err := p.run("nightly\njob\n"+schedule+"\n", "add"); err != nil {
				t.Fatalf("add failed: %v\noutput:\n%s", err, p.out)
			}

			if got := p.manifestEntry("nightly").Settings["schedule"]; got != schedule {
				t.Errorf("expected schedule %q stored, got %q", schedule, got)
			}
			if strings.Count(p.out.String(), "Schedule of nightly") != 1 {
				t.Errorf("expected the schedule asked once, got:\n%s", p.out)
			}
		})
	}
}

func TestAddJobReasksAnInvalidSchedule(t *testing.T) {
	for _, bad := range []string{
		"Daily", "yearly", "@daily", "@every 1h",
		"* * * *", "* * * * * *", "0 0 0 * * *",
		"60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 8",
		"*/0 * * * *", "5-1 * * * *", "a b c d e", "CRON_TZ=UTC 0 3 * * *", "TZ=UTC 0 3 * *",
	} {
		t.Run(bad, func(t *testing.T) {
			p := newProject(t)
			p.initProject()

			if err := p.run("nightly\njob\n"+bad+"\ndaily\n", "add"); err != nil {
				t.Fatalf("add failed: %v\noutput:\n%s", err, p.out)
			}

			if got := strings.Count(p.out.String(), "Schedule of nightly"); got != 2 {
				t.Errorf("expected %q re-asked, schedule asked %d times:\n%s", bad, got, p.out)
			}
			if got := p.manifestEntry("nightly").Settings["schedule"]; got != "daily" {
				t.Errorf("expected the re-asked answer stored, got %q", got)
			}
		})
	}
}

func TestAddJobFailsWhenInputEndsOnAnInvalidScheduleWithNothingWritten(t *testing.T) {
	p := newProject(t)
	p.initProject()
	before := snapshot(t, p.dir)

	if err := p.run("nightly\njob\nsometimes\n", "add"); err == nil {
		t.Fatalf("expected add to fail")
	}
	if !maps.Equal(before, snapshot(t, p.dir)) {
		t.Errorf("expected nothing written")
	}
}

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
