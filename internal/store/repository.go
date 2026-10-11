package store

import (
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"
)

const maxStoreBytes = 4 << 20

var repositoryPathLocks sync.Map

// Option customizes a Repository. Production callers normally need no options.
type Option func(*Repository) error

// WithClock injects the UTC timestamp source used for controlled metadata.
func WithClock(clock func() time.Time) Option {
	return func(repository *Repository) error {
		if clock == nil {
			return ErrInvalidData
		}
		repository.clock = clock
		return nil
	}
}

// WithEntropy injects the source used to generate stable device IDs.
func WithEntropy(source io.Reader) Option {
	return func(repository *Repository) error {
		if source == nil {
			return ErrInvalidData
		}
		repository.entropy = source
		return nil
	}
}

// Repository owns a versioned JSON metadata document. A path-scoped mutex
// serializes updates across Repository handles in this process; callers always
// receive copies.
type Repository struct {
	mu       sync.RWMutex
	pathLock *sync.Mutex
	path     string
	state    repositoryState
	revision uint64
	clock    func() time.Time
	entropy  io.Reader
	writer   documentWriter
}

// DefaultPath resolves the metadata file below os.UserConfigDir()/ovm.
func DefaultPath() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", newError(CodePersistence, "the user configuration directory is unavailable")
	}
	return filepath.Join(root, "ovm", DefaultFileName), nil
}

// NewDefaultRepository opens the default metadata repository.
func NewDefaultRepository(options ...Option) (*Repository, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return NewRepository(path, options...)
}

// NewRepository opens path. A missing file is a fresh empty store; malformed,
// unsupported, future, or invalid documents are rejected and never replaced.
func NewRepository(path string, options ...Option) (*Repository, error) {
	if path == "" || strings.IndexByte(path, 0) >= 0 {
		return nil, newError(CodeInvalidData, "the store path is invalid")
	}
	cleanPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, newError(CodeInvalidData, "the store path is invalid")
	}
	repository := &Repository{
		path:     cleanPath,
		pathLock: repositoryLockForPath(cleanPath),
		state:    defaultState(),
		clock:    time.Now,
		entropy:  rand.Reader,
		writer:   atomicJSONWriter{},
	}
	for _, option := range options {
		if option == nil {
			return nil, newError(CodeInvalidData, "a repository option is invalid")
		}
		if err := option(repository); err != nil {
			return nil, err
		}
	}
	if err := repository.Load(); err != nil {
		return nil, err
	}
	return repository, nil
}

// Open is an alias for NewRepository.
func Open(path string, options ...Option) (*Repository, error) {
	return NewRepository(path, options...)
}

// Path returns the configured metadata path.
func (repository *Repository) Path() string {
	if repository == nil {
		return ""
	}
	return repository.path
}

// Load atomically replaces in-memory state only after the complete file has
// passed version, shape, and invariant validation.
func (repository *Repository) Load() error {
	if repository == nil {
		return ErrInvalidData
	}
	repository.pathLock.Lock()
	defer repository.pathLock.Unlock()
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.refreshLocked()
}

// Snapshot returns a deep copy with devices in deterministic ID order.
func (repository *Repository) Snapshot() Snapshot {
	if repository == nil {
		return Snapshot{SchemaVersion: SchemaVersion, Devices: []Device{}, Preferences: DefaultPreferences()}
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	devices := make([]Device, len(repository.state.devices))
	copy(devices, repository.state.devices)
	return Snapshot{SchemaVersion: SchemaVersion, Devices: devices, Preferences: repository.state.preferences}
}

// Device returns a copy of the selected record.
func (repository *Repository) Device(id string) (Device, error) {
	if repository == nil || !validIdentifier(id) {
		return Device{}, ErrDeviceNotFound
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	for _, device := range repository.state.devices {
		if device.ID == id {
			return device, nil
		}
	}
	return Device{}, ErrDeviceNotFound
}

// Preferences returns a copy of current durable preferences.
func (repository *Repository) Preferences() Preferences {
	if repository == nil {
		return DefaultPreferences()
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	return repository.state.preferences
}

// Upsert validates and canonicalizes metadata, generates a stable ID when
// needed, controls timestamps, and atomically persists the result.
func (repository *Repository) Upsert(device Device) (Device, error) {
	saved, _, err := repository.upsertTransactional(device)
	return saved, err
}

func (repository *Repository) upsertTransactional(device Device) (Device, *rollbackToken, error) {
	if repository == nil {
		return Device{}, nil, ErrInvalidData
	}
	repository.pathLock.Lock()
	defer repository.pathLock.Unlock()
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if err := repository.refreshLocked(); err != nil {
		return Device{}, nil, err
	}

	prepared, err := canonicalizeDeviceForUpsert(device)
	if err != nil {
		return Device{}, nil, err
	}
	before := cloneState(repository.state)
	next := cloneState(repository.state)
	if prepared.ID == "" {
		prepared.ID, err = repository.generateUniqueDeviceIDLocked()
		if err != nil {
			return Device{}, nil, ErrPersistence
		}
	}

	now := repository.clock().UTC().Round(0)
	found := -1
	for index := range next.devices {
		if next.devices[index].ID == prepared.ID {
			found = index
			break
		}
	}
	if found >= 0 {
		prepared.CreatedAt = next.devices[found].CreatedAt
		if now.Before(next.devices[found].UpdatedAt) {
			prepared.UpdatedAt = next.devices[found].UpdatedAt
		} else {
			prepared.UpdatedAt = now
		}
		next.devices[found] = prepared
	} else {
		prepared.CreatedAt = now
		prepared.UpdatedAt = now
		next.devices = append(next.devices, prepared)
	}
	normalizeState(&next)
	token, commitErr := repository.commitLocked(before, next)
	return prepared, token, commitErr
}

// Remove atomically removes metadata and clears a matching selection. Use
// Coordinator.Forget when a credential reference must also be deleted.
func (repository *Repository) Remove(id string) (Device, error) {
	removed, _, err := repository.removeTransactional(id)
	return removed, err
}

func (repository *Repository) removeTransactional(id string) (Device, *rollbackToken, error) {
	if repository == nil || !validIdentifier(id) {
		return Device{}, nil, ErrDeviceNotFound
	}
	repository.pathLock.Lock()
	defer repository.pathLock.Unlock()
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if err := repository.refreshLocked(); err != nil {
		return Device{}, nil, err
	}

	before := cloneState(repository.state)
	next := cloneState(repository.state)
	index := -1
	var removed Device
	for current := range next.devices {
		if next.devices[current].ID == id {
			index = current
			removed = next.devices[current]
			break
		}
	}
	if index < 0 {
		return Device{}, nil, ErrDeviceNotFound
	}
	next.devices = append(next.devices[:index], next.devices[index+1:]...)
	if next.preferences.SelectedDeviceID == id {
		next.preferences.SelectedDeviceID = ""
	}
	token, err := repository.commitLocked(before, next)
	return removed, token, err
}

// Select updates the selected device. An empty ID clears the selection.
func (repository *Repository) Select(id string) error {
	if repository == nil {
		return ErrInvalidData
	}
	repository.pathLock.Lock()
	defer repository.pathLock.Unlock()
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if err := repository.refreshLocked(); err != nil {
		return err
	}
	if id != "" && !validIdentifier(id) {
		return ErrDeviceNotFound
	}
	if id != "" && !hasDevice(repository.state.devices, id) {
		return ErrDeviceNotFound
	}
	before := cloneState(repository.state)
	next := cloneState(repository.state)
	next.preferences.SelectedDeviceID = id
	_, err := repository.commitLocked(before, next)
	return err
}

// SetPreferences atomically replaces preferences after validating all enums and references.
func (repository *Repository) SetPreferences(preferences Preferences) error {
	if repository == nil {
		return ErrInvalidData
	}
	repository.pathLock.Lock()
	defer repository.pathLock.Unlock()
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if err := repository.refreshLocked(); err != nil {
		return err
	}
	before := cloneState(repository.state)
	next := cloneState(repository.state)
	next.preferences = preferences
	_, err := repository.commitLocked(before, next)
	return err
}

func (repository *Repository) generateUniqueDeviceIDLocked() (string, error) {
	for range 32 {
		identifier, err := generateOpaqueID(repository.entropy, "dev_", 16)
		if err != nil {
			return "", err
		}
		if !hasDevice(repository.state.devices, identifier) {
			return identifier, nil
		}
	}
	return "", errors.New("device ID generation exhausted")
}

func (repository *Repository) commitLocked(before, next repositoryState) (*rollbackToken, error) {
	normalizeState(&next)
	if err := validateState(next); err != nil {
		return nil, err
	}
	committed, err := repository.writer.write(repository.path, stateToDisk(next))
	var token *rollbackToken
	if committed {
		repository.state = cloneState(next)
		repository.revision++
		token = &rollbackToken{
			repository:       repository,
			before:           cloneState(before),
			after:            cloneState(next),
			expectedRevision: repository.revision,
		}
	}
	if err != nil {
		return token, ErrPersistence
	}
	if !committed {
		return nil, ErrPersistence
	}
	return token, nil
}

type rollbackToken struct {
	repository       *Repository
	before           repositoryState
	after            repositoryState
	expectedRevision uint64
}

func (token *rollbackToken) rollback() error {
	if token == nil || token.repository == nil {
		return ErrRollbackFailed
	}
	repository := token.repository
	repository.pathLock.Lock()
	defer repository.pathLock.Unlock()
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.revision != token.expectedRevision {
		return ErrConflict
	}
	persisted, err := loadRepositoryState(repository.path)
	if err != nil {
		return ErrRollbackFailed
	}
	if !reflect.DeepEqual(persisted, token.after) {
		return ErrConflict
	}
	committed, err := repository.writer.write(repository.path, stateToDisk(token.before))
	if committed {
		repository.state = cloneState(token.before)
		repository.revision++
	}
	if err != nil || !committed {
		return ErrRollbackFailed
	}
	return nil
}

func (token *rollbackToken) previousDevice(id string) (Device, bool) {
	if token == nil {
		return Device{}, false
	}
	for _, device := range token.before.devices {
		if device.ID == id {
			return device, true
		}
	}
	return Device{}, false
}

func hasDevice(devices []Device, id string) bool {
	for _, device := range devices {
		if device.ID == id {
			return true
		}
	}
	return false
}

func repositoryLockForPath(path string) *sync.Mutex {
	lock, _ := repositoryPathLocks.LoadOrStore(path, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func (repository *Repository) refreshLocked() error {
	loaded, err := loadRepositoryState(repository.path)
	if err != nil {
		return err
	}
	repository.state = cloneState(loaded)
	repository.revision++
	return nil
}

func loadRepositoryState(path string) (repositoryState, error) {
	data, err := readStoreFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaultState(), nil
	}
	if err != nil {
		var storeErr *Error
		if errors.As(err, &storeErr) {
			return repositoryState{}, storeErr
		}
		return repositoryState{}, ErrPersistence
	}
	decoded, err := decodeDocument(data)
	if err != nil {
		return repositoryState{}, err
	}
	return decoded, nil
}

func readStoreFile(path string) ([]byte, error) {
	// The configured path is the repository API's explicit input; decoded content is strictly bounded and validated.
	file, err := os.Open(path) //nolint:gosec // caller-selected metadata path is the intended API
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxStoreBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxStoreBytes {
		return nil, ErrMalformedStore
	}
	return data, nil
}
