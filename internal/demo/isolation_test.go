package demo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestNoProductionStateFileOrKeyringUse proves the demo touches neither a disk
// state file nor the platform keyring. It isolates HOME and the XDG config/data
// directories to fresh, empty temp trees, runs a full Bootstrap/Profiles cycle,
// and asserts that no file was created anywhere beneath them. A disk-backed
// store or a keyring file fallback would materialize a file here; the demo's
// in-memory repository and in-memory secret provider create none.
func TestNoProductionStateFileOrKeyringUse(t *testing.T) {
	home := t.TempDir()
	config := t.TempDir()
	data := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("XDG_DATA_HOME", data)

	env, err := NewEnvironment(context.Background())
	if err != nil {
		t.Fatalf("NewEnvironment() error = %v", err)
	}

	ctx := context.Background()
	if _, err := env.Manager().Bootstrap(ctx); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if _, err := env.Manager().Profiles(ctx, env.DeviceID()); err != nil {
		t.Fatalf("Profiles() error = %v", err)
	}

	closeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := env.Close(closeCtx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	for _, root := range []string{home, config, data} {
		if files := filesUnder(t, root); len(files) != 0 {
			t.Fatalf("demo created files under %s: %v", root, files)
		}
	}
}

// filesUnder returns every regular file beneath root (directories excluded).
func filesUnder(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return files
}
