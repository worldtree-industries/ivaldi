package testtool_test

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/worldtree-industries/ivaldi/internal/testtool"
)

// recorder stands in for a test, recording whether it was skipped or failed.
// Like the real thing, both stop the calling goroutine.
type recorder struct {
	testing.TB

	skipped, failed bool
	msg             string
}

func (r *recorder) Helper() {}

func (r *recorder) Skipf(format string, args ...any) {
	r.skipped, r.msg = true, fmt.Sprintf(format, args...)
	runtime.Goexit()
}

func (r *recorder) Fatalf(format string, args ...any) {
	r.failed, r.msg = true, fmt.Sprintf(format, args...)
	runtime.Goexit()
}

func lookUp(t *testing.T, name string) (*recorder, string) {
	t.Helper()
	r := &recorder{}
	var got string
	done := make(chan struct{})
	go func() {
		defer close(done)
		got = testtool.Path(r, name)
	}()
	<-done
	return r, got
}

const missing = "ivaldi-no-such-tool"

func TestMissingToolSkipsByDefault(t *testing.T) {
	t.Setenv(testtool.RequireEnv, "")
	r, _ := lookUp(t, missing)
	if !r.skipped || r.failed {
		t.Fatalf("expected a skip, got skipped=%v failed=%v", r.skipped, r.failed)
	}
}

func TestMissingToolFailsWhenLintersAreRequired(t *testing.T) {
	t.Setenv(testtool.RequireEnv, "1")
	r, _ := lookUp(t, missing)
	if !r.failed || r.skipped {
		t.Fatalf("expected a failure, got skipped=%v failed=%v", r.skipped, r.failed)
	}
}

func TestPresentToolIsReturned(t *testing.T) {
	t.Setenv(testtool.RequireEnv, "1")
	r, got := lookUp(t, "go")
	if r.skipped || r.failed || got == "" {
		t.Fatalf("expected the path to go, got %q (skipped=%v failed=%v: %s)", got, r.skipped, r.failed, r.msg)
	}
}
