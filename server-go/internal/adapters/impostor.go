package adapters

// impostor.go — alerta wifi-ssid-impostor (#1358): un BSSID que NO es de la
// flota anunciando un SSID que SÍ es de la flota es la firma clásica de evil
// twin. No hace falta ninguna recolección nueva: los scans pasivos de
// vecinos ya caen en wifi_scans (Fase 18/#452) y los BSSIDs/SSIDs propios
// ya los recopila el registry de agentes (#1087) más las radios sondeadas
// por SSH (iwinfo).
//
// Anti-transitorio: un sighting suelto no alerta (un hotspot de paso con
// nombre parecido, un vecino que puso tu SSID un rato). El BSSID candidato
// tiene que aparecer en >=2 scans REALES (timestamps distintos en
// wifi_scans, no el mismo payload recongelado) separados al menos
// impostorDwell, con el último sighting reciente. Auto-resolve cuando lleva
// impostorGone sin verse.
//
// La lista de aprobados es la propia flota (zero-config): no hay nada que
// configurar para el caso común.

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

const (
	// impostorDwell: separación mínima entre el primer y el último sighting
	// para confirmar el impostor (con el scan típico de 30 min, el segundo
	// scan real llega ~30 min después del primero).
	impostorDwell = 20 * time.Minute
	// impostorRecent: el último sighting debe ser más reciente que esto
	// para que la alerta siga considerándose activa (2x el scan interval
	// más holgado que solemos ver, 1h).
	impostorRecent = 2 * time.Hour
	// impostorWindow: sightings más viejos que esto no cuentan.
	impostorWindow = 6 * time.Hour
	// impostorAllowKey (#1400): clave kv con la lista persistida de BSSIDs
	// aprobados a mano (APs conocidos fuera de la flota NetPulse: UniFi,
	// Omada... cuyos BSSIDs no llegan por agentes/SSH/DAWN).
	impostorAllowKey = "impostor_allowlist_v1"
)

// EvalSsidImpostors: una pasada sobre wifi_scans recientes. La llama el
// handler de ingesta del agente tras guardar un scan nuevo (momento exacto
// en que la evidencia cambia). Barata: una query acotada por ventana.
func (l *Live) EvalSsidImpostors() {
	if l.db == nil || l.agents == nil || l.engine == nil {
		return
	}
	now := time.Now()

	ownBssids := map[string]bool{}
	for _, b := range l.agents.LocalBssids() {
		ownBssids[b] = true
	}
	// Las radios sondeadas por SSH (iwinfo) también son propias: sin esto un
	// router sin agente anunciando el SSID de la flota sería falso positivo.
	// Fuente: la caché de extras del último sondeo (radios con BSSID real).
	l.mu.Lock()
	for _, ex := range l.extrasCache {
		for _, r := range ex.radios {
			if r.BSSID != "" {
				ownBssids[strings.ToUpper(r.BSSID)] = true
			}
		}
	}
	// #1400: BSSIDs aprobados a mano (lista persistida en kv). Copia local
	// para evaluar fuera del mutex.
	approved := make(map[string]bool, len(l.impostorAllow))
	for b := range l.impostorAllow {
		approved[b] = true
	}
	l.mu.Unlock()
	fleetSsids := map[string]bool{}
	for _, s := range l.agents.LocalSsids() {
		fleetSsids[s] = true
	}
	if len(fleetSsids) == 0 {
		return // sin flota wifi conocida no hay nada que suplantar
	}

	rows, err := l.db.Query(
		`SELECT bssid, ssid, router_id, signal_dbm, ts FROM wifi_scans WHERE ts >= ?`,
		now.Add(-impostorWindow).Unix())
	if err != nil {
		return
	}
	type sighting struct {
		ssid     string
		routerID string
		signal   int
		tss      map[int64]bool // timestamps distintos (scans reales)
		minTs    int64
		maxTs    int64
	}
	byBssid := map[string]*sighting{}
	for rows.Next() {
		var bssid, ssid, routerID string
		var signal, ts int64
		if rows.Scan(&bssid, &ssid, &routerID, &signal, &ts) != nil {
			continue
		}
		bssid = strings.ToUpper(strings.TrimSpace(bssid))
		if bssid == "" || !fleetSsids[ssid] || ownBssids[bssid] || approved[bssid] {
			continue
		}
		s := byBssid[bssid]
		if s == nil {
			// Inicializar con los datos de ESTE row: sin ORDER BY la BD
			// puede devolver primero el sighting más reciente, y si el
			// primero procesado no deja router/señal, la alerta saldría
			// con esos campos vacíos.
			s = &sighting{ssid: ssid, routerID: routerID, signal: int(signal), minTs: ts, maxTs: ts, tss: map[int64]bool{ts: true}}
			byBssid[bssid] = s
			continue
		}
		s.tss[ts] = true
		if ts < s.minTs {
			s.minTs = ts
		}
		if ts > s.maxTs {
			s.maxTs = ts
			s.routerID = routerID
			s.signal = int(signal)
		}
	}
	rows.Close()

	l.mu.Lock()
	defer l.mu.Unlock()
	// Nuevos confirmados: >=2 scans reales separados >= dwell, último reciente.
	for bssid, s := range byBssid {
		confirmed := len(s.tss) >= 2 &&
			time.Duration(s.maxTs-s.minTs)*time.Second >= impostorDwell &&
			time.Unix(s.maxTs, 0).After(now.Add(-impostorRecent))
		if !confirmed {
			continue
		}
		alertID := "alert-ssid-impostor-" + bssid
		if _, ok := l.impostorActive[bssid]; ok {
			continue // ya emitida; el engine la conserva activa
		}
		routerName := s.routerID
		if rn := l.routerDisplayName(s.routerID); rn != "" {
			routerName = rn
		}
		l.engine.Emit(AlertEvent{
			ID:       alertID,
			// CatClients y no CatSignal: signal está en LevelNone por defecto
			// (alertas de cobertura) y una alerta de SEGURIDAD no puede
			// quedar invisible en la config stock (#1358).
			Category:    alerts.CatClients,
			Urgent:      true,
			Severity:    "critical",
			Title:       "SSID impostor detected",
			Description: fmt.Sprintf("%s advertises %s (heard by %s, %d dBm)", bssid, s.ssid, routerName, s.signal),
			Hint:        alerts.HintFor(alerts.HintSsidImpostor),
			Type:        alerts.HintSsidImpostor,
			Vars: map[string]string{
				"bssid":  bssid,
				"ssid":   s.ssid,
				"router": routerName,
				"signal": fmt.Sprintf("%d", s.signal),
			},
			RouterID: s.routerID,
		})
		l.impostorActive[bssid] = alertID
	}
	// Auto-resolve: el BSSID alertado lleva impostorRecent sin aparecer en
	// ningún scan (se apagó o se fue). Si vuelve, la gracia empieza de cero.
	for bssid, alertID := range l.impostorActive {
		s := byBssid[bssid]
		if s == nil || time.Unix(s.maxTs, 0).Before(now.Add(-impostorRecent)) {
			l.engine.Resolve(alertID)
			delete(l.impostorActive, bssid)
		}
	}
}

// ImpostorApproved (#1400): BSSIDs aprobados a mano, ordenados. La UI la
// muestra y permite revocar.
func (l *Live) ImpostorApproved() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.impostorAllow))
	for b := range l.impostorAllow {
		out = append(out, b)
	}
	sort.Strings(out)
	return out
}

// ApproveImpostorBssid (#1400): marca un BSSID como propio (AP conocido que
// no forma parte de la flota NetPulse). Persiste en kv y, si hay una alerta
// de impostor activa para él, la resuelve al momento.
func (l *Live) ApproveImpostorBssid(bssid string) error {
	mac, err := net.ParseMAC(strings.TrimSpace(bssid))
	if err != nil {
		return fmt.Errorf("invalid bssid %q: %w", bssid, err)
	}
	norm := strings.ToUpper(mac.String())
	l.mu.Lock()
	defer l.mu.Unlock()
	l.impostorAllow[norm] = true
	l.saveImpostorAllowLocked()
	if alertID, ok := l.impostorActive[norm]; ok {
		l.engine.Resolve(alertID)
		delete(l.impostorActive, norm)
	}
	return nil
}

// RevokeImpostorBssid (#1400): quita un BSSID de la lista de aprobados. Si
// el BSSID sigue visible, la regla vuelve a alertar en la próxima evaluación
// (con la confirmación anti-transitorio de siempre).
func (l *Live) RevokeImpostorBssid(bssid string) {
	norm := strings.ToUpper(strings.TrimSpace(bssid))
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.impostorAllow, norm)
	l.saveImpostorAllowLocked()
}

// saveImpostorAllowLocked: persiste la lista en kv. Caller holds l.mu.
func (l *Live) saveImpostorAllowLocked() {
	if l.db == nil {
		return
	}
	list := make([]string, 0, len(l.impostorAllow))
	for b := range l.impostorAllow {
		list = append(list, b)
	}
	raw, err := json.Marshal(list)
	if err != nil {
		return
	}
	_, _ = l.db.Exec(
		"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		impostorAllowKey, string(raw))
}
