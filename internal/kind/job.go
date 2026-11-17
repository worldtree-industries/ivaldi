package kind

import (
	"fmt"
	"slices"
	"strings"

	"github.com/robfig/cron/v3"

	"github.com/worldtree-industries/ivaldi/internal/manifest"
)

// defaultSchedule is the schedule a job gets when the user just presses
// enter: run once.
const defaultSchedule = "none"

// scheduleKeywords are the schedules stored as keywords. Brokkr turns the
// periodic ones into cron with stable jitter; Ivaldi never does.
//
//nolint:gochecknoglobals // a constant list
var scheduleKeywords = []string{"hourly", "daily", "weekly", "monthly", defaultSchedule}

// cronFields is how many fields a cron schedule has: minute, hour, day of
// month, month and day of week.
const cronFields = 5

// Job returns the job Kind: a Deployable that runs once to completion, either
// once per rollout or on a schedule.
func Job() Kind {
	return Kind{
		Name:    "job",
		Ask:     askJob,
		Modules: []string{mjolnirModule},
	}
}

func askJob(q *Questions, name string) (Answers, error) {
	message := fmt.Sprintf("Schedule of %s (%s, or a 5-field cron expression)",
		name, strings.Join(scheduleKeywords, ", "))
	schedule, err := q.Prompt.Validated(message, defaultSchedule, validateSchedule)
	if err != nil {
		return Answers{}, err
	}
	return Answers{Settings: []manifest.Setting{{Key: manifest.SettingSchedule, Value: schedule}}}, nil
}

// validateSchedule accepts a keyword or a standard 5-field cron expression.
// Requiring exactly 5 fields rules out cron descriptors (@daily, @every) and
// time zone prefixes, which the Manifest doesn't allow.
func validateSchedule(schedule string) error {
	if slices.Contains(scheduleKeywords, schedule) {
		return nil
	}
	if n := len(strings.Fields(schedule)); n != cronFields {
		return fmt.Errorf("not a keyword, and a cron expression needs %d fields, not %d", cronFields, n)
	}
	_, err := cron.ParseStandard(schedule)
	return err
}
