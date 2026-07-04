package kubeconfig

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteCreatesFileWithOwnerOnlyPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kubeconfig.yaml")
	content := []byte("apiVersion: v1\nkind: Config\n")

	if err := Write(path, content); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("content = %q, want %q", got, content)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions = %o, want 600", perm)
	}
}

func TestWriteOverwritesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kubeconfig.yaml")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Write(path, []byte("new")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("content = %q, want %q", got, "new")
	}
}

func TestWriteFailsForMissingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "kubeconfig.yaml")
	if err := Write(path, []byte("data")); err == nil {
		t.Error("Write() = nil, want error for missing directory")
	}
}

func TestWriteLeavesNoTempFilesBehind(t *testing.T) {
	dir := t.TempDir()
	if err := Write(filepath.Join(dir, "kubeconfig.yaml"), []byte("data")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "kubeconfig.yaml" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory contains %v, want only kubeconfig.yaml", names)
	}
}
