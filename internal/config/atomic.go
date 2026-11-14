package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const (
	windowsRenameMaxAttempts = 5
	windowsRenameBackoff     = 5 * time.Millisecond
)

// AtomicWriteFile writes data to a temporary file in the same directory as filePath,
// flushes buffers to disk, sets permissions, and atomically renames the temporary file over filePath.
func AtomicWriteFile(filePath string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating directory for atomic write: %w", err)
	}

	// Create temp file in same directory to ensure atomic same-volume rename
	tmpFile, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temporary file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Ensure cleanup on failure
	success := false
	defer func() {
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()

	// Ensure restricted permissions
	if err := tmpFile.Chmod(perm); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("setting file permissions on %s: %w", tmpPath, err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("writing data to %s: %w", tmpPath, err)
	}

	// Flush OS page cache to disk
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("syncing data to %s: %w", tmpPath, err)
	}

	// File MUST be closed prior to os.Rename on Windows
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("closing temporary file %s: %w", tmpPath, err)
	}

	// Atomic replace with Windows retry backoff for transient AV/indexer locks
	if err := atomicRename(tmpPath, filePath); err != nil {
		return fmt.Errorf("atomic rename from %s to %s: %w", tmpPath, filePath, err)
	}

	success = true
	return nil
}

func atomicRename(tmpPath, filePath string) error {
	maxAttempts := 1
	if runtime.GOOS == "windows" {
		maxAttempts = windowsRenameMaxAttempts
	}

	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err = os.Rename(tmpPath, filePath)
		if err == nil {
			return nil
		}
		if attempt < maxAttempts-1 {
			time.Sleep(time.Duration(attempt+1) * windowsRenameBackoff)
		}
	}
	return err
}
