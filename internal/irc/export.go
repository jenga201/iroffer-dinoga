// Pack list export: the optional plain-text (xdcclistfile) and XML
// (xdccxmlfile) exports of the current listing, written after every persist.
package irc

import (
	"encoding/xml"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/jenga201/dinoga/internal/packs"
)

// exportPacklists writes the configured xdcclistfile / xdccxmlfile (if any)
// from the current pack list. It is best-effort: failures are logged, not fatal.
func (c *Client) exportPacklists() {
	if c.cfg.XDCCListFile == "" && c.cfg.XDCCXMLFile == "" {
		return
	}
	pl := c.Packlist()
	if c.cfg.XDCCListFile != "" {
		if err := writePackListText(c.cfg.XDCCListFile, pl); err != nil {
			log.Printf("xdcclistfile: %v", err)
		}
	}
	if c.cfg.XDCCXMLFile != "" {
		if err := writePackListXML(c.cfg.XDCCXMLFile, pl); err != nil {
			log.Printf("xdccxmlfile: %v", err)
		}
	}
}

// writePackListText writes one pack per line as "#<num> <size> <name>", with
// the description (when set) on a following indented line.
func writePackListText(path string, pl []packs.Pack) error {
	var sb strings.Builder
	for _, p := range pl {
		fmt.Fprintf(&sb, "#%d %d %s\n", p.Number, p.Size, p.Name)
		if p.Desc != "" {
			fmt.Fprintf(&sb, "%s\n", p.Desc)
		}
	}
	return writeFileAtomic(path, []byte(sb.String()))
}

// xmlPacklist mirrors the pack list as a small XML document.
type xmlPacklist struct {
	XMLName xml.Name  `xml:"packlist"`
	Packs   []xmlPack `xml:"pack"`
}

type xmlPack struct {
	Number int    `xml:"number,attr"`
	Size   int64  `xml:"size,attr"`
	Name   string `xml:"name"`
	Desc   string `xml:"desc,omitempty"`
}

func writePackListXML(path string, pl []packs.Pack) error {
	doc := xmlPacklist{Packs: make([]xmlPack, len(pl))}
	for i, p := range pl {
		doc.Packs[i] = xmlPack{Number: p.Number, Size: p.Size, Name: p.Name, Desc: p.Desc}
	}
	// MarshalIndent bytes are in source order; the listing is already numbered.
	b, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	out := make([]byte, 0, len(xml.Header)+len(b)+1)
	out = append(out, xml.Header...)
	out = append(out, b...)
	out = append(out, '\n')
	return writeFileAtomic(path, out)
}

// writeFileAtomic writes data to path via a temp file + rename, creating parent
// directories as needed, so a crash mid-write never leaves a truncated export.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, ".export-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}