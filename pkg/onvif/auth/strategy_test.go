package auth

import (
	"sync"
	"testing"
	"time"
)

func TestStrategyStoreKeepsCredentialFreePerDeviceState(t *testing.T) {
	t.Parallel()

	store := NewStrategyStore()
	strategy := DeviceStrategy{
		Mode:             ModeUsernameToken,
		ClockOffset:      3 * time.Second,
		ClockUncertainty: 550 * time.Millisecond,
		UpdatedAt:        time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
	}
	if err := store.Set("device-a", strategy); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	got, ok := store.Get("device-a")
	if !ok || got != strategy {
		t.Fatalf("Get() = (%+v, %v), want (%+v, true)", got, ok, strategy)
	}
	if _, ok := store.Get("device-b"); ok {
		t.Fatal("strategy for device-a leaked into device-b")
	}
	store.Delete("device-a")
	if _, ok := store.Get("device-a"); ok {
		t.Fatal("Delete() did not remove strategy")
	}
}

func TestDeviceStrategyRequiresExplicitBasicPolicy(t *testing.T) {
	t.Parallel()

	if err := (DeviceStrategy{Mode: ModeBasic}).Validate(); err == nil {
		t.Fatal("Validate() error = nil for Basic without explicit policy")
	}
	if err := (DeviceStrategy{Mode: ModeBasic, AllowBasic: true}).Validate(); err != nil {
		t.Fatalf("Validate() error = %v for explicitly enabled Basic", err)
	}
}

func TestStrategyStoreIsConcurrent(t *testing.T) {
	t.Parallel()

	store := NewStrategyStore()
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 100; j++ {
				_ = store.Set("device", DeviceStrategy{Mode: ModeDigest})
				_, _ = store.Get("device")
			}
		}()
	}
	workers.Wait()
}
