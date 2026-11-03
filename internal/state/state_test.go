package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "statefile.json")
	f := &File{
		Packs: []PackRecord{
			{Number: 1, Name: "a.bin", Path: "/data/a.bin", Size: 10},
			{Number: 2, Name: "b.bin", Path: "/data/b.bin", Size: 20},
		},
		Resume: map[string]int64{"a.bin": 5},
	}
	if err := Save(path, f); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Packs) != 2 || got.Packs[0].Name != "a.bin" || got.Packs[1].Number != 2 {
		t.Fatalf("packs = %+v", got.Packs)
	}
	if got.Resume["a.bin"] != 5 {
		t.Fatalf("resume = %v", got.Resume)
	}
}

func TestLoadMissing(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if f == nil || len(f.Packs) != 0 || f.Resume == nil {
		t.Fatalf("missing file result = %+v", f)
	}
}

func TestSaveCreatesDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "state.json")
	if err := Save(path, &File{Packs: []PackRecord{{Number: 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file not created: %v", err)
	}
}
