package sxdcc

import (
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"strings"
)

const pinPrefix = "sha256:"

// SPKIPin computes the SHA-256 SPKI pin (RFC 7469-style) of a certificate's
// public key and renders it as "sha256:<base64url-nopad>".
func SPKIPin(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return pinPrefix + base64.RawURLEncoding.EncodeToString(sum[:])
}

func pinFromCertChain(cert tls.Certificate) (string, error) {
	leaf := cert.Leaf
	if leaf == nil {
		var err error
		leaf, err = x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			return "", err
		}
	}
	return SPKIPin(leaf), nil
}

// PinEqual compares two pin strings in constant time. It is tolerant of a
// missing or present "sha256:" prefix and returns false for malformed pins
// rather than panicking. A fixed-length base64 comparison is preferred, but a
// length difference already reveals only that the values differ, so falling
// back to subtle on the raw bytes is acceptable.
func PinEqual(a, b string) bool {
	a = strings.TrimPrefix(a, pinPrefix)
	b = strings.TrimPrefix(b, pinPrefix)
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
