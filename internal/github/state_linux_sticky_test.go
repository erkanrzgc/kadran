//go:build linux

package github

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateFileRejectsStickyWorldWritableParent(t *testing.T) {
	sub := filepath.Join(t.TempDir(), "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "state.json")
	if err := saveState(path, sampleState()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, os.ModeSticky|0o777); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := loadState(path, testClientID); ok || err == nil {
		t.Fatalf("sticky world-writable parent accepted: ok %v, err %v", ok, err)
	}
}
