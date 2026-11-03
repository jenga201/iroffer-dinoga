// Package sxdcc implements the server (bot) side of SXDCC, the secure TLS 1.3
// successor to XDCC/DCC described in IRC/dcc-protocol/SXDCC.md.
//
// It provides: self-signed Ed25519 identity with an IP subjectAltName, SPKI
// pinning, single-use tokens, the IRC rendezvous message, and TLS 1.3 control
// and data channels. A Host interface (defined in server.go) connects it to
// the surrounding bot.
package sxdcc

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Identity is a loaded or freshly generated server identity: the TLS
// certificate (with private key) and the computed SPKI pin string.
type Identity struct {
	Cert tls.Certificate
	Pin  string
}

// LoadOrCreateIdentity returns an existing identity from certFile/keyFile, or
// generates a fresh Ed25519 key + self-signed certificate with the given IP as
// its only subjectAltName and persists it to certFile/keyFile. The key and
// cert directories are created if needed.
func LoadOrCreateIdentity(certFile, keyFile, ip string) (*Identity, error) {
	if fileExists(certFile) && fileExists(keyFile) {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, err
		}
		pin, err := pinFromCertChain(cert)
		if err != nil {
			return nil, err
		}
		return &Identity{Cert: cert, Pin: pin}, nil
	}

	id, err := GenerateIdentity(ip)
	if err != nil {
		return nil, err
	}
	if err := writeIdentity(certFile, keyFile, id.Cert); err != nil {
		return nil, err
	}
	return id, nil
}

// GenerateIdentity makes a new in-memory Ed25519 self-signed leaf cert with an
// IP subjectAltName. It returns the identity but does not persist it.
func GenerateIdentity(ip string) (*Identity, error) {
	addr := net.ParseIP(ip)
	if addr == nil {
		return nil, fmt.Errorf("sxdcc: invalid IP %q", ip)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: ip},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{addr},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, pub, priv)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}

	cert := tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  priv,
		Leaf:        leaf,
	}
	pin, err := pinFromCertChain(cert)
	if err != nil {
		return nil, err
	}
	return &Identity{Cert: cert, Pin: pin}, nil
}

func writeIdentity(certFile, keyFile string, cert tls.Certificate) error {
	if cert.Leaf == nil {
		return fmt.Errorf("sxdcc: cert has no parsed leaf")
	}
	if dir := filepath.Dir(certFile); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	if dir := filepath.Dir(keyFile); dir != "" && dir != filepath.Dir(certFile) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]})

	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		return err
	}
	return os.WriteFile(certFile, certPEM, 0o644)
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

// TLSConfig returns a TLS 1.3-only server config using the identity's cert.
func (id *Identity) TLSConfig() *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{id.Cert},
		MinVersion:   tls.VersionTLS13,
	}
}
