package bounce

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"io"
	"math/big"
	"net"
	"testing"
	"time"
)

func startRelay(t *testing.T, secret string) (tunnelAddr string, clientAddr net.Addr) {
	t.Helper()
	r := NewRelay(secret)
	ta, err := r.ServeTunnel("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ca, err := r.ServeClient("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return ta.String(), ca
}

func TestRelayEchoRoundtrip(t *testing.T) {
	tunnelAddr, clientAddr := startRelay(t, "sekret")
	clientPort := clientAddr.(*net.TCPAddr).Port

	botLn, err := New(tunnelAddr, "sekret", clientPort)
	if err != nil {
		t.Fatal(err)
	}
	defer botLn.Close()

	go func() {
		for {
			c, err := botLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c) // echo
			}(c)
		}
	}()

	d := net.Dialer{Timeout: 5 * time.Second}
	c, err := d.Dial("tcp", clientAddr.String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	msg := []byte("hello through the bounce")
	if _, err := c.Write(msg); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != string(msg) {
		t.Fatalf("echo = %q, want %q", buf, msg)
	}
}

func TestTLSThroughRelay(t *testing.T) {
	tunnelAddr, clientAddr := startRelay(t, "secret2")
	clientPort := clientAddr.(*net.TCPAddr).Port

	botLn, err := New(tunnelAddr, "secret2", clientPort)
	if err != nil {
		t.Fatal(err)
	}
	defer botLn.Close()

	cert := selfSigned(t)
	server := tls.NewListener(botLn, &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	})
	go func() {
		for {
			c, err := server.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(c)
		}
	}()

	conn, err := tls.Dial("tcp", clientAddr.String(), &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// The client must see the bot's own certificate end-to-end (the relay must
	// not have substituted or terminated TLS).
	leaf := conn.ConnectionState().PeerCertificates[0]
	if spkiPin(leaf) != spkiPin(cert.Leaf) {
		t.Fatal("relay altered the server certificate")
	}

	msg := []byte("tls bytes via bounce")
	if _, err := conn.Write(msg); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != string(msg) {
		t.Fatalf("echo = %q, want %q", buf, msg)
	}
}

func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "bounce-test"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, priv.Public(), priv)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv, Leaf: leaf}
}

func spkiPin(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}