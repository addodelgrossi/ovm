package auth

import (
	"errors"
	"strings"
	"testing"
)

// TestPolicyErrorMessagesAreStableAndNonSecret verifies every reason yields a
// fixed, credential-free message.
func TestPolicyErrorMessagesAreStableAndNonSecret(t *testing.T) {
	t.Parallel()

	reasons := []PolicyReason{
		ReasonBasicDisabled,
		ReasonBasicOverPlaintext,
		ReasonRFC2069Disabled,
		ReasonIncoherentBasicPolicy,
	}
	seen := map[string]struct{}{}
	for _, reason := range reasons {
		message := (&PolicyError{Reason: reason}).Error()
		if message == "" {
			t.Fatalf("reason %q produced an empty message", reason)
		}
		if _, dup := seen[message]; dup {
			t.Fatalf("reason %q produced a non-unique message %q", reason, message)
		}
		seen[message] = struct{}{}
		for _, secretish := range []string{"password", "nonce", "realm=", "cnonce"} {
			if strings.Contains(strings.ToLower(message), secretish) {
				t.Fatalf("policy message for %q leaked token %q: %s", reason, secretish, message)
			}
		}
	}
}

// TestPolicyErrorMatchingSemantics verifies errors.Is and errors.As behave as
// documented: the sentinel matches any PolicyError, a zero-reason target
// matches any PolicyError, and a specific-reason target matches only its own.
func TestPolicyErrorMatchingSemantics(t *testing.T) {
	t.Parallel()

	err := newPolicyError(ReasonRFC2069Disabled)

	if !errors.Is(err, ErrAuthPolicy) {
		t.Fatal("PolicyError should match ErrAuthPolicy sentinel")
	}
	if !errors.Is(err, &PolicyError{}) {
		t.Fatal("zero-reason target should match any PolicyError")
	}
	if !errors.Is(err, &PolicyError{Reason: ReasonRFC2069Disabled}) {
		t.Fatal("matching-reason target should match")
	}
	if errors.Is(err, &PolicyError{Reason: ReasonBasicDisabled}) {
		t.Fatal("mismatched-reason target should not match")
	}

	var target *PolicyError
	if !errors.As(err, &target) || target.Reason != ReasonRFC2069Disabled {
		t.Fatalf("errors.As failed: %v", target)
	}
}
