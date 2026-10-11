package cli

import "testing"

// warnFailingWriter fails after okWrites successful writes so a test can sweep
// the failure point across each warning write.
type warnFailingWriter struct {
	okWrites int
	calls    int
}

func (w *warnFailingWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls > w.okWrites {
		return 0, &warnWriteError{}
	}
	return len(p), nil
}

type warnWriteError struct{}

func (*warnWriteError) Error() string { return "simulated stderr write failure" }

// TestDowngradeWarningsPropagateWriteErrors proves a stderr write failure while
// emitting any enabled warning is propagated, never swallowed, so an operator
// is never silently denied a safety notice.
func TestDowngradeWarningsPropagateWriteErrors(t *testing.T) {
	// Count the writes with both warnings enabled.
	counter := &warnFailingWriter{okWrites: 1 << 30}
	if err := writeDowngradeWarnings(counter, true, true, true, false); err != nil {
		t.Fatalf("counting pass should succeed: %v", err)
	}
	total := counter.calls
	if total < 2 {
		t.Fatalf("expected at least two warning writes, got %d", total)
	}
	for failAt := 1; failAt <= total; failAt++ {
		fw := &warnFailingWriter{okWrites: failAt - 1}
		if err := writeDowngradeWarnings(fw, true, true, true, false); err == nil {
			t.Fatalf("expected the write at position %d to propagate an error", failAt)
		}
	}
}

// TestDowngradeWarningsEmitNothingWhenNoFlags proves no warning (and no write)
// happens when neither downgrade is enabled.
func TestDowngradeWarningsEmitNothingWhenNoFlags(t *testing.T) {
	counter := &warnFailingWriter{okWrites: 0} // any write would fail
	if err := writeDowngradeWarnings(counter, false, false, false, false); err != nil {
		t.Fatalf("no flags should perform no writes, got %v", err)
	}
	if counter.calls != 0 {
		t.Fatalf("expected zero writes, got %d", counter.calls)
	}
}
