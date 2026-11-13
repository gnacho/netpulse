package httpapi

// #2011: el backup de config hecho por el agente se registraba como
// "config backup" aunque lo hubiera pedido el programador, mientras la vía SSH
// decía "scheduled config backup". Un script que cuenta las líneas programadas
// se quedaba corto con las unidades que llevan agente. El tick anota la unidad
// al pedir el backup y la subida consume la marca para elegir el prefijo.

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/firmware"
	"github.com/gnacho/netpulse/server-go/internal/sse"
)

const backup2011Slug = "ap1"

// newBackup2011Server monta el server con un agente conectado por SSE, que es
// lo que el tick necesita para pedirle el backup. Devuelve el server, el token
// del agente y el techo del stream.
func newBackup2011Server(t *testing.T) (*server, string) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	token := "agent-secret-2011"
	if _, err := d.Exec(
		`INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		agentTokenKey(backup2011Slug), hashAgentToken(token)); err != nil {
		t.Fatalf("token: %v", err)
	}
	hub := sse.NewAgentHub(func(slug, tok string) bool {
		return slug == backup2011Slug && tok == token
	})
	hub.SetAllowInsecureCommands(true)
	s := &server{db: d, agentHub: hub, firmware: firmware.NewStore(d.DB)}

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/agents/"+backup2011Slug+"/stream", nil).WithContext(ctx)
	req.SetPathValue("slug", backup2011Slug)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	go hub.HandleStream(rec, req)
	t.Cleanup(cancel)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(hub.ConnectedSlugs()) == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if len(hub.ConnectedSlugs()) == 0 {
		t.Fatal("el agente no llegó a conectarse al stream")
	}
	return s, token
}

// runScheduledTick fuerza un tick que vence ahora.
func runScheduledTick(t *testing.T, s *server, now time.Time) {
	t.Helper()
	sched := ConfigBackupSchedule{Enabled: true, DayOfWeek: int(now.Weekday()), Time: "00:00"}
	if err := s.saveConfigBackupSchedule(sched); err != nil {
		t.Fatalf("save schedule: %v", err)
	}
	s.configBackupScheduleTickAt(now)
}

// uploadAgentBackup simula la subida del agente y devuelve lo que fue al
// journal.
func uploadAgentBackup(t *testing.T, s *server, token string) string {
	t.Helper()
	mux := http.NewServeMux()
	s.registerFirmwareBackupRoutes(mux)

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	req := httptest.NewRequest(http.MethodPost, "/api/agents/"+backup2011Slug+"/firmware-backup",
		strings.NewReader("\x1f\x8btar-gz-de-prueba"))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("subida: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	return buf.String()
}

func TestAgentBackupAfterScheduledTickLogsScheduled(t *testing.T) {
	s, token := newBackup2011Server(t)
	now := time.Now()
	runScheduledTick(t, s, now)

	line := uploadAgentBackup(t, s, token)
	if !strings.Contains(line, "[netpulse] scheduled config backup: "+backup2011Slug+": agent backup") {
		t.Fatalf("la subida pedida por el programador no se registró como programada: %q", line)
	}
}

func TestAgentBackupWithoutTickLogsAdHoc(t *testing.T) {
	s, token := newBackup2011Server(t)

	line := uploadAgentBackup(t, s, token)
	if strings.Contains(line, "scheduled config backup") {
		t.Fatalf("un backup manual no puede salir como programado: %q", line)
	}
	if !strings.Contains(line, "[netpulse] config backup: "+backup2011Slug+": agent backup") {
		t.Fatalf("falta la línea del backup ad hoc: %q", line)
	}
}

func TestScheduledAgentMarkIsConsumedOnce(t *testing.T) {
	s, _ := newBackup2011Server(t)
	now := time.Now()
	s.markScheduledAgentBackup(backup2011Slug, now)
	if !s.takeScheduledAgentBackup(backup2011Slug, now.Add(time.Minute)) {
		t.Fatal("la marca debe valer dentro de la ventana")
	}
	if s.takeScheduledAgentBackup(backup2011Slug, now.Add(2*time.Minute)) {
		t.Fatal("la marca se consume con la primera subida")
	}
}

func TestScheduledAgentMarkExpires(t *testing.T) {
	s, _ := newBackup2011Server(t)
	now := time.Now()
	s.markScheduledAgentBackup(backup2011Slug, now)
	// Pasada la ventana, una subida es ad hoc: no puede heredar el prefijo de
	// una petición programada de hace media hora.
	if s.takeScheduledAgentBackup(backup2011Slug, now.Add(configBackupAgentWait+time.Minute)) {
		t.Fatal("una marca vieja no debe etiquetar la subida como programada")
	}
	s.cfgBackupAgentMu.Lock()
	pending := len(s.cfgBackupAgentPending)
	s.cfgBackupAgentMu.Unlock()
	if pending != 0 {
		t.Fatalf("quedaron %d marcas sin limpiar", pending)
	}
}
