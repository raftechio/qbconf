package kubeconfig

import (
	"fmt"
	"os"
	"path/filepath"
)

// Write stores a kubeconfig at path with owner-only permissions. The file is
// written to a temporary sibling first and renamed into place, so readers
// never observe a partially written kubeconfig.
func Write(path string, data []byte) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".qbconf-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary kubeconfig: %w", err)
	}
	defer func() {
		// No-ops after a successful rename.
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	// Kubeconfigs carry credentials: never group/world readable.
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("set kubeconfig permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write kubeconfig: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write kubeconfig: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write kubeconfig to %s: %w", path, err)
	}
	return nil
}
