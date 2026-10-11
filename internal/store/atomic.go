package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type atomicStage string

const (
	stageDirectoryCreate      atomicStage = "directory_create"
	stageDirectoryMode        atomicStage = "directory_mode"
	stageDirectoryParentOpen  atomicStage = "directory_parent_open"
	stageDirectoryParentSync  atomicStage = "directory_parent_sync"
	stageDirectoryParentClose atomicStage = "directory_parent_close"
	stageTempCreate           atomicStage = "temp_create"
	stageTempMode             atomicStage = "temp_mode"
	stageEncode               atomicStage = "encode"
	stageFileSync             atomicStage = "file_sync"
	stageFileClose            atomicStage = "file_close"
	stageFinalMode            atomicStage = "final_mode"
	stageRename               atomicStage = "rename"
	stageParentOpen           atomicStage = "parent_open"
	stageParentSync           atomicStage = "parent_sync"
	stageParentClose          atomicStage = "parent_close"
)

type documentWriter interface {
	write(path string, document diskDocumentV1) (committed bool, err error)
}

type atomicJSONWriter struct {
	before func(atomicStage) error
}

func (writer atomicJSONWriter) write(path string, document diskDocumentV1) (bool, error) {
	directory := filepath.Dir(path)
	if err := writer.ensureDirectory(directory); err != nil {
		return false, err
	}
	if err := writer.fail(stageTempCreate); err != nil {
		return false, ErrPersistence
	}
	temporary, err := os.CreateTemp(directory, ".state-*.tmp")
	if err != nil {
		return false, ErrPersistence
	}
	temporaryPath := temporary.Name()
	closed := false
	cleanup := func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}

	if err := writer.fail(stageTempMode); err != nil {
		cleanup()
		return false, ErrPersistence
	}
	if err := temporary.Chmod(0o600); err != nil {
		cleanup()
		return false, ErrPersistence
	}
	if err := writer.fail(stageEncode); err != nil {
		cleanup()
		return false, ErrPersistence
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document); err != nil {
		cleanup()
		return false, ErrPersistence
	}
	if err := writer.fail(stageFileSync); err != nil {
		cleanup()
		return false, ErrPersistence
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return false, ErrPersistence
	}
	if err := writer.fail(stageFileClose); err != nil {
		cleanup()
		return false, ErrPersistence
	}
	if err := temporary.Close(); err != nil {
		closed = true
		_ = os.Remove(temporaryPath)
		return false, ErrPersistence
	}
	closed = true
	if err := writer.fail(stageFinalMode); err != nil {
		_ = os.Remove(temporaryPath)
		return false, ErrPersistence
	}
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		_ = os.Remove(temporaryPath)
		return false, ErrPersistence
	}
	if err := writer.fail(stageRename); err != nil {
		_ = os.Remove(temporaryPath)
		return false, ErrPersistence
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		_ = os.Remove(temporaryPath)
		return false, ErrPersistence
	}

	// The rename is now visible. Any subsequent error reports uncertain
	// durability, but committed remains true so memory never diverges from disk.
	if err := writer.fail(stageParentOpen); err != nil {
		return true, ErrPersistence
	}
	// Opening the caller-selected repository directory is required to make the rename durable.
	parent, err := os.Open(directory) //nolint:gosec // directory comes from the configured repository path
	if err != nil {
		return true, ErrPersistence
	}
	if err := writer.fail(stageParentSync); err != nil {
		_ = parent.Close()
		return true, ErrPersistence
	}
	if err := parent.Sync(); err != nil {
		_ = parent.Close()
		return true, ErrPersistence
	}
	if err := writer.fail(stageParentClose); err != nil {
		_ = parent.Close()
		return true, ErrPersistence
	}
	if err := parent.Close(); err != nil {
		return true, ErrPersistence
	}
	return true, nil
}

// ensureDirectory creates every missing level of the target directory one at a
// time. Each newly created level is chmod'ed to 0700 and its parent is fsync'ed
// before descending, so a crash cannot leave a directory entry that is not
// durable. Levels that already exist are left untouched. The operation is
// retry-safe: a level that appears between the existence check and creation
// (for example residue from a concurrent or interrupted attempt) is tolerated
// instead of failing the write.
func (writer atomicJSONWriter) ensureDirectory(directory string) error {
	missing, err := writer.missingLevels(directory)
	if err != nil {
		return err
	}
	// missing is ordered shallowest-first so each parent exists before its child.
	for _, level := range missing {
		if err := writer.fail(stageDirectoryCreate); err != nil {
			return ErrPersistence
		}
		// os.Mkdir tolerates an existing entry so a retry over residue is idempotent.
		if err := os.Mkdir(level, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return ErrPersistence
		}
		if err := writer.fail(stageDirectoryMode); err != nil {
			return ErrPersistence
		}
		// The configuration directory tree is intentionally stricter than a file.
		if err := os.Chmod(level, 0o700); err != nil { //nolint:gosec // directory policy is exactly 0700
			return ErrPersistence
		}
		if err := writer.syncDirectoryParent(level); err != nil {
			return err
		}
	}
	return nil
}

// missingLevels returns the chain of directories that must be created to
// materialize directory, ordered shallowest-first. Levels that already exist
// are excluded. A non-ErrNotExist stat error is surfaced as ErrPersistence.
func (writer atomicJSONWriter) missingLevels(directory string) ([]string, error) {
	var missing []string
	for current := directory; ; current = filepath.Dir(current) {
		info, err := os.Stat(current)
		if err == nil {
			if !info.IsDir() {
				return nil, ErrPersistence
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, ErrPersistence
		}
		missing = append(missing, current)
		if parent := filepath.Dir(current); parent == current {
			// Reached the filesystem root without finding an existing ancestor.
			break
		}
	}
	reverse(missing)
	return missing, nil
}

func reverse(values []string) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

// syncDirectoryParent fsyncs the parent of a freshly created or repaired level
// so the new directory entry is durable before its child is created.
func (writer atomicJSONWriter) syncDirectoryParent(level string) error {
	if err := writer.fail(stageDirectoryParentOpen); err != nil {
		return ErrPersistence
	}
	parentPath := filepath.Dir(level)
	// The parent is derived exclusively from the configured repository path.
	parent, err := os.Open(parentPath) //nolint:gosec // repository parent directory is intentional
	if err != nil {
		return ErrPersistence
	}
	if err := writer.fail(stageDirectoryParentSync); err != nil {
		_ = parent.Close()
		return ErrPersistence
	}
	if err := parent.Sync(); err != nil {
		_ = parent.Close()
		return ErrPersistence
	}
	if err := writer.fail(stageDirectoryParentClose); err != nil {
		_ = parent.Close()
		return ErrPersistence
	}
	if err := parent.Close(); err != nil {
		return ErrPersistence
	}
	return nil
}

func (writer atomicJSONWriter) fail(stage atomicStage) error {
	if writer.before == nil {
		return nil
	}
	return writer.before(stage)
}
