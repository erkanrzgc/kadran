//go:build linux

package github

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateFileRejectsSymlinkAndLoosePerms(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.json")
	if err := saveState(real, sampleState()); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := loadState(link, testClientID); ok || err == nil {
		t.Fatalf("symlink accepted: ok %v, err %v", ok, err)
	}
	if err := os.Chmod(real, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := loadState(real, testClientID); ok || err == nil {
		t.Fatalf("0644 file accepted: ok %v, err %v", ok, err)
	}
}

func TestStateFileRejectsWritableDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := saveState(path, sampleState()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := loadState(path, testClientID); ok || err == nil {
		t.Fatalf("world-writable directory accepted: ok %v, err %v", ok, err)
	}
}

func TestStateFileRejectsRelativePath(t *testing.T) {
	if _, err := readStateFile("state.json"); err == nil {
		t.Fatal("relative path accepted")
	}
}

func TestSavedStateIs0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := saveState(path, sampleState()); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, err %v", fi.Mode(), err)
	}
}
