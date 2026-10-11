package auth

import (
	"errors"
	"sync"
	"time"
)

// Mode identifies the negotiated authentication strategy for one device.
type Mode string

const (
	// ModeUnknown means authentication has not been negotiated.
	ModeUnknown Mode = "unknown"
	// ModeUnauthenticated means the operation needs no credentials.
	ModeUnauthenticated Mode = "unauthenticated"
	// ModeUsernameToken means WS-Security PasswordDigest is selected.
	ModeUsernameToken Mode = "username_token"
	// ModeDigest means HTTP Digest is selected.
	ModeDigest Mode = "http_digest"
	// ModeBasic means explicitly permitted HTTP Basic is selected.
	ModeBasic Mode = "http_basic"
)

// DeviceStrategy contains credential-free authentication state suitable for a
// per-device cache. It is not a secret-storage model.
//
// The policy hooks are all zero-value-off: a freshly constructed strategy
// permits nothing beyond the secure defaults. They mirror DigestTransportConfig
// so a negotiated strategy can be persisted and later rebuilt into a transport
// with identical policy.
type DeviceStrategy struct {
	Mode Mode
	// AllowBasic permits HTTP Basic for this device. Over plaintext HTTP it is
	// still refused unless AllowBasicOverHTTP is also set.
	AllowBasic bool
	// AllowBasicOverHTTP additionally permits HTTP Basic over plaintext HTTP.
	// It is meaningless without AllowBasic.
	AllowBasicOverHTTP bool
	// AllowRFC2069 permits legacy no-qop (RFC 2069) Digest for this device.
	AllowRFC2069     bool
	ClockOffset      time.Duration
	ClockUncertainty time.Duration
	UpdatedAt        time.Time
}

// Validate checks policy and state invariants. Incoherent policy combinations
// return a typed PolicyError so callers can distinguish policy refusals from
// other validation errors.
func (s DeviceStrategy) Validate() error {
	switch s.Mode {
	case ModeUnknown, ModeUnauthenticated, ModeUsernameToken, ModeDigest:
	case ModeBasic:
		if !s.AllowBasic {
			return newPolicyError(ReasonBasicDisabled)
		}
	default:
		return errors.New("unknown authentication strategy mode")
	}
	// AllowBasicOverHTTP without AllowBasic can never permit any Basic exchange.
	if s.AllowBasicOverHTTP && !s.AllowBasic {
		return newPolicyError(ReasonIncoherentBasicPolicy)
	}
	if s.ClockUncertainty < 0 {
		return errors.New("camera clock uncertainty cannot be negative")
	}
	return nil
}

// StrategyStore is a concurrency-safe, in-memory per-device strategy cache.
type StrategyStore struct {
	mu         sync.RWMutex
	strategies map[string]DeviceStrategy
}

// NewStrategyStore constructs an empty strategy cache.
func NewStrategyStore() *StrategyStore {
	return &StrategyStore{strategies: make(map[string]DeviceStrategy)}
}

// Set records credential-free state for a stable device key.
func (s *StrategyStore) Set(deviceKey string, strategy DeviceStrategy) error {
	if deviceKey == "" {
		return errors.New("device strategy key is required")
	}
	if err := strategy.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.strategies == nil {
		s.strategies = make(map[string]DeviceStrategy)
	}
	s.strategies[deviceKey] = strategy
	return nil
}

// Get retrieves a device strategy.
func (s *StrategyStore) Get(deviceKey string) (DeviceStrategy, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	strategy, ok := s.strategies[deviceKey]
	return strategy, ok
}

// Delete removes a device strategy.
func (s *StrategyStore) Delete(deviceKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.strategies, deviceKey)
}
