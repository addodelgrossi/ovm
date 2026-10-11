package auth

import (
	"errors"
	"testing"
)

// TestDeviceStrategyPolicyErrorsAreTyped verifies that DeviceStrategy.Validate
// surfaces typed, non-secret PolicyError values for the policy-level refusals
// and that incoherent Basic policy is rejected independently of mode.
func TestDeviceStrategyPolicyErrorsAreTyped(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name       string
		strategy   DeviceStrategy
		wantReason PolicyReason
	}{
		{
			name:       "basic without policy",
			strategy:   DeviceStrategy{Mode: ModeBasic},
			wantReason: ReasonBasicDisabled,
		},
		{
			name:       "over-http without basic is incoherent",
			strategy:   DeviceStrategy{Mode: ModeDigest, AllowBasicOverHTTP: true},
			wantReason: ReasonIncoherentBasicPolicy,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.strategy.Validate()
			var policyErr *PolicyError
			if !errors.As(err, &policyErr) || policyErr.Reason != tt.wantReason {
				t.Fatalf("Validate() error = %v, want PolicyError(%s)", err, tt.wantReason)
			}
			if !errors.Is(err, ErrAuthPolicy) {
				t.Fatal("policy error must match ErrAuthPolicy sentinel")
			}
		})
	}
}

// TestDeviceStrategyCoherentPolicyValidates verifies coherent opt-ins validate.
func TestDeviceStrategyCoherentPolicyValidates(t *testing.T) {
	t.Parallel()

	coherent := DeviceStrategy{
		Mode:               ModeBasic,
		AllowBasic:         true,
		AllowBasicOverHTTP: true,
		AllowRFC2069:       true,
	}
	if err := coherent.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}
