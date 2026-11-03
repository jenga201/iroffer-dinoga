package web_test

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jenga201/dinoga/internal/packs"
	"github.com/jenga201/dinoga/internal/web"
)

// testPacks writes three small files into a temp dir and returns a matching
// pack listing plus the packList closure the handler expects.
func testPacks(t *testing.T) ([]packs.Pack, func() []packs.Pack) {
	t.Helper()
	dir := t.TempDir()

	names := []string{"alpha.txt", "beta.txt", "gamma.md"}
	contents := []string{"hello\n", "world\n", "foo bar baz\n"}

	var list []packs.Pack
	for i, name := range names {
		full := filepath.Join(dir, name)
		if err := os.WriteFile(full, []byte(contents[i]), 0o644); err != nil {
			t.Fatal(err)
		}
		list = append(list, packs.Pack{
			Number: i + 1,
			Name:   name,
			Path:   full,
			Size:   int64(len(contents[i])),
		})
	}
	return list, func() []packs.Pack { return list }
}

func do(h http.Handler, method, target, remote string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	if remote != "" {
		req.RemoteAddr = remote
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestIndex(t *testing.T) {
	_, pl := testPacks(t)
	h := web.New(pl, web.Config{})

	rr := do(h, http.MethodGet, "/", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Pack list") {
		t.Errorf("body is missing the heading")
	}
	for _, name := range []string{"alpha.txt", "beta.txt", "gamma.md"} {
		if !strings.Contains(body, name) {
			t.Errorf("body is missing %q", name)
		}
	}
}

func TestSearch(t *testing.T) {
	_, pl := testPacks(t)
	h := web.New(pl, web.Config{})

	rr := do(h, http.MethodGet, "/search?q=ALPHA", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "alpha.txt") {
		t.Errorf("search forgot the matching pack")
	}
	for _, name := range []string{"beta.txt", "gamma.md"} {
		if strings.Contains(body, name) {
			t.Errorf("search leaked unrelated pack %q", name)
		}
	}
}

func TestJSON(t *testing.T) {
	_, pl := testPacks(t)
	h := web.New(pl, web.Config{})

	rr := do(h, http.MethodGet, "/list.json", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got []struct {
		Number int    `json:"number"`
		Name   string `json:"name"`
		Size   int64  `json:"size"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3: %s", len(got), rr.Body.String())
	}
	if got[0].Number != 1 || got[0].Name != "alpha.txt" || got[0].Size != int64(len("hello\n")) {
		t.Errorf("unexpected first object: %+v", got[0])
	}
}

func TestMD5(t *testing.T) {
	_, pl := testPacks(t)
	h := web.New(pl, web.Config{})

	rr := do(h, http.MethodGet, "/md5/alpha.txt", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	sum := md5.Sum([]byte("hello\n"))
	want := hex.EncodeToString(sum[:])
	if got := strings.TrimSpace(rr.Body.String()); got != want {
		t.Errorf("md5 = %q, want %q", got, want)
	}
}

func TestMD5NotFound(t *testing.T) {
	_, pl := testPacks(t)
	h := web.New(pl, web.Config{})

	rr := do(h, http.MethodGet, "/md5/nope.txt", "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestSFV(t *testing.T) {
	_, pl := testPacks(t)
	h := web.New(pl, web.Config{})

	rr := do(h, http.MethodGet, "/sfv", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	lines := strings.Split(strings.TrimRight(rr.Body.String(), "\r\n"), "\r\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3: %q", len(lines), rr.Body.String())
	}
	first := strings.Fields(lines[0])
	if len(first) != 2 {
		t.Fatalf("bad sfv line %q", lines[0])
	}
	if first[0] != "alpha.txt" {
		t.Errorf("name = %q, want alpha.txt", first[0])
	}
	wantCRC := fmt.Sprintf("%08X", crc32.ChecksumIEEE([]byte("hello\n")))
	if first[1] != wantCRC {
		t.Errorf("crc = %q, want %q", first[1], wantCRC)
	}
}

func TestAccessControl(t *testing.T) {
	_, pl := testPacks(t)

	t.Run("deny", func(t *testing.T) {
		deny, err := web.ParseCIDRs([]string{"192.0.2.0/24"})
		if err != nil {
			t.Fatal(err)
		}
		h := web.New(pl, web.Config{Deny: deny})
		rr := do(h, http.MethodGet, "/", "192.0.2.10:1234")
		if rr.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rr.Code)
		}
	})

	t.Run("allow-outside", func(t *testing.T) {
		allow, err := web.ParseCIDRs([]string{"10.0.0.0/8"})
		if err != nil {
			t.Fatal(err)
		}
		h := web.New(pl, web.Config{Allow: allow})
		rr := do(h, http.MethodGet, "/", "192.0.2.10:1234")
		if rr.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rr.Code)
		}
	})

	t.Run("allow-inside", func(t *testing.T) {
		allow, err := web.ParseCIDRs([]string{"10.0.0.0/8"})
		if err != nil {
			t.Fatal(err)
		}
		h := web.New(pl, web.Config{Allow: allow})
		rr := do(h, http.MethodGet, "/", "10.1.2.3:5678")
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rr.Code)
		}
	})
}
