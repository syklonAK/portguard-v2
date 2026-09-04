package api

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"
)

func timeNow() time.Time { return time.Now() }

// parseCertBlock decodes the first CERTIFICATE block of a PEM payload.
func parseCertBlock(certPEM string) (*x509.Certificate, error) {
	rest := []byte(certPEM)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return nil, errors.New("no CERTIFICATE block found in PEM")
		}
		if block.Type == "CERTIFICATE" {
			return x509.ParseCertificate(block.Bytes)
		}
	}
}

// publicKeyOfKeyPEM loads a PEM private key (PKCS#8 / PKCS#1 / SEC1),
// skipping unrelated blocks such as bundled certificates.
func publicKeyOfKeyPEM(keyPEM string) (crypto.PublicKey, error) {
	rest := []byte(keyPEM)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return nil, errors.New("no private key block found in PEM")
		}
		switch block.Type {
		case "PRIVATE KEY":
			key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return nil, err
			}
			return pubOf(key), nil
		case "RSA PRIVATE KEY":
			key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
			if err != nil {
				return nil, err
			}
			return key.Public(), nil
		case "EC PRIVATE KEY":
			key, err := x509.ParseECPrivateKey(block.Bytes)
			if err != nil {
				return nil, err
			}
			return key.Public(), nil
		}
	}
}

// pubOf extracts the public key from any supported private key type.
func pubOf(key any) crypto.PublicKey {
	switch k := key.(type) {
	case *rsa.PrivateKey:
		return k.Public()
	case *ecdsa.PrivateKey:
		return k.Public()
	case ed25519.PrivateKey:
		return k.Public()
	}
	return nil
}

func publicKeyOfCert(cert *x509.Certificate) (crypto.PublicKey, bool) {
	switch pub := cert.PublicKey.(type) {
	case *rsa.PublicKey, *ecdsa.PublicKey, ed25519.PublicKey:
		return pub, true
	}
	return nil, false
}

// keysEqual compares two public keys by their DER encoding.
func keysEqual(a, b crypto.PublicKey) bool {
	aDER, err1 := x509.MarshalPKIXPublicKey(a)
	bDER, err2 := x509.MarshalPKIXPublicKey(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return string(aDER) == string(bDER)
}

var _ = fmt.Sprintf
