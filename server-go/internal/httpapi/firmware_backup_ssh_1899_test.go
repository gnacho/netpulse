package httpapi

// #1899: copia de config por SSH en unidades sin agente.

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
)

type sshBackupFakePool struct {
	out string
	err error
	cmd string
}

func (p *sshBackupFakePool) Run(host, cmd string, timeout time.Duration) (string, error) {
	p.cmd = cmd
	return p.out, p.err
}

func makeSshBackupServer(t *testing.T, typ string, payload string) (*server, string, *sshBackupFakePool) {
	t.Helper()
	dataDir := t.TempDir()
	cfg, err := config.Load(map[string]string{
		"AUTH_USER": "admin", "AUTH_PASS": "test123456",
		"DEMO_MODE": "0", "DATA_DIR": dataDir, "NODE_ENV": "test",
	}, dataDir)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	d, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	r, err := routerstore.AddRouter(d.DB, routerstore.AddInput{
		Name: "SshUnit", Host: "192.168.1.61", Type: typ,
	})
	if err != nil {
		t.Fatalf("add router: %v", err)
	}
	pool := &sshBackupFakePool{out: payload}
	return &server{cfg: cfg, db: d, pool: pool}, r.ID, pool
}

func TestSshConfigBackupStoresTheArchive(t *testing.T) {
	// base64 con saltos de línea, como lo emite busybox en el router.
	raw := "TAR-GZ-DE-PRUEBA"
	payload := base64.StdEncoding.EncodeToString([]byte(raw))
	payload = payload[:4] + "\n" + payload[4:] + "\n"
	s, id, pool := makeSshBackupServer(t, "openwrt", payload)
	req := httptest.NewRequest("POST", "/api/firmware-backups/"+id, nil)
	req.SetPathValue("routerId", id)
	rec := httptest.NewRecorder()
	s.handleFirmwareBackupSsh(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Name string `json:"name"`
		Size int    `json:"size"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	if body.Size != len(raw) {
		t.Fatalf("size = %d, body %s", body.Size, rec.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(s.firmwareBackupDir(), body.Name))
	if err != nil {
		t.Fatalf("el backup no quedó guardado: %v", err)
	}
	if string(data) != raw {
		t.Fatalf("contenido = %q", data)
	}
	// El temporal del router se borra siempre: /tmp allí es RAM (#1897).
	if !strings.Contains(pool.cmd, "rm -f") || !strings.Contains(pool.cmd, "sysupgrade --create-backup") {
		t.Fatalf("comando inesperado: %s", pool.cmd)
	}
}

func TestSshConfigBackupRejectsNonOpenwrt(t *testing.T) {
	s, id, _ := makeSshBackupServer(t, "managed-switch", "")
	req := httptest.NewRequest("POST", "/api/firmware-backups/"+id, nil)
	req.SetPathValue("routerId", id)
	rec := httptest.NewRecorder()
	s.handleFirmwareBackupSsh(rec, req)
	if rec.Code != 422 {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
}

func TestDecodeSshBackupRejectsGarbage(t *testing.T) {
	if _, err := decodeSshBackup("   \n"); err == nil {
		t.Fatal("salida vacía aceptada")
	}
	if _, err := decodeSshBackup("esto no es base64!!!"); err == nil {
		t.Fatal("base64 inválido aceptado")
	}
}
