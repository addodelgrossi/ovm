package main

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	core "github.com/addodelgrossi/ovm/internal/app"
)

// VLCLauncher is the narrow external-player seam used by App.
type VLCLauncher interface {
	Available() bool
	Launch(context.Context, core.StreamTransport, string) error
}

// VLCError is a safe typed launch failure. It never contains a path, URI,
// process output, or backend diagnostic.
type VLCError struct {
	Category core.ErrorCategory
	Code     string
	Message  string
}

func (e *VLCError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type processStarter interface {
	Start(context.Context, string, []string) error
}

type commandProcessStarter struct{}

func (commandProcessStarter) Start(ctx context.Context, executable string, arguments []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// executable comes only from the platform allowlist and arguments are fixed
	// options plus a validated backend-resolved RTSP URI. No shell is involved.
	// The external player intentionally outlives the short binding call, so its
	// process context is detached only after checking the call context. Wait runs
	// in a reaper goroutine instead of releasing the process and leaking a
	// CommandContext watcher.
	command := exec.CommandContext(context.WithoutCancel(ctx), executable, arguments...) //nolint:gosec // allowlisted executable and validated arguments
	if err := command.Start(); err != nil {
		return err
	}
	go func() {
		_ = command.Wait()
	}()
	return nil
}

type productionVLCLauncher struct {
	path    string
	starter processStarter
}

func newProductionVLCLauncher() VLCLauncher {
	return &productionVLCLauncher{
		path:    findAllowlistedVLC(runtime.GOOS, os.Getenv, os.Stat),
		starter: commandProcessStarter{},
	}
}

func (v *productionVLCLauncher) Available() bool {
	return v != nil && v.path != "" && v.starter != nil
}

func (v *productionVLCLauncher) Launch(ctx context.Context, transport core.StreamTransport, streamURI string) error {
	if !v.Available() {
		return &VLCError{Category: core.CategoryInternal, Code: "vlc_unavailable", Message: "VLC is unavailable"}
	}
	if transport != core.StreamTransportTCP && transport != core.StreamTransportUDP {
		return &VLCError{Category: core.CategoryUsage, Code: "invalid_transport", Message: "the stream transport is not supported"}
	}
	if !safeVLCArgument(streamURI) {
		return &VLCError{Category: core.CategoryProtocol, Code: "unsafe_stream_uri", Message: "the resolved stream URI is not safe"}
	}

	arguments := make([]string, 0, 3)
	if transport == core.StreamTransportTCP {
		arguments = append(arguments, "--rtsp-tcp")
	}
	// End option parsing before the URI even though a valid absolute RTSP URI
	// cannot begin with '-'.
	arguments = append(arguments, "--", streamURI)
	if err := v.starter.Start(ctx, v.path, arguments); err != nil {
		return &VLCError{Category: core.CategoryInternal, Code: "vlc_launch_failed", Message: "VLC could not be opened"}
	}
	return nil
}

func safeVLCArgument(raw string) bool {
	if raw == "" {
		return false
	}
	for _, character := range raw {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" {
		return false
	}
	if !strings.EqualFold(parsed.Scheme, "rtsp") && !strings.EqualFold(parsed.Scheme, "rtsps") {
		return false
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "" {
		return false
	}
	decodedPath, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil {
		return false
	}
	for _, character := range decodedPath {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func findAllowlistedVLC(goos string, getenv func(string) string, stat func(string) (os.FileInfo, error)) string {
	candidates := vlcAllowlist(goos, getenv)
	for _, candidate := range candidates {
		if candidate == "" || !filepath.IsAbs(candidate) {
			continue
		}
		clean := filepath.Clean(candidate)
		info, err := stat(clean)
		if err != nil || info.IsDir() {
			continue
		}
		if goos != "windows" && info.Mode().Perm()&0o111 == 0 {
			continue
		}
		return clean
	}
	return ""
}

func vlcAllowlist(goos string, getenv func(string) string) []string {
	switch goos {
	case "darwin":
		return []string{
			"/Applications/VLC.app/Contents/MacOS/VLC",
			filepath.Join(getenv("HOME"), "Applications", "VLC.app", "Contents", "MacOS", "VLC"),
		}
	case "linux":
		return []string{"/usr/bin/vlc", "/usr/local/bin/vlc", "/snap/bin/vlc"}
	case "windows":
		return []string{
			filepath.Join(getenv("ProgramFiles"), "VideoLAN", "VLC", "vlc.exe"),
			filepath.Join(getenv("ProgramFiles(x86)"), "VideoLAN", "VLC", "vlc.exe"),
			filepath.Join(getenv("LOCALAPPDATA"), "Programs", "VideoLAN", "VLC", "vlc.exe"),
		}
	default:
		return nil
	}
}
