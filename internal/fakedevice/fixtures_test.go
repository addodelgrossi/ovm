package fakedevice

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixtureManifest struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Fixtures      []fixtureManifestEntry `json:"fixtures"`
}

type fixtureManifestEntry struct {
	Path       string   `json:"path"`
	Synthetic  bool     `json:"synthetic"`
	Provenance string   `json:"provenance"`
	SourceSpec string   `json:"sourceSpec"`
	License    string   `json:"license"`
	SHA256     string   `json:"sha256"`
	Redactions []string `json:"redactions"`
}

func TestFixtureManifestCoversSyntheticFixtures(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "testdata", "fixtures")
	//nolint:gosec // The repository-owned test fixture path is fixed below.
	manifestData, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatalf("read fixture manifest: %v", err)
	}
	var manifest fixtureManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("parse fixture manifest: %v", err)
	}
	if manifest.SchemaVersion != 1 || len(manifest.Fixtures) == 0 {
		t.Fatalf("manifest = %+v, want schema version 1 and fixtures", manifest)
	}

	listed := make(map[string]bool, len(manifest.Fixtures))
	for _, entry := range manifest.Fixtures {
		if listed[entry.Path] {
			t.Errorf("duplicate manifest path %q", entry.Path)
		}
		listed[entry.Path] = true
		if !entry.Synthetic || entry.Provenance == "" || entry.SourceSpec == "" || entry.License != "MIT" || len(entry.Redactions) == 0 {
			t.Errorf("incomplete provenance for %q: %+v", entry.Path, entry)
		}
		resolved, resolveErr := resolveFixturePath(root, entry.Path)
		if resolveErr != nil {
			t.Errorf("unsafe fixture path %q: %v", entry.Path, resolveErr)
			continue
		}
		//nolint:gosec // resolveFixturePath proves the manifest path stays under root.
		data, err := os.ReadFile(resolved)
		if err != nil {
			t.Errorf("read fixture %q: %v", entry.Path, err)
			continue
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != entry.SHA256 {
			t.Errorf("fixture %q SHA-256 = %s, want %s", entry.Path, got, entry.SHA256)
		}
		text := strings.ToLower(string(data))
		for _, forbidden := range []string{"authorization:", "proxy-authorization:", "camera-secret", "192.168.", "10.0."} {
			if strings.Contains(text, forbidden) {
				t.Errorf("fixture %q contains forbidden material %q", entry.Path, forbidden)
			}
		}
	}

	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".xml" {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if !listed[relative] {
			t.Errorf("XML fixture %q is missing from manifest", relative)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk fixtures: %v", err)
	}
}

func TestFixtureManifestPathsStayWithinFixtureRoot(t *testing.T) {
	t.Parallel()

	root := filepath.Clean(filepath.Join("..", "..", "testdata", "fixtures"))
	for _, path := range []string{"soap12/example.xml", "../outside.xml", "/absolute.xml"} {
		resolved, err := resolveFixturePath(root, path)
		if path == "soap12/example.xml" {
			if err != nil || !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
				t.Fatalf("resolveFixturePath(%q) = (%q, %v)", path, resolved, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("resolveFixturePath(%q) unexpectedly accepted %q", path, resolved)
		}
	}
}

func resolveFixturePath(root, manifestPath string) (string, error) {
	if manifestPath == "" || filepath.IsAbs(manifestPath) {
		return "", errors.New("fixture path must be non-empty and relative")
	}
	resolved := filepath.Clean(filepath.Join(root, filepath.FromSlash(manifestPath)))
	relative, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("fixture path escapes fixture root")
	}
	return resolved, nil
}
