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

func TestRoundtripPackManagement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "statefile.json")
	f := &File{
		Packs:    []PackRecord{{Number: 1, Name: "a.bin", Path: "/data/a.bin", Size: 3, Desc: "desc"}},
		Extras:   []PackRecord{{Number: 2, Name: "x.bin", Path: "/extra/x.bin", Size: 7, Desc: "hi"}},
		Excluded: []string{"/data/y.bin"},
		Resume:   map[string]int64{"a.bin": 5},
	}
	if err := Save(path, f); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Extras) != 1 || got.Extras[0].Name != "x.bin" || got.Extras[0].Desc != "hi" {
		t.Fatalf("extras = %+v", got.Extras)
	}
	if len(got.Excluded) != 1 || got.Excluded[0] != "/data/y.bin" {
		t.Fatalf("excluded = %v", got.Excluded)
	}
	if len(got.Packs) != 1 || got.Packs[0].Desc != "desc" {
		t.Fatalf("packs = %+v", got.Packs)
	}
	if got.Resume["a.bin"] != 5 {
		t.Fatalf("resume = %v", got.Resume)
	}
}
