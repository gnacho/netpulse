// fmtutil.go — utilidades de formato ES compartidas (demo random walk y
// WireGuard live): parseBytes/fmtBytes ("1,2 GB" ⇄ 1.2e9) y relTime
// ("hace 38 s"). Port literal de demo.js:35-51 y wireguard.js:38-55.
package adapters

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var byteStrRe = regexp.MustCompile(`^([\d.,]+)\s*(B|KB|MB|GB|TB)$`)

var unitMult = map[string]float64{"B": 1, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12}

// parseBytes parsea formato ES ("1,2 GB", "214 MB", "1,1 TB") a bytes.
// Quita puntos de miles y convierte la coma decimal (demo.js:37-41).
func parseBytes(str string) float64 {
	m := byteStrRe.FindStringSubmatch(strings.TrimSpace(str))
	if m == nil {
		return 0
	}
	num := strings.NewReplacer(".", "", ",", ".").Replace(m[1])
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0
	}
	return math.Round(v * unitMult[m[2]])
}

// fmtBytes formatea bytes al estilo ES del contrato (wireguard.js:38-46):
// ≥1e9 → entero "N GB" o 1 decimal con coma; ≥1e6 → "N MB"; ≥1e3 → "N KB".
func fmtBytes(bytes float64) string {
	if bytes >= 1e9 {
		v := bytes / 1e9
		if v == math.Trunc(v) {
			return fmt.Sprintf("%d GB", int64(v))
		}
		return strings.Replace(strconv.FormatFloat(v, 'f', 1, 64), ".", ",", 1) + " GB"
	}
	if bytes >= 1e6 {
		return fmt.Sprintf("%d MB", int64(math.Round(bytes/1e6)))
	}
	if bytes >= 1e3 {
		return fmt.Sprintf("%d KB", int64(math.Round(bytes/1e3)))
	}
	return fmt.Sprintf("%d B", int64(math.Round(bytes)))
}

// relTime: epoch seg → texto relativo ES (wireguard.js:49-55).
func relTime(epochSec, nowSec int64) string {
	diff := nowSec - epochSec
	if diff < 0 {
		diff = 0
	}
	switch {
	case diff < 60:
		return fmt.Sprintf("hace %d s", diff)
	case diff < 3600:
		return fmt.Sprintf("hace %d min", diff/60)
	case diff < 86400:
		return fmt.Sprintf("hace %d h", diff/3600)
	default:
		return fmt.Sprintf("hace %d días", diff/86400)
	}
}
