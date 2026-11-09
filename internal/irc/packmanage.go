// Runtime pack management: the admin `addpack`, `removepack`, and `chdesc`
// commands plus the state-persistence glue that keeps runtime mutations (added
// packs, descriptions, and removals) intact across rescans and restarts.
package irc

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jenga201/dinoga/internal/admin"
	"github.com/jenga201/dinoga/internal/dcc"
	"github.com/jenga201/dinoga/internal/packs"
	"github.com/jenga201/dinoga/internal/state"
)

// loadState restores the runtime pack mutations (extras, descriptions, and
// exclusions) persisted in the state file. It is called once at startup, before
// the first scan merges them with the disk listing.
func (c *Client) loadState() {
	if c.cfg.StateFile == "" {
		return
	}
	f, err := state.Load(c.cfg.StateFile)
	if err != nil {
		log.Printf("state load: %v", err)
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, rec := range f.Extras {
		p, perr := packs.FromPath(rec.Path)
		if perr != nil {
			continue // the added file no longer exists
		}
		p.Desc = rec.Desc
		if c.extras == nil {
			c.extras = map[string]packs.Pack{}
		}
		c.extras[p.Path] = p
	}
	for _, rec := range f.Packs {
		if rec.Desc == "" {
			continue
		}
		if c.descs == nil {
			c.descs = map[string]string{}
		}
		c.descs[rec.Path] = rec.Desc
	}
	for _, path := range f.Excluded {
		if c.excluded == nil {
			c.excluded = map[string]bool{}
		}
		c.excluded[path] = true
	}
	if f.Resume != nil {
		c.resume = f.Resume
	}
	c.totalSent = f.TotalSent
	if c.gets == nil {
		c.gets = map[string]int64{}
	}
	for _, rec := range append(f.Packs, f.Extras...) {
		if rec.Gets > 0 {
			c.gets[rec.Path] = rec.Gets
		}
	}
}

// persist snapshots the current pack list (plus extras, descriptions, resume
// offsets, and exclusions) to the state file.
func (c *Client) persist() {
	if c.cfg.StateFile == "" {
		return
	}
	c.backupState()

	c.mu.RLock()
	packsSlice := recordPackList(c.packlist)
	extras := make([]state.PackRecord, 0, len(c.extras))
	for _, p := range c.extras {
		extras = append(extras, packRecord(p))
	}
	excluded := make([]string, 0, len(c.excluded))
	for path := range c.excluded {
		excluded = append(excluded, path)
	}
	resume := make(map[string]int64, len(c.resume))
	for path, off := range c.resume {
		resume[path] = off
	}
	sort.Strings(excluded)
	c.mu.RUnlock()

	if err := state.Save(c.cfg.StateFile, &state.File{
		Packs:     packsSlice,
		Extras:    extras,
		Excluded:  excluded,
		Resume:    resume,
		TotalSent: c.totalSent,
	}); err != nil {
		log.Printf("state save: %v", err)
	}

	c.exportPacklists()
}

// backupState rolls the current state file to old_statefile (if configured)
// before a new state is written, so the previous-known-good state survives.
func (c *Client) backupState() {
	if c.cfg.OldStateFile == "" || c.cfg.OldStateFile == c.cfg.StateFile {
		return
	}
	if _, err := os.Stat(c.cfg.StateFile); err == nil {
		if err := os.Rename(c.cfg.StateFile, c.cfg.OldStateFile); err != nil {
			log.Printf("old_statefile: %v", err)
		}
	}
}

// mergeLocked folds the disk-scanned list with the runtime extras, descriptions,
// and exclusions, then sorts and renumbers the result. It must be called with
// c.mu held.
func (c *Client) mergeLocked(disk []packs.Pack) []packs.Pack {
	byPath := make(map[string]packs.Pack, len(disk)+len(c.extras))
	for _, p := range disk {
		if c.excluded[p.Path] {
			continue
		}
		if d := c.descs[p.Path]; d != "" {
			p.Desc = d
		}
		p.Gets = c.gets[p.Path]
		byPath[p.Path] = p
	}
	for path, p := range c.extras {
		if c.excluded[path] {
			continue
		}
		if existing, ok := byPath[path]; ok {
			if existing.Desc == "" {
				existing.Desc = p.Desc
			}
			existing.Gets = c.gets[path]
			byPath[path] = existing
			continue
		}
		if d := c.descs[path]; d != "" {
			p.Desc = d
		}
		p.Gets = c.gets[path]
		byPath[path] = p
	}

	out := make([]packs.Pack, 0, len(byPath))
	for _, p := range byPath {
		out = append(out, p)
	}
	natural := c.cfg.NaturalSort
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			if natural {
				return packs.Less(out[i].Name, out[j].Name)
			}
			return out[i].Name < out[j].Name
		}
		return out[i].Path < out[j].Path
	})
	renumberPacks(out)
	return out
}

// addPack stats path and adds it to the pack list as a runtime extra. It is
// idempotent for a path that is already listed.
func (c *Client) addPack(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	p, err := packs.FromPath(abs)
	if err != nil {
		return err
	}

	c.mu.Lock()
	for i := range c.packlist {
		if c.packlist[i].Path == p.Path {
			c.mu.Unlock()
			return nil // already listed
		}
	}
	delete(c.excluded, p.Path)
	if d := c.descs[p.Path]; d != "" {
		p.Desc = d
	}
	if c.extras == nil {
		c.extras = map[string]packs.Pack{}
	}
	c.extras[p.Path] = p
	c.packlist = append(c.packlist, p)
	renumberPacks(c.packlist)
	c.mu.Unlock()

	c.persist()
	return nil
}

// removePack removes pack number n (1-based) from the list and records its path
// as excluded so a later rescan does not re-add a disk file.
func (c *Client) removePack(n int) error {
	c.mu.Lock()
	if n < 1 || n > len(c.packlist) {
		c.mu.Unlock()
		return fmt.Errorf("no such pack: %d", n)
	}
	p := c.packlist[n-1]
	if c.excluded == nil {
		c.excluded = map[string]bool{}
	}
	c.excluded[p.Path] = true
	delete(c.extras, p.Path)
	delete(c.descs, p.Path)
	c.packlist = append(c.packlist[:n-1], c.packlist[n:]...)
	renumberPacks(c.packlist)
	c.mu.Unlock()

	c.persist()
	return nil
}

// setDesc sets (or, for an empty desc, clears) the description of pack n.
func (c *Client) setDesc(n int, desc string) error {
	c.mu.Lock()
	if n < 1 || n > len(c.packlist) {
		c.mu.Unlock()
		return fmt.Errorf("no such pack: %d", n)
	}
	p := c.packlist[n-1]
	if desc == "" {
		delete(c.descs, p.Path)
	} else {
		if c.descs == nil {
			c.descs = map[string]string{}
		}
		c.descs[p.Path] = desc
	}
	c.packlist[n-1].Desc = desc
	if e, ok := c.extras[p.Path]; ok {
		e.Desc = desc
		c.extras[p.Path] = e
	}
	c.mu.Unlock()

	c.persist()
	return nil
}

// registerAdminCommands adds the runtime pack-management commands to the admin
// server. The generic admin package has no pack concept, so these live here.
func (c *Client) registerAdminCommands(s *admin.Server) {
	_ = s.Register(admin.Command{
		Name:  "addpack",
		Usage: "addpack <path>",
		Help:  "Add a file to the packlist.",
		Run: func(out io.Writer, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("usage: addpack <path>")
			}
			if err := c.addPack(args[0]); err != nil {
				return err
			}
			fmt.Fprintln(out, "added")
			return nil
		},
	})
	_ = s.Register(admin.Command{
		Name:  "removepack",
		Usage: "removepack <n>",
		Help:  "Remove pack number <n> from the packlist.",
		Run: func(out io.Writer, args []string) error {
			n, err := parsePackNum(args)
			if err != nil {
				return err
			}
			if err := c.removePack(n); err != nil {
				return err
			}
			fmt.Fprintln(out, "removed")
			return nil
		},
	})
	_ = s.Register(admin.Command{
		Name:  "chdesc",
		Usage: "chdesc <n> [description...]",
		Help:  "Set (or, with no description, clear) pack <n>'s description.",
		Run: func(out io.Writer, args []string) error {
			n, err := parsePackNum(args)
			if err != nil {
				return err
			}
			if err := c.setDesc(n, strings.Join(args[1:], " ")); err != nil {
				return err
			}
			fmt.Fprintln(out, "updated")
			return nil
		},
	})
	_ = s.Register(admin.Command{
		Name:  "regenkey",
		Usage: "regenkey",
		Help:  "Regenerate the SXDCC identity and print the new SPKI pin.",
		Run: func(out io.Writer, args []string) error {
			if c.sxdcc == nil {
				return fmt.Errorf("sxdcc is not enabled")
			}
			pin, err := c.sxdcc.RegenerateKey()
			if err != nil {
				return err
			}
			if ip, port := c.advertise(); ip != "" && port > 0 {
				if ipInt, e := dcc.IPToInt(ip); e == nil && c.sdcc != nil {
					c.sdcc.SetAdvertise(ipInt, port, pin)
				}
			}
			fmt.Fprintf(out, "pin: %s\n", pin)
			c.Logf("sxdcc: regenerated identity, pin %s", pin)
			return nil
		},
	})
}

func parsePackNum(args []string) (int, error) {
	if len(args) < 1 {
		return 0, fmt.Errorf("usage expects a pack number")
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 {
		return 0, fmt.Errorf("invalid pack number: %q", args[0])
	}
	return n, nil
}

func renumberPacks(pl []packs.Pack) {
	for i := range pl {
		pl[i].Number = i + 1
	}
}

func packRecord(p packs.Pack) state.PackRecord {
	return state.PackRecord{
		Number: p.Number,
		Name:   p.Name,
		Path:   p.Path,
		Size:   p.Size,
		Desc:   p.Desc,
		Gets:   p.Gets,
	}
}

func recordPackList(pl []packs.Pack) []state.PackRecord {
	out := make([]state.PackRecord, len(pl))
	for i, p := range pl {
		out[i] = packRecord(p)
	}
	return out
}
