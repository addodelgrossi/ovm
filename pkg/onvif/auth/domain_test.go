package auth

import (
	"net/url"
	"reflect"
	"testing"
)

func testOrigin(t *testing.T, raw string) origin {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse origin %q: %v", raw, err)
	}
	return originFromURL(parsed)
}

func TestParseDomainDirectiveValidationAndCanonicalization(t *testing.T) {
	t.Parallel()

	scope := testOrigin(t, "https://cam.invalid:8443/")
	valid := []struct {
		name   string
		domain string
		want   []string
	}{
		{name: "absolute path remains exact prefix", domain: "/onvif/device_service", want: []string{"/onvif/device_service"}},
		{name: "directory path", domain: "/onvif/", want: []string{"/onvif/"}},
		{name: "same-origin absolute URI", domain: "https://cam.invalid:8443/media/", want: []string{"/media/"}},
		{name: "query retained", domain: "/onvif/device_service?profile=main", want: []string{"/onvif/device_service?profile=main"}},
		{name: "deduplicated and sorted", domain: "/b/ /a/ /b/", want: []string{"/a/", "/b/"}},
	}
	for _, tt := range valid {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			space, err := parseDomainDirective(tt.domain, scope)
			if err != nil {
				t.Fatalf("parseDomainDirective(%q): %v", tt.domain, err)
			}
			if space.originWide || !reflect.DeepEqual(space.prefixes, tt.want) {
				t.Fatalf("parseDomainDirective(%q) = %+v, want %v", tt.domain, space, tt.want)
			}
		})
	}

	invalid := []string{
		"",
		"   \t  ",
		"/ok/ mailto:x",
		"https://other.invalid:8443/a/",
		"https://cam.invalid:9999/a/",
		"http://cam.invalid:8443/a/",
		"https://user@cam.invalid:8443/a/",
		"/a/#fragment",
		"urn:example:resource",
		"//cam.invalid:8443/a/",
		"relative/path",
		"ftp://cam.invalid:8443/a/",
	}
	for _, domain := range invalid {
		domain := domain
		t.Run("reject "+domain, func(t *testing.T) {
			t.Parallel()
			if _, err := parseDomainDirective(domain, scope); err == nil {
				t.Fatalf("parseDomainDirective(%q) accepted an unsafe directive", domain)
			}
		})
	}
}

func TestProtectionSpaceContainsAndSpecificity(t *testing.T) {
	t.Parallel()

	originWide := originWideProtectionSpace()
	if !originWide.contains("/anything/here?x=1") || originWide.specificity("/anything") != 0 {
		t.Fatal("origin-wide protection space did not cover every target at specificity zero")
	}

	space := protectionSpaceSet{prefixes: []string{"/onvif/", "/media/stream?profile="}}
	cases := []struct {
		target      string
		contained   bool
		specificity int
	}{
		{target: "/onvif/device_service", contained: true, specificity: len("/onvif/")},
		{target: "/media/stream?profile=main", contained: true, specificity: len("/media/stream?profile=")},
		{target: "/media/stream/other", contained: false, specificity: -1},
		{target: "/admin/status", contained: false, specificity: -1},
		{target: "/onvifextra/x", contained: false, specificity: -1},
	}
	for _, tt := range cases {
		if got := space.contains(tt.target); got != tt.contained {
			t.Fatalf("contains(%q) = %v, want %v", tt.target, got, tt.contained)
		}
		if got := space.specificity(tt.target); got != tt.specificity {
			t.Fatalf("specificity(%q) = %d, want %d", tt.target, got, tt.specificity)
		}
	}
}

func TestProtectionSpaceKeyAndBasicPrefix(t *testing.T) {
	t.Parallel()

	if originWideProtectionSpace().key() != "*" {
		t.Fatal("origin-wide key must be the reserved marker")
	}
	a := protectionSpaceSet{prefixes: []string{"/a/"}}
	b := protectionSpaceSet{prefixes: []string{"/b/"}}
	if a.key() == b.key() || a.key() == originWideProtectionSpace().key() {
		t.Fatal("distinct protection spaces must have distinct keys")
	}
	if got := basicProtectionPrefix("/onvif/device_service"); got != "/onvif/" {
		t.Fatalf("basicProtectionPrefix = %q, want /onvif/", got)
	}
}
