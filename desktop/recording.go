package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/addodelgrossi/ovm/pkg/stream"
)

var (
	errRecordingActive   = errors.New("a recording is already running")
	errRecordingInactive = errors.New("no recording is running")
	errClipPath          = errors.New("the clip path is not valid")
	errRevealUnsupported = errors.New("revealing files is not supported on this platform")
)

// previewSink feeds the loopback bridge and, while recording, a clip. It keeps
// the current initialization segment so a recording started mid-stream can
// begin with it.
type previewSink struct {
	bridge stream.FragmentSink

	mu       sync.Mutex
	codec    string
	init     []byte
	recorder *stream.ClipRecorder
	path     string
}

func (s *previewSink) SetInit(codec string, init []byte) {
	s.mu.Lock()
	s.codec, s.init = codec, append([]byte(nil), init...)
	recorder := s.recorder
	s.mu.Unlock()
	s.bridge.SetInit(codec, init)
	if recorder != nil {
		recorder.SetInit(codec, init)
	}
}

func (s *previewSink) WriteFragment(fragment []byte, key bool) {
	s.bridge.WriteFragment(fragment, key)
	s.mu.Lock()
	recorder := s.recorder
	s.mu.Unlock()
	if recorder != nil {
		recorder.WriteFragment(fragment, key)
	}
}

func (s *previewSink) startRecording(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.recorder != nil {
		return errRecordingActive
	}
	recorder, err := stream.NewClipRecorder(path, 0)
	if err != nil {
		return err
	}
	if s.init != nil {
		recorder.SetInit(s.codec, s.init)
	}
	s.recorder, s.path = recorder, path
	return nil
}

// finishRecording publishes the clip. interrupted reports a clip that was
// published but cut short by a codec change, the size limit, or the stream
// ending early.
func (s *previewSink) finishRecording() (string, int64, bool, error) {
	s.mu.Lock()
	recorder, path := s.recorder, s.path
	s.recorder, s.path = nil, ""
	s.mu.Unlock()
	if recorder == nil {
		return "", 0, false, errRecordingInactive
	}
	size, err := recorder.Finish()
	if errors.Is(err, stream.ErrClipInterrupted) {
		return path, size, true, nil
	}
	if err != nil {
		return "", 0, false, err
	}
	return path, size, false, nil
}

// clipDirectory is ~/Movies/ovm on macOS and ~/Videos/ovm elsewhere.
func clipDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	folder := "Videos"
	if runtime.GOOS == "darwin" {
		folder = "Movies"
	}
	return filepath.Join(home, folder, "ovm"), nil
}

// clipFileName builds a portable, collision-resistant name from the profile
// token and local time. Only [A-Za-z0-9_-] survive from the token.
func clipFileName(profileToken string, now time.Time) string {
	var builder strings.Builder
	for _, r := range profileToken {
		if builder.Len() >= 32 {
			break
		}
		if r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			builder.WriteRune(r)
		}
	}
	name := builder.String()
	if name == "" {
		name = "stream"
	}
	return "ovm-" + name + "-" + now.Format("20060102-150405.000") + ".mp4"
}

// validateClipPath accepts only an existing regular ovm clip file directly
// inside the clip folder, so a frontend-supplied path can never select an
// arbitrary file or directory.
func validateClipPath(directory, path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") {
		return "", errClipPath
	}
	clean := filepath.Clean(path)
	if filepath.Dir(clean) != filepath.Clean(directory) {
		return "", errClipPath
	}
	name := filepath.Base(clean)
	if !strings.HasPrefix(name, "ovm-") || !strings.HasSuffix(name, ".mp4") {
		return "", errClipPath
	}
	info, err := os.Lstat(clean)
	if err != nil || !info.Mode().IsRegular() {
		return "", errClipPath
	}
	return clean, nil
}

// revealCommand selects the platform file manager. macOS selects the file in
// Finder; Linux opens the containing folder.
func revealCommand(goos, path string) (string, []string, error) {
	switch goos {
	case "darwin":
		return "/usr/bin/open", []string{"-R", path}, nil
	case "linux":
		return "/usr/bin/xdg-open", []string{filepath.Dir(path)}, nil
	default:
		return "", nil, errRevealUnsupported
	}
}
