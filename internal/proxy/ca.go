package proxy

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// EnsureCA garante que existe um CA auto-assinado em dir.
// Se cert.pem e key.pem existem, carrega. Caso contrario, gera e grava.
// Retorna (cert, key, err).
//
// Uso: instalar cert.pem no browser/emulador como CA confiavel para MITM HTTPS.
func EnsureCA(dir string) (*x509.Certificate, *rsa.PrivateKey, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("mkdir ca: %w", err)
	}
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	if cert, key, err := loadCA(certPath, keyPath); err == nil {
		return cert, key, nil
	}
	cert, key, err := generateCA()
	if err != nil {
		return nil, nil, err
	}
	if err := writePEM(certPath, "CERTIFICATE", cert.Raw); err != nil {
		return nil, nil, err
	}
	kb, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	if err := writePEM(keyPath, "PRIVATE KEY", kb); err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func loadCA(certPath, keyPath string) (*x509.Certificate, *rsa.PrivateKey, error) {
	cb, err := os.ReadFile(certPath)
	if err != nil {
		return nil, nil, err
	}
	kb, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, err
	}
	cbBlock, _ := pem.Decode(cb)
	if cbBlock == nil {
		return nil, nil, fmt.Errorf("invalid cert pem")
	}
	cert, err := x509.ParseCertificate(cbBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}
	kbBlock, _ := pem.Decode(kb)
	if kbBlock == nil {
		return nil, nil, fmt.Errorf("invalid key pem")
	}
	keyAny, err := x509.ParsePKCS8PrivateKey(kbBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}
	key, ok := keyAny.(*rsa.PrivateKey)
	if !ok {
		return nil, nil, fmt.Errorf("key is not RSA")
	}
	return cert, key, nil
}

func generateCA() (*x509.Certificate, *rsa.PrivateKey, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			CommonName:   "mcp-proxy MITM CA",
			Organization: []string{"mcp-proxy"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func writePEM(path, blockType string, der []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: blockType, Bytes: der})
}
