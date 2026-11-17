package cli_test

import (
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// freePort returns a port nothing is listening on right now.
func freePort(t *testing.T) int {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err = ln.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

// status GETs url and returns the status code, or 0 if the request failed.
func status(t *testing.T, url string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func TestAddedHTTPServesProbesAndDrainsOnSIGTERM(t *testing.T) {
	p := newProject(t)
	p.initProject()
	if err := p.run("web\nhttp\n\n\n", "add"); err != nil {
		t.Fatalf("add failed: %v\noutput:\n%s", err, p.out)
	}
	p.fetch(mjolnirVersion)

	port := freePort(t)
	var out syncBuffer
	cmd := exec.CommandContext(t.Context(), p.goBuild("web"))
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), "PORT="+strconv.Itoa(port))
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	base := "http://127.0.0.1:" + strconv.Itoa(port)

	deadline := time.Now().Add(10 * time.Second)
	for status(t, base+"/livez") != http.StatusOK {
		if time.Now().After(deadline) {
			t.Fatalf("web never answered /livez on PORT %d:\n%s", port, out.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	for path, want := range map[string]int{
		"/livez":    http.StatusOK,
		"/startupz": http.StatusOK,
		"/readyz":   http.StatusServiceUnavailable, // until the readiness check is implemented
	} {
		if got := status(t, base+path); got != want {
			t.Errorf("GET %s = %d, want %d", path, got, want)
		}
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	waitFor(t, &out, "draining")
	if got := status(t, base+"/livez"); got != http.StatusOK {
		t.Errorf("expected /livez still served while draining, got %d", got)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if code := exitCode(t, err); code != 0 {
			t.Errorf("expected exit 0 after draining, got %d:\n%s", code, out.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("web still running 15s after SIGTERM:\n%s", out.String())
	}
	waitFor(t, &out, "Server stopped")
}
