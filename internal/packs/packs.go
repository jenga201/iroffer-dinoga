// Package packs lists the files offered over XDCC.
//
// Scan is deliberately O(number of files): it never hashes file contents.
// MD5 and CRC32 checksums are computed only on demand (via the MD5/CRC32
// methods or the batch Hash function) and memoized per pack.
package packs

import (
	"crypto/md5"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

var crcTable = crc32.MakeTable(crc32.IEEE)

// Pack is a single file offered over XDCC.
//
// Number is the 1-based position in the listing (assigned by Scan). MD5 and
// CRC32 are not part of the base scan: they are computed lazily and memoized.
// Use p.MD5(), p.CRC32(), p.Hash(), or the package-level Hash to populate
// them. The checksums are memoized through an internal pointer, so value
// copies made after a pack has been hashed share the cached result.
type Pack struct {
	Number  int
	Name    string
	Path    string
	Size    int64
	ModTime time.Time

	hash *hashState // memoized checksums; shared by copies of this pack
}

type hashState struct {
	once  sync.Once
	md5   string
	crc32 uint32
	err   error
}

// Option customises Scan.
type Option func(*options)

type options struct {
	recurse     bool
	match       []string // include only names matching one of these globs
	exclude     []string // skip names matching one of these globs
	minSize     int64    // skip files smaller than this (bytes)
	naturalSort bool
	dedup       bool
	hashes      bool
}

// WithRecurse descends into subdirectories recursively.
func WithRecurse() Option {
	return func(o *options) { o.recurse = true }
}

// WithMatch keeps only files whose base name matches at least one of the
// given glob patterns (using path.Match). Multiple calls are combined (OR).
func WithMatch(patterns ...string) Option {
	return func(o *options) { o.match = append(o.match, patterns...) }
}

// WithExclude skips files whose base name matches at least one of the given
// glob patterns (using path.Match). Multiple calls are combined (OR).
func WithExclude(patterns ...string) Option {
	return func(o *options) { o.exclude = append(o.exclude, patterns...) }
}

// WithMinSize skips files smaller than size bytes.
func WithMinSize(size int64) Option {
	return func(o *options) { o.minSize = size }
}

// WithNaturalSort orders the listing with natural (strnatcmp-style) string
// ordering instead of plain lexicographic ordering.
func WithNaturalSort() Option {
	return func(o *options) { o.naturalSort = true }
}

// WithDedup removes duplicate files that share the same name and size,
// keeping the first occurrence in sorted order (iroffer-style noduplicatefiles).
func WithDedup() Option {
	return func(o *options) { o.dedup = true }
}

// WithHashes computes MD5 and CRC32 for every file as part of the scan. This
// is a compatibility option for callers that still need eager hashing; it
// reintroduces O(total bytes) work and should not be used for large listings.
func WithHashes() Option {
	return func(o *options) { o.hashes = true }
}

// Scan lists the regular files under dir and numbers them consecutively.
// The default scan is O(number of files) and never reads file contents:
// MD5/CRC32 are only computed when explicitly requested. opts may be passed
// to enable recursion, filtering, natural sorting, de-duplication, or eager
// hashing.
func Scan(dir string, opts ...Option) ([]Pack, error) {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	var out []Pack
	if o.recurse {
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil || !info.Mode().IsRegular() {
				return nil
			}
			if !accept(o, d.Name(), info) {
				return nil
			}
			out = append(out, newPack(p, d.Name(), info))
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		out = make([]Pack, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			info, err := e.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if !accept(o, e.Name(), info) {
				continue
			}
			out = append(out, newPack(filepath.Join(dir, e.Name()), e.Name(), info))
		}
	}

	sortPacklist(out, o.naturalSort)
	if o.dedup {
		out = dedup(out)
	}
	for i := range out {
		out[i].Number = i + 1
	}

	if o.hashes {
		// Best-effort, matching the old tolerant behaviour: unreadable files
		// simply end up with an empty MD5 and CRC32 of 0.
		for i := range out {
			_ = out[i].Hash()
		}
	}
	return out, nil
}

func newPack(path, name string, info fs.FileInfo) Pack {
	return Pack{
		Name:    name,
		Path:    path,
		Size:    info.Size(),
		ModTime: info.ModTime(),
		hash:    &hashState{},
	}
}

func accept(o *options, name string, info fs.FileInfo) bool {
	if info.Size() < o.minSize {
		return false
	}
	if len(o.match) > 0 && !matchesAny(o.match, name) {
		return false
	}
	if len(o.exclude) > 0 && matchesAny(o.exclude, name) {
		return false
	}
	return true
}

func matchesAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if ok, err := path.Match(p, name); err == nil && ok {
			return true
		}
	}
	return false
}

func sortPacklist(out []Pack, natural bool) {
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].Path < out[j].Path
		}
		if natural {
			return naturalLess(out[i].Name, out[j].Name)
		}
		return out[i].Name < out[j].Name
	})
}

type dedupKey struct {
	name string
	size int64
}

func dedup(out []Pack) []Pack {
	seen := make(map[dedupKey]struct{}, len(out))
	n := 0
	for _, p := range out {
		key := dedupKey{name: p.Name, size: p.Size}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out[n] = p
		n++
	}
	return out[:n]
}

// Hash computes (once) and memoizes the MD5 and CRC32 of this pack.
func (p *Pack) Hash() error {
	h := p.hash
	if h == nil {
		h = &hashState{}
		p.hash = h
	}
	h.once.Do(func() {
		h.md5, h.crc32, h.err = fileHashes(p.Path)
	})
	return h.err
}

// MD5 returns the file's MD5 digest (lowercase hex), computing and memoizing
// it on first use. It returns "" if the file cannot be read.
func (p *Pack) MD5() string {
	if p.Hash() != nil {
		return ""
	}
	return p.hash.md5
}

// CRC32 returns the file's CRC-32 (IEEE) checksum, computing and memoizing it
// on first use. It returns 0 if the file cannot be read.
func (p *Pack) CRC32() uint32 {
	if p.Hash() != nil {
		return 0
	}
	return p.hash.crc32
}

// Hash computes and memoizes the MD5 and CRC32 of every pack, stopping at the
// first error. This is the explicit batch form of the lazy per-pack methods.
func Hash(packs []Pack) error {
	for i := range packs {
		if err := packs[i].Hash(); err != nil {
			return err
		}
	}
	return nil
}

func fileHashes(path string) (string, uint32, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	md5h := md5.New()
	crc32h := crc32.New(crcTable)
	if _, err := io.Copy(io.MultiWriter(md5h, crc32h), f); err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("%x", md5h.Sum(nil)), crc32h.Sum32(), nil
}

// naturalLess implements a strnatcmp-style ordering: runs of digits compare
// numerically (leading zeros ignored, with fewer leading zeros first on ties),
// and everything else compares case-insensitively with a byte-order tiebreak.
func naturalLess(a, b string) bool {
	ai, bi := 0, 0
	for {
		if ai >= len(a) {
			return bi < len(b)
		}
		if bi >= len(b) {
			return false
		}

		ca, cb := a[ai], b[bi]
		if isDigit(ca) && isDigit(cb) {
			az, bz := 0, 0
			for ai < len(a) && a[ai] == '0' {
				ai++
				az++
			}
			for bi < len(b) && b[bi] == '0' {
				bi++
				bz++
			}
			as, bs := ai, bi
			for ai < len(a) && isDigit(a[ai]) {
				ai++
			}
			for bi < len(b) && isDigit(b[bi]) {
				bi++
			}
			aLen, bLen := ai-as, bi-bs
			if aLen != bLen {
				return aLen < bLen
			}
			if a[as:ai] != b[bs:bi] {
				return a[as:ai] < b[bs:bi]
			}
			if az != bz {
				return az < bz
			}
			continue
		}

		la, lb := asciiLower(ca), asciiLower(cb)
		if la != lb {
			return la < lb
		}
		if ca != cb {
			return ca < cb
		}
		ai++
		bi++
	}
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func asciiLower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}
