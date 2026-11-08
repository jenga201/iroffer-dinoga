package irc

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jenga201/dinoga/internal/config"
	"github.com/jenga201/dinoga/internal/packs"
)

func pmWriteFile(t *testing.T, dir, name, data string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func pmNames(c *Client) []string {
	pl := c.Packlist()
	out := make([]string, len(pl))
	for i, p := range pl {
		out[i] = p.Name
	}
	return out
}

func packNames(pl []packs.Pack) []string {
	out := make([]string, len(pl))
	for i, p := range pl {
		out[i] = p.Name
	}
	return out
}

func newPMClient(t *testing.T, dir string) *Client {
	t.Helper()
	return New(config.Config{
		FileDir:   dir,
		StateFile: filepath.Join(t.TempDir(), "state.json"),
	}, nil)
}

func TestAddPack(t *testing.T) {
	dir := t.TempDir()
	pmWriteFile(t, dir, "disk.bin", "disk")
	extra := pmWriteFile(t, t.TempDir(), "extra.bin", "extra")

	c := newPMClient(t, dir)
	if err := c.scan(); err != nil {
		t.Fatal(err)
	}
	if got := pmNames(c); len(got) != 1 || got[0] != "disk.bin" {
		t.Fatalf("initial = %v", got)
	}

	if err := c.addPack(extra); err != nil {
		t.Fatal(err)
	}
	got := pmNames(c)
	if len(got) != 2 || got[0] != "disk.bin" || got[1] != "extra.bin" {
		t.Fatalf("after add = %v", got)
	}
	if p, ok := c.PackByNumber(2); !ok || p.Name != "extra.bin" {
		t.Fatalf("PackByNumber(2) = %q ok=%v", p.Name, ok)
	}
}

func TestRemovePackPersistsAcrossRescan(t *testing.T) {
	dir := t.TempDir()
	pmWriteFile(t, dir, "a.bin", "a")
	pmWriteFile(t, dir, "b.bin", "b")

	c := newPMClient(t, dir)
	if err := c.scan(); err != nil {
		t.Fatal(err)
	}
	if err := c.removePack(1); err != nil { // removes a.bin
		t.Fatal(err)
	}
	if got := pmNames(c); len(got) != 1 || got[0] != "b.bin" {
		t.Fatalf("after remove = %v", got)
	}

	// A rescan must not resurrect the excluded file.
	if err := c.scan(); err != nil {
		t.Fatal(err)
	}
	if got := pmNames(c); len(got) != 1 || got[0] != "b.bin" {
		t.Fatalf("after rescan = %v", got)
	}
}

func TestSetDesc(t *testing.T) {
	dir := t.TempDir()
	pmWriteFile(t, dir, "a.bin", "a")

	c := newPMClient(t, dir)
	if err := c.scan(); err != nil {
		t.Fatal(err)
	}
	if err := c.setDesc(1, "the first pack"); err != nil {
		t.Fatal(err)
	}
	if pl := c.Packlist(); pl[0].Desc != "the first pack" {
		t.Fatalf("desc = %q", pl[0].Desc)
	}
	if err := c.setDesc(1, ""); err != nil {
		t.Fatal(err)
	}
	if pl := c.Packlist(); pl[0].Desc != "" {
		t.Fatalf("desc not cleared = %q", pl[0].Desc)
	}
}

func TestRestartPersistence(t *testing.T) {
	dir := t.TempDir()
	pmWriteFile(t, dir, "disk.bin", "disk")
	extra := pmWriteFile(t, t.TempDir(), "extra.bin", "extra")
	statePath := filepath.Join(t.TempDir(), "state.json")

	c1 := New(config.Config{FileDir: dir, StateFile: statePath}, nil)
	if err := c1.scan(); err != nil {
		t.Fatal(err)
	}
	if err := c1.addPack(extra); err != nil {
		t.Fatal(err)
	}
	if err := c1.setDesc(1, "described"); err != nil { // disk.bin is #1
		t.Fatal(err)
	}

	c2 := New(config.Config{FileDir: dir, StateFile: statePath}, nil)
	c2.loadState()
	if err := c2.scan(); err != nil {
		t.Fatal(err)
	}

	if got := pmNames(c2); len(got) != 2 || got[0] != "disk.bin" || got[1] != "extra.bin" {
		t.Fatalf("after restart = %v", got)
	}
	if pl := c2.Packlist(); pl[0].Desc != "described" {
		t.Fatalf("desc after restart = %q", pl[0].Desc)
	}
}

func TestMergeLockedDeterministic(t *testing.T) {
	c := New(config.Config{FileDir: t.TempDir()}, nil)
	c.mu.Lock()
	c.extras = map[string]packs.Pack{
		"/e/z.bin": {Name: "z.bin", Path: "/e/z.bin"},
		"/e/a.bin": {Name: "a.bin", Path: "/e/a.bin"},
	}
	c.mu.Unlock()

	disk := []packs.Pack{{Name: "m.bin", Path: "/d/m.bin"}}
	got := c.mergeLocked(disk)
	if names := packNames(got); names[0] != "a.bin" || names[1] != "m.bin" || names[2] != "z.bin" {
		t.Fatalf("merge order = %v", names)
	}
	for i, p := range got {
		if p.Number != i+1 {
			t.Fatalf("renumber = %+v", got)
		}
	}
}
