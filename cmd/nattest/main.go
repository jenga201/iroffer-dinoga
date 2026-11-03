// Command nattest is a self-contained NAT-traversal test harness for the
// bounce transport. -mode server runs a TLS server behind the reverse tunnel
// (bot side, outbound only); -mode client dials the bounce's public port and
// verifies a deterministic payload, proving end-to-end TLS + bytes cross the
// relay with no inbound ports on either peer.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"flag"
	"io"
	"log"
	"math/big"
	"net"
	"time"

	"github.com/jenga201/dinoga/internal/bounce"
)

func main() {
	mode := flag.String("mode", "", "server|client")
	tunnel := flag.String("tunnel", "", "bounce tunnel endpoint (server)")
	secret := flag.String("secret", "", "shared secret")
	port := flag.Int("port", 30000, "public bounce port to register (server)")
	target := flag.String("target", "", "bounce public client address (client)")
	size := flag.Int64("size", 4<<20, "payload size in bytes")
	flag.Parse()

	switch *mode {
	case "server":
		runServer(*tunnel, *secret, *port, *size)
	case "client":
		runClient(*target, *size)
	default:
		log.Fatal("nattest: -mode is required (server|client)")
	}
}

func runServer(tunnel, secret string, port int, size int64) {
	var ln net.Listener
	var err error
	for i := 0; i < 30; i++ {
		ln, err = bounce.New(tunnel, secret, port)
		if err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()

	tlsLn := tls.NewListener(ln, &tls.Config{
		Certificates: []tls.Certificate{selfSigned()},
		MinVersion:   tls.VersionTLS13,
	})
	log.Printf("nattest: server registered on bounce port %d", port)
	for {
		c, err := tlsLn.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(60 * time.Second))
			n, err := io.CopyN(c, &pattern{left: size}, size)
			if err != nil {
				log.Printf("nattest: server send: %v", err)
				return
			}
			log.Printf("nattest: server sent %d bytes through the bounce", n)
		}(c)
	}
}

func runClient(target string, size int64) {
	for i := 0; i < 30; i++ {
		conn, err := tls.Dial("tcp", target, &tls.Config{
			InsecureSkipVerify: true, // pin verification covered by unit tests
			MinVersion:         tls.VersionTLS13,
		})
		if err == nil {
			checkConn(conn, size)
			return
		}
		time.Sleep(time.Second)
	}
	log.Fatal("nattest: client could not reach the bounce")
}

func checkConn(conn net.Conn, size int64) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(60 * time.Second))

	buf := make([]byte, 64*1024)
	var total int64
	for total < size {
		n, err := conn.Read(buf)
		for i := 0; i < n; i++ {
			if buf[i] != byte(total+int64(i)) {
				log.Fatalf("nattest: client byte mismatch at offset %d", total+int64(i))
			}
		}
		total += int64(n)
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("nattest: client read: %v", err)
		}
	}
	if total != size {
		log.Fatalf("nattest: client got %d bytes, want %d", total, size)
	}
	log.Printf("nattest: client verified %d bytes through the bounce", total)
}

type pattern struct {
	left int64
	next byte
}

func (p *pattern) Read(b []byte) (int, error) {
	if p.left <= 0 {
		return 0, io.EOF
	}
	n := len(b)
	if int64(n) > p.left {
		n = int(p.left)
	}
	for i := 0; i < n; i++ {
		b[i] = p.next
		p.next++
	}
	p.left -= int64(n)
	return n, nil
}

func selfSigned() tls.Certificate {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "nattest"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, priv.Public(), priv)
	if err != nil {
		panic(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}
}