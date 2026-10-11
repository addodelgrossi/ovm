//go:build (!darwin && !linux) || android || ios

package store

import (
	"errors"
	"testing"
)

func TestPlatformSecretProviderIsExplicitlyUnsupported(t *testing.T) {
	t.Parallel()
	provider, err := NewPlatformSecretProvider()
	if provider != nil || !errors.Is(err, ErrSecretUnsupported) {
		t.Fatalf("NewPlatformSecretProvider() = (%T, %v), want (nil, ErrSecretUnsupported)", provider, err)
	}
}
