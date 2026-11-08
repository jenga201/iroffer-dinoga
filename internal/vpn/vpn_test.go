package vpn

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadForwardedPort(t *testing.T) {
	p := write(t, "54321\n")
	port, err := ReadForwardedPort(p)
	if err != nil {
		t.Fatal(err)
	}
	if port != 54321 {
		t.Fatalf("port = %d, want 54321", port)
	}
}

func TestReadForwardedPortInvalid(t *testing.T) {
	p := write(t, "notaport\n")
	if _, err := ReadForwardedPort(p); err == nil {
		t.Fatal("expected error for invalid port")
	}
}

func TestPeerEndpointIP(t *testing.T) {
	p := write(t, `[Interface]
PrivateKey = x
[Peer]
PublicKey = y
AllowedIPs = 0.0.0.0/0
Endpoint = 198.51.100.42:1337
`)
	ip, err := PeerEndpointIP(p)
	if err != nil {
		t.Fatal(err)
	}
	if ip != "198.51.100.42" {
		t.Fatalf("ip = %q, want 198.51.100.42", ip)
	}
}

func TestPeerEndpointIPv6(t *testing.T) {
	p := write(t, "[Peer]\nEndpoint = [2001:db8::1]:51820\n")
	ip, err := PeerEndpointIP(p)
	if err != nil {
		t.Fatal(err)
	}
	if ip != "2001:db8::1" {
		t.Fatalf("ip = %q", ip)
	}
}

func TestPeerEndpointMissing(t *testing.T) {
	p := write(t, "[Interface]\nPrivateKey = x\n")
	if _, err := PeerEndpointIP(p); err == nil {
		t.Fatal("expected error for missing endpoint")
	}
}

func TestEchoIP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("203.0.113.9\n"))
	}))
	defer srv.Close()

	ip, err := EchoIP(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if ip != "203.0.113.9" {
		t.Fatalf("ip = %q", ip)
	}
}

func TestDiscoverIPFallsBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("203.0.113.9"))
	}))
	defer srv.Close()

	// No wg conf: falls back to the echo.
	ip, err := DiscoverIP("/nonexistent/wg.conf", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if ip != "203.0.113.9" {
		t.Fatalf("ip = %q", ip)
	}
}
