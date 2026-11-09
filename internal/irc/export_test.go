package irc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jenga201/dinoga/internal/config"
	"github.com/jenga201/dinoga/internal/packs"
)

func TestWritePackListText(t *testing.T) {
	pl := []packs.Pack{
		{Number: 1, Name: "a.bin", Size: 10, Desc: "first pack"},
		{Number: 2, Name: "b.bin", Size: 20},
	}
	path := filepath.Join(t.TempDir(), "packlist.txt")
	if err := writePackListText(path, pl); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, "#1 10 a.bin\nfirst pack\n") {
		t.Fatalf("text export missing desc line:\n%s", got)
	}
	if !strings.Contains(got, "#2 20 b.bin\n") {
		t.Fatalf("text export missing second pack:\n%s", got)
	}
}

func TestWritePackListXML(t *testing.T) {
	pl := []packs.Pack{
		{Number: 1, Name: "a&b.bin", Size: 10, Desc: "x < y"},
	}
	path := filepath.Join(t.TempDir(), "packlist.xml")
	if err := writePackListXML(path, pl); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.HasPrefix(got, "<?xml") {
		t.Fatalf("XML export missing header:\n%s", got)
	}
	if !strings.Contains(got, "a&amp;b.bin") || !strings.Contains(got, "x &lt; y") {
		t.Fatalf("XML export not escaped:\n%s", got)
	}
}

func TestExportPacklistsWritesBothFiles(t *testing.T) {
	dir := t.TempDir()
	txt := filepath.Join(dir, "packs.txt")
	xml := filepath.Join(dir, "packs.xml")

	c := &Client{
		cfg:      config.Config{XDCCListFile: txt, XDCCXMLFile: xml},
		packlist: []packs.Pack{{Number: 1, Name: "a.bin", Size: 5}},
	}
	c.exportPacklists()

	for _, p := range []string{txt, xml} {
		if b, err := os.ReadFile(p); err != nil || len(b) == 0 {
			t.Fatalf("expected non-empty export at %s (err=%v, len=%d)", p, err, len(b))
		}
	}
}