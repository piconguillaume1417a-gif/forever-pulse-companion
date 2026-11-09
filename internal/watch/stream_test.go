package watch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenSnapshotDetectsWriteDuringProcessing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ForeverPulse.lua")
	if err := os.WriteFile(path, []byte("Saved = {}"), 0600); err != nil {
		t.Fatal(err)
	}
	f, stable, err := OpenSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = stable(); err != nil {
		t.Fatal(err)
	}
	w, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.WriteString("\nSaved = {1,2,3}"); err != nil {
		w.Close()
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if err = stable(); err == nil {
		t.Fatal("a concurrent write was mistaken for a stable snapshot")
	}
}
