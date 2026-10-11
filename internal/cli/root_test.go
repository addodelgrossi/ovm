package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/internal/buildinfo"
)

func TestVersionHumanOutput(t *testing.T) {
	info := buildinfo.Info{Version: "v0.1.0", Commit: "abc123", Date: "2026-10-08T00:00:00Z"}
	cmd := NewRootCommand(info, testDependencies())
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	want := "ovm v0.1.0 (commit abc123, built 2026-10-08T00:00:00Z)\n"
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}

func TestVersionJSONOutput(t *testing.T) {
	info := buildinfo.Info{Version: "dev", Commit: "unknown", Date: "unknown"}
	cmd := NewRootCommand(info, testDependencies())
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"version", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var got buildinfo.Info
	if err := json.NewDecoder(&output).Decode(&got); err != nil {
		t.Fatalf("decode version output: %v; output = %q", err, output.String())
	}
	if got != info {
		t.Fatalf("decoded info = %#v, want %#v", got, info)
	}
	if strings.Contains(output.String(), "<") {
		t.Fatalf("JSON output unexpectedly contains HTML-sensitive output: %q", output.String())
	}
}
