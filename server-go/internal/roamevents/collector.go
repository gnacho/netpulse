// Package roamevents — ingestión y persistencia continua de eventos de
// hostapd/DAWN para la pestaña Eventos de /roaming (Fase 14.5).
//
// El feed viene de `logread` en cada router con SSH. Una goroutine dedicada
// (Collector) corre cada 60s, hace SSH `logread | grep -E 'AP-STA-CONNECTED|
// AP-STA-DISCONNECTED|dawn:'` por router, parsea cada línea a un RoamEvent y
// lo inserta con INSERT OR IGNORE (dedup por content_hash).
//
// Solo se ingestan 3 tipos de eventos: connected, disconnected, dawn_decision.
// BEACON-REQ/RESP se descartan (90% del ruido, valor bajo sin contexto FT).
package roamevents

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Tipo de evento roaming.
const (
	TypeConnected     = "connected"
	TypeDisconnected  = "disconnected"
	TypeDawnDecision = "dawn_decision"
)

// Event es una fila de roam_events (entrada del feed).
type Event struct {
	ID        int64  `json:"id"`
	TsMs      int64  `json:"ts_ms"`
	RouterID  string `json:"router_id"`
	Type      string `json:"type"`
	MAC       string `json:"mac,omitempty"`
	Iface     string `json:"iface,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// RouterHost resuelve router_id → host para que el Collector haga SSH.
// Se inyecta desde Live para no acoplar roamevents al adapter.
type RouterHost struct {
	ID        string
	Host      string
	Name      string
	AgentOnly bool
}

// Runner es la abstracción de SSH (satisfecha por *sshpool.Pool).
type Runner interface {
	Run(host, cmd string, timeout time.Duration) (string, error)
}

// Collector coordina la ingesta continua de eventos.
type Collector struct {
	db     *sql.DB
	runner Runner
	hosts  func() []RouterHost
	stop   chan struct{}
	wg     sync.WaitGroup
}

// NewCollector construye un Collector. `hosts` se llama en cada ciclo para
// obtener la lista actual de routers (permite alta/baja en caliente).
func NewCollector(db *sql.DB, runner Runner, hosts func() []RouterHost) *Collector {
	return &Collector{db: db, runner: runner, hosts: hosts, stop: make(chan struct{})}
}

// Start lanza la goroutine del ciclo de ingesta (cada 60s).
func (c *Collector) Start() {
	c.wg.Add(1)
	go c.loop()
}

// Stop detiene el ciclo y espera a la goroutine.
func (c *Collector) Stop() {
	close(c.stop)
	c.wg.Wait()
}

func (c *Collector) loop() {
	defer c.wg.Done()
	// Primer ciclo casi inmediato para no esperar 60s en arranque.
	c.tick()
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-t.C:
			c.tick()
		}
	}
}

func (c *Collector) tick() {
	hosts := c.hosts()
	if len(hosts) == 0 {
		return
	}
	for _, h := range hosts {
		if h.AgentOnly {
			continue
		}
		select {
		case <-c.stop:
			return
		default:
		}
		c.collectRouter(h)
	}
}

func (c *Collector) collectRouter(h RouterHost) {
	// grep extiende el set a futuro (eventos FT, etc.) sin tocar el binario.
	cmd := "logread 2>/dev/null | grep -E 'AP-STA-CONNECTED|AP-STA-DISCONNECTED|dawn:' | tail -100"
	out, err := c.runner.Run(h.Host, cmd, 8*time.Second)
	if err != nil {
		return
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		ev, ok := ParseLogreadLine(line, h.ID)
		if !ok {
			continue
		}
		InsertEvent(c.db, ev)
	}
}

// InsertEvent inserta un evento con dedup por content_hash. No falla el
// ciclo si el insert choca con el UNIQUE (caso normal: mismo evento en
// varios ciclos de logread).
func InsertEvent(db *sql.DB, ev Event) error {
	hash := contentHash(ev)
	_, err := db.Exec(
		`INSERT OR IGNORE INTO roam_events (ts_ms, router_id, type, mac, iface, detail, content_hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ev.TsMs, ev.RouterID, ev.Type, ev.MAC, ev.Iface, ev.Detail, hash,
	)
	return err
}

// contentHash calcula el hash de dedup: minute + router + type + mac + iface.
// Granularidad minuto: dos eventos idénticos en el mismo minuto = dup.
// En práctica raro (un cliente no se conecta dos veces en 60s al mismo AP).
func contentHash(ev Event) string {
	minute := ev.TsMs / 60_000
	h := sha1.New()
	fmt.Fprintf(h, "%d|%s|%s|%s|%s", minute, ev.RouterID, ev.Type, ev.MAC, ev.Iface)
	return hex.EncodeToString(h.Sum(nil))
}

// ListEvents lee eventos desde SQLite ordenados por ts DESC. Filtros opcionales.
func ListEvents(db *sql.DB, limit int, sinceMs int64, routerID, eventType string) ([]Event, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	q := "SELECT id, ts_ms, router_id, type, COALESCE(mac,''), COALESCE(iface,''), COALESCE(detail,'') FROM roam_events WHERE ts_ms >= ?"
	args := []any{sinceMs}
	if routerID != "" {
		q += " AND router_id = ?"
		args = append(args, routerID)
	}
	if eventType != "" {
		q += " AND type = ?"
		args = append(args, eventType)
	}
	q += " ORDER BY ts_ms DESC LIMIT ?"
	args = append(args, limit)
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var ev Event
		if err := rows.Scan(&ev.ID, &ev.TsMs, &ev.RouterID, &ev.Type, &ev.MAC, &ev.Iface, &ev.Detail); err != nil {
			continue
		}
		out = append(out, ev)
	}
	return out, nil
}

// --- Parser -------------------------------------------------------------

// logread usa formato syslog: "Sat Aug  8 19:21:45 2026 daemon.notice hostapd: wlan0: AP-STA-CONNECTED wlan0 04:95:e6:76:55:a1"
// El año puede faltar en algunos sistemas; asumimos el año actual.
//
// Regex principal: captura timestamp + resto. Las sub-regex descomponen
// hostapd y dawn por separado porque su estructura es muy distinta.
var (
	// syslog formato completo: "Sat Aug  8 19:21:45 2026 daemon.notice hostapd: <resto>".
	// Weekday opcional (algunos sistemas lo omiten). Múltiples espacios
	// entre día y mes en logread de OpenWrt (alineación).
	syslogRe = regexp.MustCompile(`^(?:\w{3}\s+)?(\w{3})\s+(\d+)\s+(\d{2}):(\d{2}):(\d{2})\s+\d{4}\s+daemon\.\w+\s+(\w+):\s+(.+)$`)
	// Variante sin año: "Sat Aug  8 19:21:45 daemon.notice hostapd: <resto>".
	syslogNoYearRe = regexp.MustCompile(`^(?:\w{3}\s+)?(\w{3})\s+(\d+)\s+(\d{2}):(\d{2}):(\d{2})\s+daemon\.\w+\s+(\w+):\s+(.+)$`)

	// hostapd: "<iface>: AP-STA-CONNECTED <iface2> <mac>"
	hostapdConnRe = regexp.MustCompile(`^(\S+):\s+AP-STA-(CONNECTED|DISCONNECTED)\s+\S+\s+([0-9a-fA-F:]{17})`)
	// hostapd variants sin segundo iface: "AP-STA-CONNECTED <mac>"
	hostapdConnShortRe = regexp.MustCompile(`^AP-STA-(CONNECTED|DISCONNECTED)\s+([0-9a-fA-F:]{17})`)

	// dawn: "Client / BSSID = <mac> / <bssid>: <action>"
	dawnClientRe = regexp.MustCompile(`Client\s*/\s*BSSID\s*=\s*([0-9a-fA-F:]{17})\s*/\s*([0-9a-fA-F:]{17}):\s*(.+)`)
)

// ParseLogreadLine intenta parsear una línea de logread a un Event.
// Devuelve (event, true) si casa; (zero, false) si no casa ningún tipo
// reconocido. routerID se inyecta desde el caller (no está en la línea).
//
// El timestamp se interpreta en la zona horaria local del servidor (los
// routers usan NTP sincronizado a la misma hora).
func ParseLogreadLine(line, routerID string) (Event, bool) {
	var mon, day, hh, mm, ss, prog, rest string
	m := syslogRe.FindStringSubmatch(line)
	if m == nil {
		m = syslogNoYearRe.FindStringSubmatch(line)
	}
	if m == nil {
		return Event{}, false
	}
	mon, day, hh, mm, ss, prog, rest = m[1], m[2], m[3], m[4], m[5], m[6], m[7]
	ts := parseSyslogTime(mon, day, hh, mm, ss)
	if ts == 0 {
		return Event{}, false
	}

	switch prog {
	case "hostapd":
		ev, ok := parseHostapd(rest, routerID, ts)
		if !ok {
			return Event{}, false
		}
		return ev, true
	case "dawn":
		ev, ok := parseDawn(rest, routerID, ts)
		if !ok {
			return Event{}, false
		}
		return ev, true
	}
	return Event{}, false
}

func parseHostapd(rest, routerID string, ts int64) (Event, bool) {
	// Caso largo: "wlan0: AP-STA-CONNECTED wlan0 04:95:..."
	if m := hostapdConnRe.FindStringSubmatch(rest); m != nil {
		iface := m[1]
		mac := m[3]
		typ := TypeConnected
		if m[2] == "DISCONNECTED" {
			typ = TypeDisconnected
		}
		return Event{TsMs: ts, RouterID: routerID, Type: typ, MAC: mac, Iface: iface}, true
	}
	// Caso corto: "AP-STA-CONNECTED 04:95:..."
	if m := hostapdConnShortRe.FindStringSubmatch(rest); m != nil {
		mac := m[2]
		typ := TypeConnected
		if m[1] == "DISCONNECTED" {
			typ = TypeDisconnected
		}
		return Event{TsMs: ts, RouterID: routerID, Type: typ, MAC: mac}, true
	}
	return Event{}, false
}

func parseDawn(rest, routerID string, ts int64) (Event, bool) {
	if m := dawnClientRe.FindStringSubmatch(rest); m != nil {
		mac := m[1]
		bssid := m[2]
		action := strings.TrimSpace(m[3])
		return Event{
			TsMs: ts, RouterID: routerID, Type: TypeDawnDecision, MAC: mac,
			Detail: fmt.Sprintf("BSSID %s: %s", bssid, action),
		}, true
	}
	return Event{}, false
}

var months = map[string]int{
	"Jan": 1, "Feb": 2, "Mar": 3, "Apr": 4, "May": 5, "Jun": 6,
	"Jul": 7, "Aug": 8, "Sep": 9, "Oct": 10, "Nov": 11, "Dec": 12,
}

// parseSyslogTime convierte "Aug 8 19:21:45" → epoch ms (asumiendo año actual).
// Devuelve 0 si el mes no se reconoce.
func parseSyslogTime(mon, day, hh, mm, ss string) int64 {
	month, ok := months[mon]
	if !ok {
		return 0
	}
	d, _ := strconv.Atoi(day)
	h, _ := strconv.Atoi(hh)
	mi, _ := strconv.Atoi(mm)
	s, _ := strconv.Atoi(ss)
	now := time.Now()
	t := time.Date(now.Year(), time.Month(month), d, h, mi, s, 0, time.Local)
	// Si la fecha parseada es más de 1 día en el futuro, probablemente el
	// evento fue del año pasado (28 dic parseado en enero).
	if t.After(now.Add(24 * time.Hour)) {
		t = t.AddDate(-1, 0, 0)
	}
	return t.UnixMilli()
}

// Ping mantiene el import del paquete context aunque todavía no se use.
// (Lo necesitará un futuro refactor a ctx-aware en el Runner.)
var _ = context.Background
