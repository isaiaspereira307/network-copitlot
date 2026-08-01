package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateV1ToV2_NoOpWhenClean(t *testing.T) {
	dir := t.TempDir()
	if err := MigrateV1ToV2(dir); err != nil {
		t.Fatal(err)
	}
	// marker nao deve existir (so criado quando ha dados)
	if _, err := os.Stat(filepath.Join(dir, migrationMarker)); err == nil {
		t.Error("marker must not be created when nothing to migrate")
	}
}

func TestMigrateV1ToV2_MovesLegacyDB(t *testing.T) {
	dir := t.TempDir()
	// simula v1: requests.db na raiz do workspace
	if err := os.WriteFile(filepath.Join(dir, "requests.db"), []byte("fake-v1-db"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MigrateV1ToV2(dir); err != nil {
		t.Fatal(err)
	}
	// requests.db na raiz deve ter sido removido
	if _, err := os.Stat(filepath.Join(dir, "requests.db")); !os.IsNotExist(err) {
		t.Error("legacy requests.db should be removed")
	}
	// marker criado
	if _, err := os.Stat(filepath.Join(dir, migrationMarker)); err != nil {
		t.Errorf("marker not created: %v", err)
	}
	// alvo imported-v1/legacy/ deve existir
	legacyDir := filepath.Join(dir, "imported-v1", "targets", "legacy")
	if _, err := os.Stat(legacyDir); err != nil {
		t.Errorf("legacy target dir not created: %v", err)
	}
}

func TestMigrateV1ToV2_Idempotent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requests.db"), []byte("fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MigrateV1ToV2(dir); err != nil {
		t.Fatal(err)
	}
	// segunda chamada: no-op, sem erro
	if err := MigrateV1ToV2(dir); err != nil {
		t.Fatalf("second call failed: %v", err)
	}
}
