package httpapi

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
)

// sshPoolRecorder anota los hosts que reciben un dial y puede fallar en los
// que se le indiquen, para probar que un fallo no aborta el resto (#1924).
type sshPoolRecorder struct {
	mu        sync.Mutex
	hosts     []string
	out       string
	failHosts map[string]bool
}

func (p *sshPoolRecorder) Run(host, cmd string, timeout time.Duration) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hosts = append(p.hosts, host)
	if p.failHosts[host] {
		return "", errors.New("ssh: handshake failed")
	}
	return p.out, nil
}

func (p *sshPoolRecorder) calls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.hosts...)
}

func TestConfigBackupSshTargets(t *testing.T) {
	rows := []adapters.RouterConfig{
		{ID: "gw", Name: "gw", Host: "10.0.0.1", Type: "openwrt"},
		{ID: "ap", Name: "ap", Host: "10.0.0.2", Type: ""},
		{ID: "sw", Name: "sw", Host: "10.0.0.3", Type: "managed-switch"},
		{ID: "served", Name: "served", Host: "10.0.0.4", Type: "openwrt"},
		{ID: "nohost", Name: "nohost", Host: "", Type: "openwrt"},
		{ID: "agentonly", Name: "agentonly", Host: "10.0.0.5", Type: "openwrt", AgentOnly: true},
	}
	got := configBackupSshTargets(rows, map[string]bool{"served": true})
	want := []string{"gw", "ap"}
	if len(got) != len(want) {
		t.Fatalf("selección: %d unidades, se esperaban %d (%+v)", len(got), len(want), got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("posición %d: %s, se esperaba %s", i, got[i].ID, id)
		}
	}
}

func newSshScheduleServer(t *testing.T, pool SSHRunner) *server {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return &server{db: d, pool: pool}
}

func TestConfigBackupSshPassStoresArchive(t *testing.T) {
	payload := []byte("tar-gz-fake")
	pool := &sshPoolRecorder{out: base64.StdEncoding.EncodeToString(payload) + "\n"}
	s := newSshScheduleServer(t, pool)
	if _, err := routerstore.AddRouter(s.db.DB, routerstore.AddInput{Name: "ap2", Host: "10.0.0.2"}); err != nil {
		t.Fatalf("AddRouter: %v", err)
	}
	if _, err := routerstore.AddRouter(s.db.DB, routerstore.AddInput{
		Name: "agent-only", Host: "10.0.0.9", InventoryMode: "agent",
	}); err != nil {
		t.Fatalf("AddRouter agent-only: %v", err)
	}

	if copied := s.configBackupSshPass(time.Now(), nil); copied != 1 {
		t.Fatalf("copias: %d, se esperaba 1", copied)
	}
	files, err := filepath.Glob(filepath.Join(s.firmwareBackupDir(), "*.tar.gz"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("archivos en el almacén: %d, se esperaba 1 (%v)", len(files), files)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("leer archivo: %v", err)
	}
	if string(data) != string(payload) {
		t.Fatalf("contenido %q, se esperaba %q", data, payload)
	}
	if calls := pool.calls(); len(calls) != 1 || calls[0] != "10.0.0.2:22" {
		t.Fatalf("diales: %v, se esperaba solo la unidad SSH", calls)
	}
}

func TestConfigBackupSshPassSurvivesFailures(t *testing.T) {
	payload := []byte("ok")
	pool := &sshPoolRecorder{
		out:       base64.StdEncoding.EncodeToString(payload),
		failHosts: map[string]bool{"10.0.0.10:22": true},
	}
	s := newSshScheduleServer(t, pool)
	for _, in := range []routerstore.AddInput{
		{Name: "caida", Host: "10.0.0.10"},
		{Name: "viva", Host: "10.0.0.11"},
	} {
		if _, err := routerstore.AddRouter(s.db.DB, in); err != nil {
			t.Fatalf("AddRouter: %v", err)
		}
	}
	if copied := s.configBackupSshPass(time.Now(), nil); copied != 1 {
		t.Fatalf("copias: %d, se esperaba 1 (la unidad caída no aborta el pase)", copied)
	}
	if calls := pool.calls(); len(calls) != 2 {
		t.Fatalf("diales: %v, se esperaban 2", calls)
	}
}

func TestConfigBackupSshPassHonoursRetry(t *testing.T) {
	pool := &sshPoolRecorder{out: base64.StdEncoding.EncodeToString([]byte("ok"))}
	s := newSshScheduleServer(t, pool)
	if _, err := routerstore.AddRouter(s.db.DB, routerstore.AddInput{Name: "ap", Host: "10.0.0.2"}); err != nil {
		t.Fatalf("AddRouter: %v", err)
	}
	now := time.Now()
	if copied := s.configBackupSshPass(now, nil); copied != 1 {
		t.Fatalf("primer pase: %d copias", copied)
	}
	if copied := s.configBackupSshPass(now.Add(time.Minute), nil); copied != 0 {
		t.Fatalf("segundo pase sin esperar: %d copias, se esperaba 0", copied)
	}
	if copied := s.configBackupSshPass(now.Add(configBackupSshRetry+time.Second), nil); copied != 1 {
		t.Fatalf("pase tras el freno: %d copias", copied)
	}
	if calls := pool.calls(); len(calls) != 2 {
		t.Fatalf("diales: %v, se esperaban 2", calls)
	}
}

func TestConfigBackupSshPassSkipsServedByAgent(t *testing.T) {
	pool := &sshPoolRecorder{out: base64.StdEncoding.EncodeToString([]byte("ok"))}
	s := newSshScheduleServer(t, pool)
	rc, err := routerstore.AddRouter(s.db.DB, routerstore.AddInput{Name: "ap", Host: "10.0.0.2"})
	if err != nil {
		t.Fatalf("AddRouter: %v", err)
	}
	if copied := s.configBackupSshPass(time.Now(), map[string]bool{rc.ID: true}); copied != 0 {
		t.Fatalf("copias: %d, se esperaba 0 (ya la sirvió su agente)", copied)
	}
	if calls := pool.calls(); len(calls) != 0 {
		t.Fatalf("diales: %v, se esperaba ninguno", calls)
	}
}
