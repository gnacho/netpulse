// integrations_sshpass_internal_test.go - #1708: applyIntegrationSSHAuth con
// un pool que solo implementa Run (los fakes clásicos de la app): la
// aplicación de credencial es opcional y no debe romper el camino SSH.
package httpapi

import (
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/rintegration"
)

type runOnlyPool struct {
	runs int
}

func (p *runOnlyPool) Run(host, cmd string, _ time.Duration) (string, error) {
	p.runs++
	return "", nil
}

// TestApplyIntegrationSSHAuthWithoutSetter: sin capacidad de contraseña en
// el pool, el comando SSH sigue ejecutándose con la llave de siempre.
func TestApplyIntegrationSSHAuthWithoutSetter(t *testing.T) {
	pool := &runOnlyPool{}
	mgr := rintegration.NewManager(t.TempDir())
	s := &server{pool: pool, integrations: mgr}
	if err := mgr.WriteSSHPassword("wrt", "pw"); err != nil {
		t.Fatal(err)
	}
	row := rintegration.Row{Slug: "wrt", Host: "10.0.0.9", Username: "admin"}

	if _, err := s.runIntegrationSSH(row, "true", time.Second); err != nil {
		t.Fatalf("runIntegrationSSH: %v", err)
	}
	if pool.runs != 1 {
		t.Fatalf("runs = %d, quiero 1", pool.runs)
	}
}
