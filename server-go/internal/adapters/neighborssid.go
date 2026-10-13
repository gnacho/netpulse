package adapters

// neighborssid.go — alerta new-neighbor-ssid (#1359): un SSID vecino que
// nunca se había visto y que SE QUEDA (dwell) merece un aviso info (un
// router nuevo enchufado, un IoT en modo pairing, un hotspot fijo); un
// hotspot de paso no dice nada.
//
// wifi_scans solo retiene 48h (#475), así que "nunca visto" NO se puede
// derivar de la tabla: los SSIDs conocidos se persisten en kv
// (ssid_known:<ssid>) y los ya alertados en ssid_alerted:<ssid> (one-shot
// de verdad, sobrevive a reinicios y a la poda).
//
// Anti-avalancha de arranque: en la primera evaluación de la feature (kv
// ssid_known_seeded ausente) TODOS los SSIDs activos se siembran como
// conocidos sin alertar - solo alertan los verdaderamente nuevos.
//
// La flota está auto-aprobada por definición (LocalSsids): nada que
// configurar para el caso común.

import (
	"fmt"
	"strings"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

const (
	// newSsidDwell: el SSID nuevo debe llevar visto al menos este tiempo
	// (minTs dentro de la retención) para alertar. Con scans cada 30 min,
	// un hotspot de paso nunca llega.
	newSsidDwell = 4 * time.Hour
	// newSsidActive: el SSID debe seguir activo (visto dentro de esta
	// ventana) para que el dwell cuente como cumplido ahora.
	newSsidActive = 2 * time.Hour
	// newSsidScanWindow: historia consultada = la retención de wifi_scans.
	newSsidScanWindow = 48 * time.Hour
)

// EvalNewNeighborSsids: una pasada sobre los SSIDs activos. La llama el
// handler de ingesta tras guardar un scan nuevo, igual que el impostor
// (#1358): es el momento exacto en que la evidencia cambia.
func (l *Live) EvalNewNeighborSsids() {
	if l.db == nil || l.agents == nil || l.engine == nil {
		return
	}
	now := time.Now()

	fleetSsids := map[string]bool{}
	for _, s := range l.agents.LocalSsids() {
		fleetSsids[s] = true
	}

	// SSIDs activos en la retención: primero/último sighting por SSID.
	// Sin fleet ni vacíos (redes ocultas no anuncian nombre).
	rows, err := l.db.Query(
		`SELECT ssid, MIN(ts), MAX(ts) FROM wifi_scans
		 WHERE ts >= ? AND ssid <> '' GROUP BY ssid`,
		now.Add(-newSsidScanWindow).Unix())
	if err != nil {
		return
	}
	type ssidSeen struct {
		minTs int64
		maxTs int64
	}
	active := map[string]ssidSeen{}
	for rows.Next() {
		var ssid string
		var minTs, maxTs int64
		if rows.Scan(&ssid, &minTs, &maxTs) != nil {
			continue
		}
		if fleetSsids[ssid] {
			continue
		}
		active[ssid] = ssidSeen{minTs: minTs, maxTs: maxTs}
	}
	rows.Close()

	// Siembra anti-avalancha (una vez por instalación de la feature). Debe
	// correr AUNQUE no haya activos ahora: si no, la siembra se pospondría
	// al primer SSID nuevo de verdad y se tragaría su alerta.
	var seeded int
	_ = l.db.QueryRow(`SELECT COUNT(1) FROM kv WHERE key='ssid_known_seeded'`).Scan(&seeded)
	if seeded == 0 {
		for ssid := range active {
			_, _ = l.db.Exec(`INSERT INTO kv (key, value) VALUES (?, '1') ON CONFLICT(key) DO NOTHING`, "ssid_known:"+ssid)
		}
		_, _ = l.db.Exec(`INSERT INTO kv (key, value) VALUES ('ssid_known_seeded', '1') ON CONFLICT(key) DO NOTHING`)
		return
	}
	if len(active) == 0 {
		return
	}

	known, alerted := map[string]bool{}, map[string]bool{}
	if kvs, err := l.db.Query(`SELECT key FROM kv WHERE key LIKE 'ssid_known:%' OR key LIKE 'ssid_alerted:%'`); err == nil {
		for kvs.Next() {
			var k string
			if kvs.Scan(&k) == nil {
				if rest, ok := strings.CutPrefix(k, "ssid_known:"); ok {
					known[rest] = true
				} else if rest, ok := strings.CutPrefix(k, "ssid_alerted:"); ok {
					alerted[rest] = true
				}
			}
		}
		kvs.Close()
	}

	for ssid, seen := range active {
		if known[ssid] || alerted[ssid] {
			continue
		}
		first := time.Unix(seen.minTs, 0)
		last := time.Unix(seen.maxTs, 0)
		if now.Sub(first) < newSsidDwell || last.Before(now.Add(-newSsidActive)) {
			continue // aún en dwell o ya desapareció
		}
		// Router y señal del sighting más reciente (para que el aviso diga
		// dónde se oye).
		var routerID string
		var signal int
		_ = l.db.QueryRow(
			`SELECT router_id, signal_dbm FROM wifi_scans WHERE ssid = ? ORDER BY ts DESC LIMIT 1`,
			ssid).Scan(&routerID, &signal)
		l.mu.Lock()
		routerName := routerID
		if rn := l.routerDisplayName(routerID); rn != "" {
			routerName = rn
		}
		l.mu.Unlock()
		alertID := "alert-new-ssid-" + ssid
		l.engine.Emit(AlertEvent{
			ID:       alertID,
			Category: alerts.CatClients, // visible por defecto (signal=none)
			Urgent:   false,
			Severity: "info",
			Title:    "New neighbor SSID",
			Description: fmt.Sprintf("%s heard for the first time (by %s, %d dBm, since %s)",
				ssid, routerName, signal, first.Format("15:04")),
			Hint: alerts.HintFor(alerts.HintNewNeighborSsid),
			Type: alerts.HintNewNeighborSsid,
			Vars: map[string]string{
				"ssid":   ssid,
				"router": routerName,
				"signal": fmt.Sprintf("%d", signal),
				"since":  first.Format("15:04"),
			},
			RouterID: routerID,
		})
		// One-shot: alertado y conocido aunque desaparezca y vuelva.
		_, _ = l.db.Exec(`INSERT INTO kv (key, value) VALUES (?, '1') ON CONFLICT(key) DO NOTHING`, "ssid_alerted:"+ssid)
		_, _ = l.db.Exec(`INSERT INTO kv (key, value) VALUES (?, '1') ON CONFLICT(key) DO NOTHING`, "ssid_known:"+ssid)
	}
}
