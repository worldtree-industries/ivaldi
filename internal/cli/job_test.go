package cli_test

import (
	"maps"
	"strings"
	"testing"

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
