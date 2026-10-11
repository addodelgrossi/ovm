package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestMissingManifestBlocksTheRelease(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if code := run([]string{"testdata/does-not-exist.json", "v0.1.0"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(stderr.String(), "release blocked") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if code := run(nil, &stdout, &stderr); code != 2 {
		t.Fatalf("usage exit code = %d", code)
	}
}

func TestTemplateIsNotAReleaseManifest(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if code := run([]string{"../../../docs/validation/template.json", "v0.1.0"}, &stdout, &stderr); code != 1 {
		t.Fatalf("the template with a zero commit passed: %s", stdout.String())
	}
}
