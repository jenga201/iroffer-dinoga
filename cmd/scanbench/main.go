// Command scanbench generates N test files and times a single packs.Scan over
// them, reporting generation time, scan time, and files/sec. It is used to
// stress-test the packlist indexer (see internal/packs). Files are removed on
// completion unless -keep is passed.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jenga201/dinoga/internal/packs"
)

func main() {
	var (
		n    = flag.Int("n", 1000000, "number of 1 KiB files to generate")
		dir  = flag.String("dir", "/tmp/scanbench", "directory to create files in")
		keep = flag.Bool("keep", false, "do not remove the generated files")
	)
	flag.Parse()

	if err := os.MkdirAll(*dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir:", err)
		os.Exit(1)
	}
	if !*keep {
		defer os.RemoveAll(*dir)
	}

	data := make([]byte, 1024)
	genStart := time.Now()
	for i := 0; i < *n; i++ {
		if err := os.WriteFile(filepath.Join(*dir, fmt.Sprintf("f%07d.bin", i)), data, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "write:", err)
			os.Exit(1)
		}
	}
	genDur := time.Since(genStart)

	scanStart := time.Now()
	pl, err := packs.Scan(*dir)
	scanDur := time.Since(scanStart)
	if err != nil {
		fmt.Fprintln(os.Stderr, "scan:", err)
		os.Exit(1)
	}

	rate := 0.0
	if scanDur.Seconds() > 0 {
		rate = float64(len(pl)) / scanDur.Seconds()
	}
	fmt.Printf("files=%d gen=%v scan=%v rate=%.0f files/s packs=%d\n",
		*n, genDur.Round(time.Millisecond), scanDur.Round(time.Millisecond), rate, len(pl))
}
