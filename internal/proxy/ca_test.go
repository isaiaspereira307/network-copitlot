package proxy

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureCA_CreatesIfMissing(t *testing.T) {
	dir := t.TempDir()
	cert, key, err := EnsureCA(filepath.Join(dir, "ca"))
	if err != nil {
		t.Fatal(err)
	}
	if cert == nil || key == nil {
		t.Fatal("nil cert or key")
	}
	// arquivos no disco
	if _, err := os.Stat(filepath.Join(dir, "ca", "cert.pem")); err != nil {
		t.Errorf("cert.pem missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ca", "key.pem")); err != nil {
		t.Errorf("key.pem missing: %v", err)
	}
	// cert decodifica como x509
	pemData, _ := os.ReadFile(filepath.Join(dir, "ca", "cert.pem"))
	block, _ := pem.Decode(pemData)
	if block == nil {
		t.Fatal("cert.pem not PEM")
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	if !parsed.IsCA {
		t.Error("cert is not a CA")
	}
	if parsed.Subject.CommonName == "" {
		t.Error("cert has no CN")
	}
}

func TestEnsureCA_ReusesExisting(t *testing.T) {
	dir := t.TempDir()
	cert1, _, err := EnsureCA(filepath.Join(dir, "ca"))
	if err != nil {
		t.Fatal(err)
	}
	cert2, _, err := EnsureCA(filepath.Join(dir, "ca"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(cert1.Raw, cert2.Raw) {
		t.Error("EnsureCA generated a new cert on second call; should reuse")
	}
}
