// Package release validates the physical-acceptance manifest that gates a
// public release. A release may be published only when two cameras passed the
// documented acceptance checks and the manifest is free of credentials and
// identifying network data.
package release

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strings"
)

// ManifestSchema identifies the manifest format.
const ManifestSchema = "ovm.validation-manifest/v1"

// Check results.
const (
	ResultPass         = "pass"
	ResultFail         = "fail"
	ResultNotSupported = "not_supported"
)

// RequiredChecks must pass on every camera.
var RequiredChecks = []string{
	"discovery", "authentication", "profiles", "rtsp_copy", "live_preview",
	"h264", "ntp", "password_change", "clock_check", "stability", "report",
}

// OptionalChecks may be not_supported when the camera lacks the feature, but
// never fail.
var OptionalChecks = []string{"ir_day", "events", "recording", "snapshot"}

// Minimum stability gate.
const (
	MinimumCameras      = 2
	MinimumCycles       = 100
	MinimumCycleSeconds = 20
)

// Manifest is the sanitized record of a physical acceptance run.
type Manifest struct {
	Schema      string   `json:"schema"`
	Release     string   `json:"release"`
	Commit      string   `json:"ovm_commit"`
	ValidatedAt string   `json:"validated_at"`
	Validator   string   `json:"validator"`
	Host        Host     `json:"host"`
	Cameras     []Camera `json:"cameras"`
}

// Host is the computer the acceptance ran on.
type Host struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

// Camera is one validated camera. It identifies the model, never the unit.
type Camera struct {
	Label        string            `json:"label"`
	Manufacturer string            `json:"manufacturer"`
	Model        string            `json:"model"`
	Firmware     string            `json:"firmware"`
	Checks       map[string]string `json:"checks"`
	Stability    Stability         `json:"stability"`
	Notes        string            `json:"notes,omitempty"`
}

// Stability is the summary of the camera's stability test.
type Stability struct {
	Cycles       int `json:"cycles"`
	CycleSeconds int `json:"cycle_seconds"`
	Passed       int `json:"passed"`
}

var (
	commitPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	releasePattern = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)
	datePattern    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	macPattern     = regexp.MustCompile(`(?i)\b[0-9a-f]{2}([:-][0-9a-f]{2}){5}\b`)
	ipv4Pattern    = regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}\b`)
	forbiddenKeys  = []string{"password", "secret", "token", "serial", "mac", "hw_address", "username", "user", "credential"}
)

// documentation ranges (RFC 5737) may appear in examples.
var documentationPrefixes = []netip.Prefix{
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
}

// Validate parses a manifest and checks it against the release tag. It
// returns every problem found, not only the first.
func Validate(data []byte, releaseTag string) (Manifest, []error) {
	var problems []error
	add := func(format string, args ...any) { problems = append(problems, fmt.Errorf(format, args...)) }

	problems = append(problems, sanitized(data)...)

	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, append(problems, fmt.Errorf("the manifest is not valid JSON for %s: %w", ManifestSchema, err))
	}
	if manifest.Schema != ManifestSchema {
		add("schema must be %q", ManifestSchema)
	}
	if !releasePattern.MatchString(manifest.Release) {
		add("release must look like v0.1.0")
	} else if releaseTag != "" && manifest.Release != releaseTag {
		add("release %s does not match the tag %s", manifest.Release, releaseTag)
	}
	if !commitPattern.MatchString(manifest.Commit) || strings.Trim(manifest.Commit, "0") == "" {
		add("ovm_commit must be the full 40-character hash of the validated commit")
	}
	if !datePattern.MatchString(manifest.ValidatedAt) {
		add("validated_at must be a YYYY-MM-DD date")
	}
	if strings.TrimSpace(manifest.Validator) == "" {
		add("validator is required")
	}
	if manifest.Host.OS == "" || manifest.Host.Arch == "" {
		add("host os and arch are required")
	}
	if len(manifest.Cameras) < MinimumCameras {
		add("at least %d cameras are required, found %d", MinimumCameras, len(manifest.Cameras))
	}
	labels := map[string]bool{}
	for index, camera := range manifest.Cameras {
		name := camera.Label
		if name == "" {
			name = fmt.Sprintf("camera %d", index+1)
			add("%s: label is required", name)
		}
		if labels[camera.Label] {
			add("%s: labels must be unique", name)
		}
		labels[camera.Label] = true
		if camera.Manufacturer == "" || camera.Model == "" || camera.Firmware == "" {
			add("%s: manufacturer, model, and firmware are required", name)
		}
		for _, check := range RequiredChecks {
			if result := camera.Checks[check]; result != ResultPass {
				add("%s: required check %q is %q, want pass", name, check, result)
			}
		}
		for _, check := range OptionalChecks {
			if result, ok := camera.Checks[check]; ok && result != ResultPass && result != ResultNotSupported {
				add("%s: optional check %q is %q, want pass or not_supported", name, check, result)
			}
		}
		for check := range camera.Checks {
			if !slices.Contains(RequiredChecks, check) && !slices.Contains(OptionalChecks, check) {
				add("%s: unknown check %q", name, check)
			}
		}
		stability := camera.Stability
		if stability.Cycles < MinimumCycles || stability.CycleSeconds < MinimumCycleSeconds || stability.Passed != stability.Cycles {
			add("%s: stability must pass every one of at least %d cycles of %d s", name, MinimumCycles, MinimumCycleSeconds)
		}
	}
	return manifest, problems
}

// sanitized rejects credential keys, URLs, MAC addresses, and IPv4 addresses
// outside the documentation ranges anywhere in the document.
func sanitized(data []byte) []error {
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil
	}
	var problems []error
	var walk func(path string, value any)
	walk = func(path string, value any) {
		switch typed := value.(type) {
		case map[string]any:
			for key, child := range typed {
				lower := strings.ToLower(key)
				for _, forbidden := range forbiddenKeys {
					if lower == forbidden || strings.HasSuffix(lower, "_"+forbidden) {
						problems = append(problems, fmt.Errorf("%s.%s: field names a credential or unit identifier", path, key))
					}
				}
				walk(path+"."+key, child)
			}
		case []any:
			for index, child := range typed {
				walk(fmt.Sprintf("%s[%d]", path, index), child)
			}
		case string:
			if strings.Contains(typed, "://") {
				problems = append(problems, fmt.Errorf("%s: URLs are not allowed", path))
			}
			if macPattern.MatchString(typed) {
				problems = append(problems, fmt.Errorf("%s: MAC addresses are not allowed", path))
			}
			for _, candidate := range ipv4Pattern.FindAllString(typed, -1) {
				address, err := netip.ParseAddr(candidate)
				if err != nil {
					continue
				}
				if !slices.ContainsFunc(documentationPrefixes, func(prefix netip.Prefix) bool { return prefix.Contains(address) }) {
					problems = append(problems, fmt.Errorf("%s: IPv4 addresses outside RFC 5737 are not allowed", path))
				}
			}
		}
	}
	walk("$", document)
	return problems
}

// ErrInvalid summarizes a manifest that failed validation.
var ErrInvalid = errors.New("the validation manifest does not allow a release")
