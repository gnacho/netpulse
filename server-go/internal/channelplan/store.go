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
	// ownSeeds devuelve BSSIDs exactos reportados como propios por los
	// agentes (dawn/usteer local=true, #1082); opcional, se cablea en main.
	ownSeeds func() []string
}

// NewStore crea el store sobre una conexión SQLite ya abierta.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// SetOwnSeeds fija el proveedor de BSSIDs propios exactos (p. ej.
// AgentRegistry.LocalBssids). Se combinan con el prefijo MAC de routers.mac
// y la transitividad por SSID.
func (s *Store) SetOwnSeeds(f func() []string) { s.ownSeeds = f }

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
	// Own marca los BSSIDs de la propia malla (#1070): comparten los 5
	// primeros octetos con la MAC de un router monitorizado. La UI los
	// destaca (cascada y tabla) en vez de pintarlos como vecinos ajenos.
	Own bool `json:"own"`
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
	// Semillas exactas de los agentes (dawn/usteer local=true, #1082).
	exactOwn := map[string]bool{}
	if s.ownSeeds != nil {
		for _, mac := range s.ownSeeds() {
			if mac = strings.ToUpper(strings.TrimSpace(mac)); mac != "" {
				exactOwn[mac] = true
			}
		}
	}
	// Los prefijos de la malla propia se leen ANTES de la query principal:
	// el pool de SQLite va con MaxOpenConns(1) (internal/db) y
	// ownMeshPrefixes hace su propia Query; con las dos vivas a la vez el
	// segundo db.Query se queda esperando conexión para siempre (deadlock
	// detectado con -timeout: 90 s colgado en db.conn).
	prefixes := s.ownMeshPrefixes()
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

	out := make([]ScanRow, 0)
	for rows.Next() {
		var r ScanRow
		if err := rows.Scan(&r.RouterID, &r.Iface, &r.BSSID, &r.SSID, &r.Channel, &r.Freq, &r.Signal, &r.Ts); err != nil {
			return nil, err
		}
		if exactOwn[strings.ToUpper(strings.TrimSpace(r.BSSID))] {
			r.Own = true
		} else {
			r.Own = isOwnMeshBSSID(r.BSSID, prefixes)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// #1076: transitividad por SSID. Los BSSIDs de una misma unidad pueden
	// no compartir prefijo con routers.mac (p. ej. el guest de un GL.iNet);
	// si CUALQUIER BSSID de un SSID es propio por prefijo, todo ese SSID se
	// considera de la malla. Riesgo de falso positivo: un vecino con el
	// mismo ESSID (raro en SSIDs personalizados; aceptable en uso doméstico).
	ownSSIDs := map[string]bool{}
	for _, r := range out {
		if r.Own && r.SSID != "" {
			ownSSIDs[r.SSID] = true
		}
	}
	if len(ownSSIDs) > 0 {
		for i := range out {
			if !out[i].Own && out[i].SSID != "" && ownSSIDs[out[i].SSID] {
				out[i].Own = true
			}
		}
	}
	return out, nil
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
	// Scores puntúa TODOS los canales/bloques de la banda (no solo los
	// recomendables), para el panel de puntuación del informe (#1070/#1076):
	// 2.4 GHz canal a canal; 5 GHz bloque a bloque a su ancho, DFS incluidos
	// (informativos, no recomendables).
	Scores []ChannelScore `json:"scores,omitempty"`
	// Section es la sección UCI de la radio ("radio0") reportada por el
	// agente (#500); vacía con agentes antiguos (la UI deshabilita apply).
	Section string `json:"section,omitempty"`
}

// ChannelScore puntúa un canal (2.4 GHz) o bloque a su ancho (5 GHz) de la
// banda del radio. Score es el weighted neighbor score; menor = más limpio.
// DFS marca bloques que en ETSI requieren detección de radar (usables, pero
// no los recomienda el motor). Recommendable = no-DFS y apto para sugerir.
type ChannelScore struct {
	Channel       int  `json:"channel"`
	Score         int  `json:"score"`
	Neighbors     int  `json:"neighbors"`
	Strongest     int  `json:"strongest"` // dBm de la vecina más fuerte considerada
	DFS           bool `json:"dfs"`
	Recommendable bool `json:"recommendable"`
}

// bandBlocks devuelve TODOS los canales/bloques puntuables de la banda a su
// ancho (#1076): 2.4 GHz canal a canal (20 MHz); 5 GHz primarios cuyo bloque
// completo cabe en la banda (DFS incluidos); 6 GHz solo los no-DFS (no
// mantenemos la tabla PSC).
func bandBlocks(band string, widthMhz int) []ChannelScore {
	mk := func(ch int, dfs bool) ChannelScore {
		return ChannelScore{Channel: ch, DFS: dfs, Recommendable: !dfs}
	}
	switch band {
	case "2.4 GHz":
		// Se puntúan los 13 canales, pero el motor solo SUGIERE los no
		// solapados 1/6/11 (#631): el resto se muestra en la tira (informa
		// de la congestión real) sin ser recomendables.
		out := make([]ChannelScore, 0, 13)
		for ch := 1; ch <= 13; ch++ {
			orthodox := ch == 1 || ch == 6 || ch == 11
			c := mk(ch, false)
			c.Recommendable = orthodox
			out = append(out, c)
		}
		return out
	case "5 GHz":
		n := widthMhz / 20
		if n < 1 {
			n = 1
		}
		// Bloques NO solapados: el salto es el ancho del bloque (n canales
		// de 20 MHz), alineados a los dos rangos de canales (36-144 y
		// 149-165). Con salto fijo de 4 los bloques de 40/80 MHz se
		// solapaban entre sí y la tira de puntuación duplicaba el mismo
		// espectro (#1076).
		stride := n * 4
		out := make([]ChannelScore, 0, 12)
		for _, base := range [2]int{36, 149} {
			for ch := base; ch <= 165; ch += stride {
				if base == 36 && ch > 144 {
					break
				}
				if ch+(n-1)*4 > 165 {
					continue
				}
				dfs := false
				for i := 0; i < n; i++ {
					if isDFSChannel(ch + i*4) {
						dfs = true
					}
				}
				// Recomendables solo UNII-1 (36-48) y UNII-3 (149-161): el
				// rango 68-92 (5350-5470 MHz) no está asignado a RLAN en
				// ETSI, aunque no sea DFS (#1076).
				c := mk(ch, dfs)
				c.Recommendable = !dfs && (ch <= 48 || ch >= 149)
				out = append(out, c)
			}
		}
		return out
	default:
		out := make([]ChannelScore, 0, len(candidateChannels(band, widthMhz)))
		for _, ch := range candidateChannels(band, widthMhz) {
			out = append(out, mk(ch, false))
		}
		return out
	}
}

// Recommend recibe el estado wireless del router (radios propias) y devuelve
// una recomendación de canal por radio, ponderando por intensidad de señal.
func (s *Store) Recommend(routerID string, radios []probe.Radio, within time.Duration) ([]Radio, error) {
	scans, err := s.RecentScans(routerID, within)
	if err != nil {
		return nil, err
	}

	// #631 + #1080: la malla propia NO congestiona. La exclusion usa el flag
	// Own de los propios ScanRow: prefijo MAC de routers.mac + transitividad
	// por SSID (#1076), asi el guest de una unidad con MAC ajena tampoco
	// penaliza. Fuente unica con lo que la UI pinta como "Tu red".

	// Agrupar scans por banda y canal (descartando la propia malla).
	byBand := map[string]map[int][]ScanRow{}
	for _, sc := range scans {
		if sc.Own {
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
		blocks := bandBlocks(band, r.WidthMhz)
		bestCh, bestScore := 0, math.MaxInt
		currentScore := math.MaxInt
		scores := make([]ChannelScore, 0, len(blocks))
		for _, b := range blocks {
			b.Score, b.Neighbors, b.Strongest = channelScoreDetailed(byBand[band], b.Channel)
			if b.Channel == r.Channel {
				currentScore = b.Score
			}
			// El motor recomienda solo el mejor bloque no-DFS (#518/#631).
			if b.Recommendable && b.Score < bestScore {
				bestScore = b.Score
				bestCh = b.Channel
			}
			scores = append(scores, b)
		}
		if len(scores) > 0 {
			rec.Scores = scores
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

// channelScoreDetailed pondera APs vecinos por canal: señales más fuertes
// (menos negativas) pesan más y se penalizan APs en canales adyacentes
// (sobre todo en 2.4 GHz). Además cuenta cuántas vecinas participan y la
// más fuerte entre ellas, para el panel de puntuación del informe (#1070).
func channelScoreDetailed(scans map[int][]ScanRow, channel int) (score int, neighbors int, strongest int) {
	total := 0.0
	strongest = math.MinInt
	for ch, list := range scans {
		for _, ap := range list {
			diff := abs(ch - channel)
			weighted := false
			if diff == 0 {
				// Mismo canal: peso completo, en dominio de potencia (#1080):
				// w = 10^((s+60)/10); una vecina a -50 pesa ~3000x una a -85.
				total += math.Pow(10, float64(ap.Signal+60)/10)
				weighted = true
			} else if diff <= 2 {
				// Canal adyacente (+-2): un cuarto (-6 dB), sobre todo en 2.4.
				total += math.Pow(10, float64(ap.Signal+60)/10) / 4
				weighted = true
			}
			if !weighted {
				continue
			}
			neighbors++
			if strongest == math.MinInt || ap.Signal > strongest {
				strongest = ap.Signal
			}
		}
	}
	if neighbors == 0 {
		// Sin vecinas no hay "más fuerte": 0 en vez de MinInt, que en JSON
		// sale como -9223372036854775808 y rompe la tabla.
		strongest = 0
	}
	// x1000 para conservar resolucion al pasar a int (w(-90 dBm) ~= 0.001).
	return int(total * 1000), neighbors, strongest
}

// channelScore es la puntuación ponderada pura (menor = canal más limpio).
func channelScore(scans map[int][]ScanRow, channel int) int {
	score, _, _ := channelScoreDetailed(scans, channel)
	return score
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
