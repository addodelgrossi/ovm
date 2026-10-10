package main

import (
	"os"
	"strings"
	"testing"
)

func TestGeneratedBindingsExposeExpectedSafeSurface(t *testing.T) {
	declarations, err := os.ReadFile("frontend/wailsjs/go/main/App.d.ts")
	if err != nil {
		t.Fatalf("read generated App declarations: %v", err)
	}
	declarationText := string(declarations)
	for _, method := range []string{
		"Bootstrap", "ListInterfaces", "Scan", "CancelScan", "AddManualDevice",
		"AuthenticateDevice", "ForgetDevice", "SelectDevice", "LoadOverview",
		"LoadProfiles", "UpdatePreferences", "OpenInVLC",
	} {
		if !strings.Contains(declarationText, "export function "+method+"(") {
			t.Errorf("generated declarations do not expose %s", method)
		}
	}

	models, err := os.ReadFile("frontend/wailsjs/go/models.ts")
	if err != nil {
		t.Fatalf("read generated models: %v", err)
	}
	modelText := string(models)
	lower := strings.ToLower(modelText)
	for _, forbidden := range []string{"export namespace store", "export class secret", "credential_reference"} {
		if strings.Contains(lower, forbidden) {
			t.Errorf("generated models contain forbidden secret-storage surface %q", forbidden)
		}
	}

	responseStart := strings.Index(modelText, "export class AuthenticateDeviceResponse")
	if responseStart < 0 {
		t.Fatal("generated authentication response model was not found")
	}
	responseEnd := strings.Index(modelText[responseStart:], "export class CapabilityFlags")
	if responseEnd < 0 {
		t.Fatal("generated authentication response model terminator was not found")
	}
	authenticationResponse := strings.ToLower(modelText[responseStart : responseStart+responseEnd])
	if strings.Contains(authenticationResponse, "password") || strings.Contains(authenticationResponse, "secret") {
		t.Fatalf("generated authentication response contains sensitive fields: %s", authenticationResponse)
	}
}
