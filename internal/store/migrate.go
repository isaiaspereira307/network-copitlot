package store

import (
	"fmt"
	"os"
	"path/filepath"
)

const migrationMarker = ".mcp-proxy-v2-migrated"

// MigrateV1ToV2 move um requests.db flat da raiz do workspace para
// imported-v1/targets/legacy/requests.db. Idempotente: se marker existe
// ou se workspace ja tem projetos, no-op.
func MigrateV1ToV2(workspaceDir string) error {
	marker := filepath.Join(workspaceDir, migrationMarker)
	if _, err := os.Stat(marker); err == nil {
		return nil // ja migrado
	}
	legacy := filepath.Join(workspaceDir, "requests.db")
	if _, err := os.Stat(legacy); os.IsNotExist(err) {
		return nil // nada a fazer
	} else if err != nil {
		return err
	}
	// ha dados v1; cria estrutura v2
	targetDir := filepath.Join(workspaceDir, "imported-v1", "targets", "legacy")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	// cria meta.yaml do projeto + alvo
	projMeta := `name: imported-v1
type: bugbounty
created_at: 0001-01-01T00:00:00Z
`
	if err := os.WriteFile(filepath.Join(workspaceDir, "imported-v1", "meta.yaml"), []byte(projMeta), 0o600); err != nil {
		return fmt.Errorf("write project meta: %w", err)
	}
	tgtMeta := `host: legacy
created_at: 0001-01-01T00:00:00Z
notes: "importado da v1 (flat store)"
`
	if err := os.WriteFile(filepath.Join(targetDir, "meta.yaml"), []byte(tgtMeta), 0o600); err != nil {
		return fmt.Errorf("write target meta: %w", err)
	}
	// move o DB
	if err := os.Rename(legacy, filepath.Join(targetDir, "requests.db")); err != nil {
		return fmt.Errorf("move db: %w", err)
	}
	// cria marker
	return os.WriteFile(marker, []byte("migrated"), 0o600)
}
