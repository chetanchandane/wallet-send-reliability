package durable

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCorruptSnapshotFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	var state map[string]int
	if f, _, err := Open(path, &state); err == nil {
		f.Close()
		t.Fatal("corruption accepted")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "{broken" {
		t.Fatal("corruption silently reset")
	}
}

func TestSnapshotReplacementAndLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	var state map[string]int
	f, exists, err := Open(path, &state)
	if err != nil || exists {
		t.Fatalf("open: %v %v", exists, err)
	}
	if other, _, err := Open(path, &state); err == nil {
		other.Close()
		t.Fatal("multiple writers")
	}
	if err := f.Save(map[string]int{"value": 1}); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(map[string]int{"value": 2}); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	f, exists, err = Open(path, &state)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if !exists || state["value"] != 2 {
		t.Fatalf("state: %v", state)
	}
}
