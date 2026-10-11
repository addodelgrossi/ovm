package release

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func template(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile("../../docs/validation/template.json")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document["ovm_commit"] = strings.Repeat("a", 40)
	return document
}

func validate(t *testing.T, document map[string]any, tag string) []error {
	t.Helper()
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	_, problems := Validate(data, tag)
	return problems
}

func camera(document map[string]any, index int) map[string]any {
	return document["cameras"].([]any)[index].(map[string]any)
}

func TestTemplateShapeIsAccepted(t *testing.T) {
	t.Parallel()
	if problems := validate(t, template(t), "v0.1.0"); len(problems) != 0 {
		t.Fatalf("problems = %v", problems)
	}
}

func TestPlaceholderCommitIsRejected(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../docs/validation/template.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, problems := Validate(data, "v0.1.0"); len(problems) != 1 {
		t.Fatalf("the template's placeholder commit must be its only problem: %v", problems)
	}
}

func TestGateRejectsIncompleteAcceptance(t *testing.T) {
	t.Parallel()
	cases := map[string]func(map[string]any){
		"one camera": func(d map[string]any) { d["cameras"] = d["cameras"].([]any)[:1] },
		"failed required check": func(d map[string]any) {
			camera(d, 0)["checks"].(map[string]any)["h264"] = ResultFail
		},
		"missing required check": func(d map[string]any) {
			delete(camera(d, 1)["checks"].(map[string]any), "stability")
		},
		"failed optional check": func(d map[string]any) {
			camera(d, 1)["checks"].(map[string]any)["events"] = ResultFail
		},
		"short stability": func(d map[string]any) {
			camera(d, 0)["stability"] = map[string]any{"cycles": 50, "cycle_seconds": 20, "passed": 50}
		},
		"failed stability cycle": func(d map[string]any) {
			camera(d, 0)["stability"] = map[string]any{"cycles": 100, "cycle_seconds": 20, "passed": 99}
		},
		"duplicate label": func(d map[string]any) { camera(d, 1)["label"] = "camera-a" },
		"private address": func(d map[string]any) { camera(d, 0)["notes"] = "tested at 10.0.0.5" },
		"url":             func(d map[string]any) { camera(d, 0)["notes"] = "rtsp://cam/stream" },
		"mac address":     func(d map[string]any) { camera(d, 0)["notes"] = "aa:bb:cc:dd:ee:ff" },
		"unknown field":   func(d map[string]any) { camera(d, 0)["serial"] = "SN1" },
		"short commit":    func(d map[string]any) { d["ovm_commit"] = "abc123" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			document := template(t)
			mutate(document)
			if problems := validate(t, document, "v0.1.0"); len(problems) == 0 {
				t.Fatal("the manifest was accepted")
			}
		})
	}
	if problems := validate(t, template(t), "v0.1.1"); len(problems) == 0 {
		t.Fatal("a tag mismatch was accepted")
	}
}
