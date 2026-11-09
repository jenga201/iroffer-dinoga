// Package sxdcc implements the server (bot) side of SXDCC, the secure TLS 1.3
// successor to XDCC/DCC described in IRC/dcc-protocol/SXDCC.md.
//
// It provides: self-signed Ed25519 identity with an IP subjectAltName, SPKI
// pinning, single-use tokens, the IRC rendezvous message, and TLS 1.3 control
// and data channels. A Host interface (defined in server.go) connects it to
// the surrounding bot.
package sxdcc

import (
	"crypto"
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
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return RenewIdentity(priv, ip)
}

// RenewIdentity issues a self-signed leaf for ip SAN signed by priv, returning
// the identity. Because the SPKI pin is the SHA-256 of the public key, renewing
// the certificate around the same key keeps the pin unchanged (SXDCC.md §7.5),
// so an advertised-IP change (e.g. VPN egress) does not break client TOFU.
func RenewIdentity(priv crypto.Signer, ip string) (*Identity, error) {
	addr := net.ParseIP(ip)
	if addr == nil {
		return nil, fmt.Errorf("sxdcc: invalid IP %q", ip)
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

	der, err := x509.CreateCertificate(rand.Reader, template, template, priv.Public(), priv)
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

// ipSAN returns the identity's IP subjectAltName (from the parsed leaf, or by
// parsing the certificate when the leaf is not populated on a loaded identity).
func (id *Identity) ipSAN() (string, bool) {
	leaf := id.Cert.Leaf
	if leaf == nil {
		leaf, _ = x509.ParseCertificate(id.Cert.Certificate[0])
	}
	if leaf == nil || len(leaf.IPAddresses) == 0 {
		return "", false
	}
	return leaf.IPAddresses[0].String(), true
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
// Cipher suites are pinned to prefer AES-256-GCM then ChaCha20-Poly1305
// (SXDCC.md §6), with AES-128-GCM last for compatibility.
func (id *Identity) TLSConfig() *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{id.Cert},
		MinVersion:   tls.VersionTLS13,
		CipherSuites: []uint16{
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_AES_128_GCM_SHA256,
		},
	}
}
