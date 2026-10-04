// Package channelplan — almacenamiento y recomendación de canales WiFi
// a partir de scans pasivos del agente (Fase 18, #452).
package channelplan

import (
	"database/sql"
	"math"
	"strings"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
)

// Store persiste scans y calcula recomendaciones de canal.
type Store struct {
	db *sql.DB
}

// NewStore crea el store sobre una conexión SQLite ya abierta.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// SaveScan guarda los resultados de un scan pasivo recibido en un push.
func (s *Store) SaveScan(routerID string, ts int64, scans []probe.ScanResult) error {
	if len(scans) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO wifi_scans (router_id, iface, bssid, ssid, channel, freq, signal_dbm, ts)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, sc := range scans {
		if _, err := stmt.Exec(routerID, sc.Iface, strings.ToUpper(sc.BSSID), sc.SSID, sc.Channel, sc.Freq, sc.Signal, ts); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ScanRow es un vecino persistido (forma plana para la UI/API).
type ScanRow struct {
	Iface    string `json:"iface"`
	BSSID    string `json:"bssid"`
	SSID     string `json:"ssid"`
	Channel  int    `json:"channel"`
	Freq     int    `json:"freq"`
	Signal   int    `json:"signal"`
	Ts       int64  `json:"ts"`
	RouterID string `json:"routerId"`
}

// RecentScans devuelve los vecinos vistos recientemente (opcionalmente
// filtrado por routerID) dentro de la ventana de fresividad, DEDUPLICADOS por
// BSSID: cada push del agente reinserta los mismos vecinos, y sin dedup el
// mismo AP contaba una vez por push (decenas de miles de filas al día),
// reventaba el score (overflow) y la tabla de vecinos era puro ruido.
// SQLite: con un único agregado MAX(ts), las columnas "bare" salen de la
// fila del máximo (https://sqlite.org/lang_select.html#bareagg).
func (s *Store) RecentScans(routerID string, within time.Duration) ([]ScanRow, error) {
	cutoff := time.Now().Add(-within).Unix()
	var rows *sql.Rows
	var err error
	if routerID != "" {
		rows, err = s.db.Query(`
			SELECT router_id, iface, bssid, ssid, channel, freq, signal_dbm, MAX(ts)
			FROM wifi_scans
			WHERE router_id = ? AND ts >= ?
			GROUP BY bssid
			ORDER BY signal_dbm ASC, bssid
		`, routerID, cutoff)
	} else {
		rows, err = s.db.Query(`
			SELECT router_id, iface, bssid, ssid, channel, freq, signal_dbm, MAX(ts)
			FROM wifi_scans
			WHERE ts >= ?
			GROUP BY router_id, bssid
			ORDER BY signal_dbm ASC, bssid
		`, cutoff)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ScanRow
	for rows.Next() {
		var r ScanRow
		if err := rows.Scan(&r.RouterID, &r.Iface, &r.BSSID, &r.SSID, &r.Channel, &r.Freq, &r.Signal, &r.Ts); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Radio es un resumen de una radio propia con su canal actual y recomendación.
type Radio struct {
	Iface        string `json:"iface"`
	Name         string `json:"name"`    // "2.4 GHz" | "5 GHz" | "6 GHz"
	Channel      int    `json:"channel"` // canal actual
	WidthMhz     int    `json:"widthMhz"`
	Recommended  int    `json:"recommended"` // canal recomendado; 0 = sin datos
	CurrentScore int    `json:"currentScore"`
	BestScore    int    `json:"bestScore"`
	// Section es la sección UCI de la radio ("radio0") reportada por el
	// agente (#500); vacía con agentes antiguos (la UI deshabilita apply).
	Section string `json:"section,omitempty"`
}

// Recommend recibe el estado wireless del router (radios propias) y devuelve
// una recomendación de canal por radio, ponderando por intensidad de señal.
func (s *Store) Recommend(routerID string, radios []probe.Radio, within time.Duration) ([]Radio, error) {
	scans, err := s.RecentScans(routerID, within)
	if err != nil {
		return nil, err
	}

	// #631: la malla propia (los APs de los routers monitorizados) NO debe
	// contarse como "vecino": sus BSSIDs comparten los primeros 5 octetos con
	// la MAC del router (radios = MAC base, +1, +2...). Se excluyen del
	// scoring para que la recomendación optimice contra interferencia externa
	// y no sugiera canales que la propia malla ya ocupa (autointerferencia).
	ownPrefixes := s.ownMeshPrefixes()

	// Agrupar scans por banda y canal (descartando la propia malla).
	byBand := map[string]map[int][]ScanRow{}
	for _, sc := range scans {
		if isOwnMeshBSSID(sc.BSSID, ownPrefixes) {
			continue
		}
		band := bandForFreq(sc.Freq)
		if byBand[band] == nil {
			byBand[band] = map[int][]ScanRow{}
		}
		byBand[band][sc.Channel] = append(byBand[band][sc.Channel], sc)
	}

	out := make([]Radio, 0, len(radios))
	for _, r := range radios {
		band := bandForName(r.Name)
		freq := channelToFreq(r.Channel) // best-effort para casar con scans
		if freq == 0 {
			freq = bandCenter(band, r.Channel)
		}
		rec := Radio{
			Iface:    r.Section, // sección real si el agente la reporta (#500)
			Name:     r.Name,
			Channel:  r.Channel,
			WidthMhz: r.WidthMhz,
			Section:  r.Section,
		}
		if rec.Iface == "" {
			rec.Iface = ifaceForBand(band) // placeholder; el agente no reporta iface por radio
		}
		candidates := candidateChannels(band, r.WidthMhz)
		bestCh, bestScore := 0, math.MaxInt
		currentScore := math.MaxInt
		for _, ch := range candidates {
			score := channelScore(byBand[band], ch)
			if ch == r.Channel {
				currentScore = score
			}
			if score < bestScore {
				bestScore = score
				bestCh = ch
			}
		}
		// #518: el canal ACTUAL puede ser DFS (52/100/112/116...) y no estar en
		// candidateChannels (solo no-DFS para recomendar). Su score es
		// informativo ("cuánto ruido tengo ahora") y debe calcularse igual,
		// no quedarse en MaxInt y pintar 9223372036854775807 en la UI.
		if currentScore == math.MaxInt {
			currentScore = channelScore(byBand[band], r.Channel)
		}
		if bestCh != 0 && bestScore != math.MaxInt {
			rec.Recommended = bestCh
			rec.CurrentScore = currentScore
			rec.BestScore = bestScore
		}
		out = append(out, rec)
	}
	return out, nil
}

// channelScore pondera APs vecinos por canal: señales más fuertas (menos
// negativas) pesan más. Se suma una penalización por APs en canales adyacentes
// (sobre todo en 2.4 GHz).
func channelScore(scans map[int][]ScanRow, channel int) int {
	score := 0.0
	for ch, list := range scans {
		for _, ap := range list {
			diff := abs(ch - channel)
			if diff == 0 {
				// Mismo canal: peso completo. Señal fuerte (+60 dBm) suma 60;
				// señal débil (-90 dBm) suma 10.
				score += float64(-ap.Signal) / 1.5
			} else if diff <= 2 {
				// Canal adyacente: peso reducido. Importante en 2.4 GHz.
				score += float64(-ap.Signal) / 5.0
			}
		}
	}
	return int(score)
}

// candidateChannels devuelve los canales candidatos no-DFS para la banda,
// filtrando por el ancho del radio (#631): a 40/80/160 MHz solo valen los
// canales cuyo bloque completo (canales ch..ch+(n-1)*4, n=w/20) no entra en
// un canal DFS (52-64, 100-144) ni se sale de la banda.
func candidateChannels(band string, widthMhz int) []int {
	var base []int
	switch band {
	case "2.4 GHz":
		base = []int{1, 6, 11}
	case "5 GHz":
		// UNII-1/3 canales no-DFS preferidos para uso doméstico.
		base = []int{36, 40, 44, 48, 149, 153, 157, 161, 165}
	case "6 GHz":
		base = []int{1, 5, 9, 13, 17, 21, 25, 29}
	default:
		return nil
	}
	if widthMhz <= 20 {
		return base
	}
	out := make([]int, 0, len(base))
	for _, ch := range base {
		if validForWidth(ch, widthMhz, band) {
			out = append(out, ch)
		}
	}
	return out
}

// validForWidth indica si un canal al ancho dado ocupa un bloque fuera de
// canales DFS (5 GHz) y dentro de la banda.
func validForWidth(ch, widthMhz int, band string) bool {
	if band != "5 GHz" {
		// 2.4 y 6 GHz no tienen canales DFS en el ámbito doméstico.
		return true
	}
	n := widthMhz / 20 // 1,2,4,8 para 20/40/80/160
	if n < 1 {
		n = 1
	}
	for i := 0; i < n; i++ {
		cc := ch + i*4
		if cc > 165 || isDFSChannel(cc) {
			return false
		}
	}
	return true
}

// isDFSChannel: canales que en ETSI/EU requieren detección DFS (radio no
// puede usarlos sin radar) — UNII-2A (52-64) y UNII-2C (100-144).
func isDFSChannel(ch int) bool {
	return (ch >= 52 && ch <= 64) || (ch >= 100 && ch <= 144)
}

// ownMeshPrefixes devuelve los primeros 5 octetos de la MAC de cada router
// monitorizado (tabla routers). Un BSSID cuyo prefijo coincida es un AP de la
// propia malla y no debe computarse como vecino (#631).
func (s *Store) ownMeshPrefixes() []string {
	var out []string
	if s.db == nil {
		return out
	}
	rows, err := s.db.Query(`SELECT mac FROM routers WHERE mac IS NOT NULL AND mac <> ''`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var mac string
		if rows.Scan(&mac) == nil {
			mac = strings.ToUpper(strings.TrimSpace(mac))
			if len(mac) >= 14 {
				out = append(out, mac[:14]) // "AA:BB:CC:DD:EE"
			}
		}
	}
	return out
}

func isOwnMeshBSSID(bssid string, prefixes []string) bool {
	b := strings.ToUpper(strings.TrimSpace(bssid))
	if len(b) < 14 {
		return false
	}
	pref := b[:14]
	for _, p := range prefixes {
		if p == pref {
			return true
		}
	}
	return false
}

func bandForFreq(freq int) string {
	switch {
	case freq >= 2412 && freq <= 2484:
		return "2.4 GHz"
	case freq >= 5180 && freq <= 5885:
		return "5 GHz"
	case freq >= 5955:
		return "6 GHz"
	}
	return ""
}

func bandForName(name string) string {
	if strings.Contains(name, "2.4") {
		return "2.4 GHz"
	}
	if strings.Contains(name, "5") {
		return "5 GHz"
	}
	if strings.Contains(name, "6") {
		return "6 GHz"
	}
	return ""
}

func ifaceForBand(band string) string {
	switch band {
	case "2.4 GHz":
		return "wlan0"
	case "5 GHz":
		return "wlan1"
	}
	return ""
}

// channelToFreq best-effort para 2.4 y 5 GHz; se usa como fallback si no
// tenemos freq directa.
func channelToFreq(ch int) int {
	if ch >= 1 && ch <= 14 {
		if ch == 14 {
			return 2484
		}
		return 2407 + ch*5
	}
	if ch >= 36 && ch <= 165 {
		return 5000 + ch*5
	}
	return 0
}

func bandCenter(band string, ch int) int {
	if f := channelToFreq(ch); f > 0 {
		return f
	}
	switch band {
	case "2.4 GHz":
		return 2437
	case "5 GHz":
		return 5180
	case "6 GHz":
		return 5955
	}
	return 0
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Prune elimina scans más antiguos que `retention`.
func (s *Store) Prune(retention time.Duration) error {
	cutoff := time.Now().Add(-retention).Unix()
	_, err := s.db.Exec(`DELETE FROM wifi_scans WHERE ts < ?`, cutoff)
	return err
}
