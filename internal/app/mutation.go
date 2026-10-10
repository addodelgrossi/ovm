package app

import (
	"context"
	"time"
)

// MutationOutcome classifies what the device actually did after a Set,
// determined by reading the configuration back.
type MutationOutcome string

const (
	// OutcomeExact means every requested value was read back unchanged.
	OutcomeExact MutationOutcome = "exact"
	// OutcomeAdjusted means the device accepted the change but stored at
	// least one different value (for example a clamped bitrate).
	OutcomeAdjusted MutationOutcome = "adjusted"
	// OutcomeNotApplied means the device acknowledged the Set but the read
	// back still shows the previous values.
	OutcomeNotApplied MutationOutcome = "not_applied"
	// OutcomeDisconnected means the Set was sent but the device could not be
	// read back, as after a network change or reboot.
	OutcomeDisconnected MutationOutcome = "disconnected"
)

// FieldChange compares one field before the Set, as requested, and as read
// back. Values are presentation strings.
type FieldChange struct {
	Field     string `json:"field"`
	Before    string `json:"before"`
	Requested string `json:"requested"`
	After     string `json:"after"`
}

// MutationReport is the verified result of a configuration change.
type MutationReport struct {
	Outcome MutationOutcome `json:"outcome"`
	Changes []FieldChange   `json:"changes"`
}

// readbackPolicy controls how long a use case waits for a change to appear.
// Some devices apply settings asynchronously, so the read back is retried.
type readbackPolicy struct {
	attempts int
	delay    time.Duration
}

var defaultReadback = readbackPolicy{attempts: 4, delay: 750 * time.Millisecond}

// fieldValues is an ordered presentation of a configuration.
type fieldValues struct {
	names  []string
	values map[string]string
}

func newFieldValues() fieldValues { return fieldValues{values: map[string]string{}} }

func (f *fieldValues) set(name, value string) {
	if _, exists := f.values[name]; !exists {
		f.names = append(f.names, name)
	}
	f.values[name] = value
}

// matches reports whether every field in want has the same value here.
func (f fieldValues) matches(want fieldValues) bool {
	for _, name := range want.names {
		if f.values[name] != want.values[name] {
			return false
		}
	}
	return true
}

// compareMutation builds the report. Only fields whose requested value differs
// from the previous value, or whose read back differs from the request, are
// listed.
func compareMutation(before, requested, after fieldValues) MutationReport {
	report := MutationReport{Outcome: OutcomeExact, Changes: []FieldChange{}}
	changed, applied, reverted := 0, 0, 0
	for _, name := range requested.names {
		change := FieldChange{
			Field:     name,
			Before:    before.values[name],
			Requested: requested.values[name],
			After:     after.values[name],
		}
		if change.Requested == change.Before && change.After == change.Requested {
			continue
		}
		report.Changes = append(report.Changes, change)
		if change.Requested != change.Before {
			changed++
		}
		switch change.After {
		case change.Requested:
			applied++
		case change.Before:
			reverted++
		}
	}
	switch {
	case len(report.Changes) == 0 || applied == len(report.Changes):
		report.Outcome = OutcomeExact
	case changed > 0 && reverted == len(report.Changes):
		report.Outcome = OutcomeNotApplied
	default:
		report.Outcome = OutcomeAdjusted
	}
	return report
}

// readBack polls read until it returns values matching requested or the
// attempts are exhausted, returning the last successful read. ok is false
// when no read succeeded.
func readBack(ctx context.Context, policy readbackPolicy, requested fieldValues, read func(context.Context) (fieldValues, error)) (fieldValues, bool) {
	var last fieldValues
	ok := false
	for attempt := 0; attempt < policy.attempts; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(policy.delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return last, ok
			case <-timer.C:
			}
		}
		current, err := read(ctx)
		if err != nil {
			continue
		}
		last, ok = current, true
		if current.matches(requested) {
			return current, true
		}
	}
	return last, ok
}
