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
}

// Recommend recibe el estado wireless del router (radios propias) y devuelve
// una recomendación de canal por radio, ponderando por intensidad de señal.
func (s *Store) Recommend(routerID string, radios []probe.Radio, within time.Duration) ([]Radio, error) {
	scans, err := s.RecentScans(routerID, within)
	if err != nil {
		return nil, err
	}

	// Agrupar scans por banda y canal.
	byBand := map[string]map[int][]ScanRow{}
	for _, sc := range scans {
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
			Iface:    ifaceForBand(band), // placeholder; el agente no reporta iface por radio
			Name:     r.Name,
			Channel:  r.Channel,
			WidthMhz: r.WidthMhz,
		}
		candidates := candidateChannels(band)
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

func candidateChannels(band string) []int {
	switch band {
	case "2.4 GHz":
		return []int{1, 6, 11}
	case "5 GHz":
		// UNII-1/2/3 canales no-DFS preferidos para uso doméstico.
		return []int{36, 40, 44, 48, 149, 153, 157, 161, 165}
	case "6 GHz":
		return []int{1, 5, 9, 13, 17, 21, 25, 29}
	}
	return nil
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
