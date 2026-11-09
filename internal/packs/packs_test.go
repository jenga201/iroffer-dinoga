package packs

import (
	"crypto/md5"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestScanNumbersAndSorts(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "b.bin", []byte("b"))
	writeFile(t, dir, "a.bin", []byte("a"))
	writeFile(t, dir, "c.bin", []byte("c"))

	pl, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pl) != 3 {
		t.Fatalf("len = %d, want 3", len(pl))
	}
	if pl[0].Name != "a.bin" || pl[0].Number != 1 {
		t.Fatalf("first = %#v", pl[0])
	}
	if pl[2].Name != "c.bin" || pl[2].Number != 3 {
		t.Fatalf("last = %#v", pl[2])
	}
}

func TestSizeString(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{512, "<1k"},
		{1024, "1K"},
		{939 * 1024 * 1024, "939M"},
		{9 * 1024 * 1024, "9.0M"},
		{1024 * 1024 * 1024, "1.0G"},
		{1181116006, "1.1G"}, // ≈1.1 GiB
	}
	for _, c := range cases {
		if got := SizeString(c.in); got != c.want {
			t.Errorf("SizeString(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNaturalSort(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"file2.bin", "file10.bin", "file1.bin"} {
		writeFile(t, dir, n, []byte(n))
	}
	pl, err := Scan(dir, WithNaturalSort())
	if err != nil {
		t.Fatal(err)
	}
	got := []string{pl[0].Name, pl[1].Name, pl[2].Name}
	want := []string{"file1.bin", "file2.bin", "file10.bin"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("natural order = %v, want %v", got, want)
		}
	}
}

func TestLessNaturalOrder(t *testing.T) {
	if !Less("file1.bin", "file2.bin") {
		t.Fatal("file1 should sort before file2")
	}
	if !Less("file2.bin", "file10.bin") {
		t.Fatal("file2 should sort before file10 (numeric run)")
	}
	if Less("file10.bin", "file2.bin") {
		t.Fatal("file10 must not sort before file2")
	}
	// Case-insensitive with byte-order tiebreak, mirroring naturalLess.
	if !Less("A.bin", "a.bin") {
		t.Fatal("uppercase A should sort before lowercase a on tie")
	}
}

func TestRecurseAndFilter(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sub"), 0o700)
	writeFile(t, dir, "top.txt", []byte("top"))
	writeFile(t, filepath.Join(dir, "sub"), "sub.txt", []byte("sub"))
	writeFile(t, filepath.Join(dir, "sub"), "sub.bin", []byte("bin"))

	// Non-recursive: only the top file.
	pl, _ := Scan(dir)
	if len(pl) != 1 || pl[0].Name != "top.txt" {
		t.Fatalf("non-recursive = %v", names(pl))
	}
	// Recursive: 3 files.
	pl, _ = Scan(dir, WithRecurse())
	if len(pl) != 3 {
		t.Fatalf("recurse = %v", names(pl))
	}
	// Recursive + include only *.txt.
	pl, _ = Scan(dir, WithRecurse(), WithMatch("*.txt"))
	if len(pl) != 2 {
		t.Fatalf("match = %v", names(pl))
	}
	// Exclude *.bin.
	pl, _ = Scan(dir, WithRecurse(), WithExclude("*.bin"))
	if len(pl) != 2 {
		t.Fatalf("exclude = %v", names(pl))
	}
}

func TestMinSizeAndDedup(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "small.bin", []byte("x"))
	writeFile(t, dir, "big.bin", []byte("xx"))
	pl, _ := Scan(dir, WithMinSize(2))
	if len(pl) != 1 || pl[0].Name != "big.bin" {
		t.Fatalf("minSize = %v", names(pl))
	}

	// De-dup by (name,size): same name+size in two dirs collapses to one.
	sub := filepath.Join(dir, "sub")
	os.MkdirAll(sub, 0o700)
	writeFile(t, dir, "dup.bin", []byte("x"))
	writeFile(t, sub, "dup.bin", []byte("x"))
	pl, _ = Scan(dir, WithRecurse(), WithDedup())
	if len(pl) != 3 { // small.bin, big.bin, dup.bin (deduped), + dup.bin? no: small+big+one dup = 3
		t.Fatalf("dedup = %v", names(pl))
	}
}

func TestLazyHashes(t *testing.T) {
	dir := t.TempDir()
	data := []byte("hello world")
	writeFile(t, dir, "a.bin", data)

	pl, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pl) != 1 {
		t.Fatalf("len = %d", len(pl))
	}
	pk := pl[0]

	md5 := mustMD5(data)
	if got := pk.MD5(); got != md5 {
		t.Fatalf("MD5 = %q, want %q", got, md5)
	}
	if got := pk.CRC32(); got != crc32.ChecksumIEEE(data) {
		t.Fatalf("CRC32 = %d, want %d", got, crc32.ChecksumIEEE(data))
	}

	// Hash batch form on a fresh scan.
	pl2, _ := Scan(dir)
	if err := Hash(pl2); err != nil {
		t.Fatal(err)
	}
	if pl2[0].MD5() != md5 {
		t.Fatalf("batch MD5 mismatch")
	}
}

func TestModTimeSet(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.bin", []byte("x"))
	pl, _ := Scan(dir)
	if pl[0].ModTime.IsZero() {
		t.Fatal("ModTime is zero")
	}
	if pl[0].Path != filepath.Join(dir, "a.bin") {
		t.Fatalf("Path = %q", pl[0].Path)
	}
}

func names(pl []Pack) []string {
	out := make([]string, len(pl))
	for i := range pl {
		out[i] = pl[i].Name
	}
	return out
}

func mustMD5(b []byte) string {
	return fmt.Sprintf("%x", md5.Sum(b))
}

// BenchmarkScan measures a default (non-hashing) scan of N files. Set
// PACKS_BENCH_N to control the count (default 5000). Files are 1 KiB.
func BenchmarkScan(b *testing.B) {
	n := 5000
	if v := os.Getenv("PACKS_BENCH_N"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			n = parsed
		}
	}
	dir := b.TempDir()
	data := make([]byte, 1024)
	for i := 0; i < n; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file%07d.bin", i)), data, 0o600); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pl, err := Scan(dir)
		if err != nil {
			b.Fatal(err)
		}
		if len(pl) != n {
			b.Fatalf("got %d packs, want %d", len(pl), n)
		}
	}
}

func TestFromPath(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "a.bin", []byte("hello"))
	pk, err := FromPath(p)
	if err != nil {
		t.Fatal(err)
	}
	if pk.Name != "a.bin" || pk.Path != p || pk.Size != 5 || pk.ModTime.IsZero() {
		t.Fatalf("pack = %#v", pk)
	}

	if _, err := FromPath(filepath.Join(dir, "missing.bin")); err == nil {
		t.Fatal("FromPath should error for a missing file")
	}
	if _, err := FromPath(dir); err == nil {
		t.Fatal("FromPath should error for a directory")
	}
}
