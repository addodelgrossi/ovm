package buildinfo

import "testing"

func TestCurrent(t *testing.T) {
	oldVersion, oldCommit, oldDate := Version, Commit, Date
	t.Cleanup(func() {
		Version, Commit, Date = oldVersion, oldCommit, oldDate
	})

	Version, Commit, Date = "v0.1.0", "abc123", "2026-10-08T00:00:00Z"

	got := Current()
	if got.Version != Version || got.Commit != Commit || got.Date != Date {
		t.Fatalf("Current() = %#v, want configured build values", got)
	}
}
