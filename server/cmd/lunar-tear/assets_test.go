package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateAssetsMissingList(t *testing.T) {
	root := t.TempDir()
	releaseDir := filepath.Join(root, "assets", "release")
	if err := os.MkdirAll(releaseDir, 0o755); err != nil {
		t.Fatalf("mkdir release: %v", err)
	}
	if err := os.WriteFile(filepath.Join(releaseDir, "20240404193219.bin.e"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write bin.e: %v", err)
	}

	err := validateAssets(root)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), filepath.Clean(filepath.Join(root, "assets", "revisions", "0", "list.bin"))) {
		t.Fatalf("expected list.bin path in error, got %q", err)
	}
}

func TestValidateAssetsMissingRelease(t *testing.T) {
	root := t.TempDir()
	listPath := filepath.Join(root, "assets", "revisions", "0", "list.bin")
	if err := os.MkdirAll(filepath.Dir(listPath), 0o755); err != nil {
		t.Fatalf("mkdir list dir: %v", err)
	}
	if err := os.WriteFile(listPath, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write list.bin: %v", err)
	}

	err := validateAssets(root)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), filepath.Clean(filepath.Join(root, "assets", "release", "*.bin.e"))) {
		t.Fatalf("expected release glob in error, got %q", err)
	}
}

func TestValidateAssetsSuccess(t *testing.T) {
	root := t.TempDir()
	listPath := filepath.Join(root, "assets", "revisions", "0", "list.bin")
	if err := os.MkdirAll(filepath.Dir(listPath), 0o755); err != nil {
		t.Fatalf("mkdir list dir: %v", err)
	}
	if err := os.WriteFile(listPath, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write list.bin: %v", err)
	}

	releasePath := filepath.Join(root, "assets", "release", "20240404193219.bin.e")
	if err := os.MkdirAll(filepath.Dir(releasePath), 0o755); err != nil {
		t.Fatalf("mkdir release dir: %v", err)
	}
	if err := os.WriteFile(releasePath, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write bin.e: %v", err)
	}

	if err := validateAssets(root); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
