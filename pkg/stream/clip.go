package stream

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Clip recording limits keep an unattended recording from filling the disk.
const (
	DefaultClipMaxBytes = 2 << 30
)

var (
	// ErrClipExists reports that the destination already exists.
	ErrClipExists = errors.New("stream: the clip file already exists")
	// ErrClipEmpty reports a recording that ended before its first keyframe.
	ErrClipEmpty = errors.New("stream: the clip contains no keyframe")
	// ErrClipWrite reports a local file failure.
	ErrClipWrite = errors.New("stream: the clip could not be written")
	// ErrClipInterrupted reports a recording ended by a codec change, a size
	// limit, or the stream ending; whatever was recorded is still published.
	ErrClipInterrupted = errors.New("stream: the clip recording was interrupted")
)

// ClipRecorder is a FragmentSink that records a fragmented MP4 clip starting
// at the first keyframe after it is attached. Bytes go to a 0600 temporary
// file beside the destination; Finish publishes it atomically without
// replacing an existing file. It is safe for concurrent use.
type ClipRecorder struct {
	path     string
	maxBytes int64

	mu          sync.Mutex
	file        *os.File
	init        []byte
	started     bool
	written     int64
	interrupted bool
	failed      error
	done        bool
}

// NewClipRecorder prepares a recording to path. The destination must not
// exist yet; its directory is created with mode 0700 when missing.
func NewClipRecorder(path string, maxBytes int64) (*ClipRecorder, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultClipMaxBytes
	}
	if _, err := os.Lstat(path); err == nil {
		return nil, ErrClipExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, ErrClipWrite
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, ErrClipWrite
	}
	file, err := os.CreateTemp(directory, ".clip-*.mp4.tmp")
	if err != nil {
		return nil, ErrClipWrite
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return nil, ErrClipWrite
	}
	return &ClipRecorder{path: path, maxBytes: maxBytes, file: file}, nil
}

// SetInit records the initialization segment. A second, different
// initialization (parameter sets changed) interrupts the clip because one MP4
// track cannot change its decoder configuration.
func (r *ClipRecorder) SetInit(_ string, init []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done || r.interrupted {
		return
	}
	if r.started {
		r.interrupted = true
		return
	}
	r.init = append([]byte(nil), init...)
}

// WriteFragment appends a fragment once the first keyframe has arrived.
func (r *ClipRecorder) WriteFragment(fragment []byte, key bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done || r.interrupted || r.failed != nil {
		return
	}
	if !r.started {
		if !key || r.init == nil {
			return
		}
		if !r.writeLocked(r.init) {
			return
		}
		r.started = true
	}
	if r.written+int64(len(fragment)) > r.maxBytes {
		r.interrupted = true
		return
	}
	r.writeLocked(fragment)
}

func (r *ClipRecorder) writeLocked(data []byte) bool {
	if _, err := r.file.Write(data); err != nil {
		r.failed = ErrClipWrite
		return false
	}
	r.written += int64(len(data))
	return true
}

// Bytes reports how much has been written.
func (r *ClipRecorder) Bytes() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.written
}

// Finish ends the recording and publishes the clip. It returns the published
// size; ErrClipInterrupted accompanies a published but truncated clip. It is
// idempotent after the first call.
func (r *ClipRecorder) Finish() (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return 0, ErrClipWrite
	}
	r.done = true
	name := r.file.Name()
	discard := func(err error) (int64, error) {
		_ = r.file.Close()
		_ = os.Remove(name)
		return 0, err
	}
	if r.failed != nil {
		return discard(r.failed)
	}
	if !r.started {
		return discard(ErrClipEmpty)
	}
	if err := r.file.Sync(); err != nil {
		return discard(ErrClipWrite)
	}
	if err := r.file.Close(); err != nil {
		_ = os.Remove(name)
		return 0, ErrClipWrite
	}
	// Link is the atomic no-clobber publication; the temporary name is then
	// removed.
	if err := os.Link(name, r.path); err != nil {
		_ = os.Remove(name)
		if errors.Is(err, os.ErrExist) {
			return 0, ErrClipExists
		}
		return 0, ErrClipWrite
	}
	_ = os.Remove(name)
	if r.interrupted {
		return r.written, ErrClipInterrupted
	}
	return r.written, nil
}
