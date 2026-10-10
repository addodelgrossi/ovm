package fakedevice

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image/jpeg"
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

// fixtureIsText reports whether a manifest path names a text fixture whose
// bytes are safe to scan as a string. XML fixtures are text; the JPEG fixture
// is binary and is enumerated and hashed but not substring-scanned as text.
func fixtureIsText(path string) bool {
	return filepath.Ext(path) == ".xml"
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
		if !fixtureIsText(entry.Path) {
			continue
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
		if entry.IsDir() {
			return nil
		}
		extension := filepath.Ext(path)
		if extension != ".xml" && extension != ".jpg" {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if !listed[relative] {
			t.Errorf("fixture %q is missing from manifest", relative)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk fixtures: %v", err)
	}
}

// TestFixtureJPEGIsDecodableSyntheticImage proves the JPEG fixture decodes as a
// valid baseline image and carries no JFIF/EXIF/comment metadata segments, so
// it is safe to serve from the synthetic snapshot endpoint and contains no
// embedded vendor strings. It enumerates the manifest for JPEG entries so the
// check follows the manifest rather than a hard-coded path.
func TestFixtureJPEGIsDecodableSyntheticImage(t *testing.T) {
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

	jpegCount := 0
	for _, entry := range manifest.Fixtures {
		if filepath.Ext(entry.Path) != ".jpg" {
			continue
		}
		jpegCount++
		resolved, resolveErr := resolveFixturePath(root, entry.Path)
		if resolveErr != nil {
			t.Errorf("unsafe fixture path %q: %v", entry.Path, resolveErr)
			continue
		}
		//nolint:gosec // resolveFixturePath proves the manifest path stays under root.
		data, err := os.ReadFile(resolved)
		if err != nil {
			t.Errorf("read JPEG fixture %q: %v", entry.Path, err)
			continue
		}
		if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
			t.Errorf("JPEG fixture %q does not decode: %v", entry.Path, err)
		}
		// Reject metadata application segments (APP0..APP15 = 0xFFE0..0xFFEF)
		// and comment segments (0xFFFE), which are the carriers of JFIF/EXIF
		// identifiers and free-form text that could embed vendor strings.
		for index := 0; index+1 < len(data); index++ {
			if data[index] != 0xFF {
				continue
			}
			marker := data[index+1]
			if (marker >= 0xE0 && marker <= 0xEF) || marker == 0xFE {
				t.Errorf("JPEG fixture %q carries a metadata/comment segment 0xFF%02X", entry.Path, marker)
				break
			}
			if marker == 0xDA { // start of scan: entropy data follows, stop scanning markers
				break
			}
		}
	}
	if jpegCount == 0 {
		t.Fatal("manifest lists no JPEG fixture")
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
