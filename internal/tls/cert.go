package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// LoadOrGenerate loads the TLS cert/key from disk, generating them on first run.
// Returns the tls.Certificate and the SHA-256 fingerprint of the cert.
func LoadOrGenerate(configDir string) (tls.Certificate, string, error) {
	certPath := filepath.Join(configDir, "cert.pem")
	keyPath := filepath.Join(configDir, "key.pem")

	if fileExists(certPath) && fileExists(keyPath) {
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return tls.Certificate{}, "", fmt.Errorf("load cert: %w", err)
		}
		fp, err := fingerprint(cert)
		if err != nil {
			return tls.Certificate{}, "", err
		}
		return cert, fp, nil
	}

	return generate(certPath, keyPath)
}

// NewServer returns an *http.Server with TLS configured.
func NewServer(addr string, handler http.Handler, cert tls.Certificate) *http.Server {
	return &http.Server{
		Addr:    addr,
		Handler: handler,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		},
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
}

// VerifyFunc returns a tls.VerifyPeerCertificate-compatible function that
// implements TOFU: on first contact it records the fingerprint, on subsequent
// contacts it verifies it matches. lookup and onNew are the synchronized
// read/write sides of the trust store — this function never touches the
// underlying storage directly, so it stays safe under concurrent handshakes
// to the same peerIP as long as lookup/onNew are themselves synchronized.
func VerifyFunc(peerIP string, lookup func(ip string) (string, bool), onNew func(ip, fp string)) func([][]byte, [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return fmt.Errorf("no certificates from peer")
		}
		sum := sha256.Sum256(rawCerts[0])
		fp := "sha256:" + hex.EncodeToString(sum[:])

		if known, ok := lookup(peerIP); ok {
			if known != fp {
				return fmt.Errorf("fingerprint mismatch for %s: expected %s got %s", peerIP, known, fp)
			}
			return nil
		}

		// First contact — trust and record.
		onNew(peerIP, fp)
		return nil
	}
}

func generate(certPath, keyPath string) (tls.Certificate, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("generate key: %w", err)
	}

	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "localsend-cli"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("create cert: %w", err)
	}

	if err := writePEM(certPath, "CERTIFICATE", derBytes, 0o600); err != nil {
		return tls.Certificate{}, "", err
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	if err := writePEM(keyPath, "EC PRIVATE KEY", keyDER, 0o600); err != nil {
		return tls.Certificate{}, "", err
	}

	cert, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	)
	if err != nil {
		return tls.Certificate{}, "", err
	}

	sum := sha256.Sum256(derBytes)
	fp := "sha256:" + hex.EncodeToString(sum[:])
	return cert, fp, nil
}

func fingerprint(cert tls.Certificate) (string, error) {
	if len(cert.Certificate) == 0 {
		return "", fmt.Errorf("empty certificate chain")
	}
	sum := sha256.Sum256(cert.Certificate[0])
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func writePEM(path, typ string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: typ, Bytes: data})
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
