// dawn.go - módulo de orquestación "DAWN" (Fase 17.9).
//
// Instala/configura DAWN (Distributed AP WiFi Neighbor) y alinea los parámetros
// de roaming 802.11k/v/r en los APs. El módulo es declarativo: compara el
// estado detectado contra un baseline deseado y genera las Ops allowlistedas
// necesarias.
//
// Baseline por defecto (verificada en red Mandor y documentada en DAWN):
//   - kicking=3, kicking_threshold=20, duration=150, min_number_to_kick=3,
//     bandwidth_threshold=6
//   - eval_auth_req=0, eval_assoc_req=0, eval_probe_req=0
//   - network_option=2, hostapd_dir=/var/run/hostapd
//   - 802.11k=1, 802.11v=1, 802.11r=1, bss_transition=1, ft_over_ds=0,
//     ft_psk_generate_local=1, mobility_domain común
//   - random_bssid=0 en todas las radios
package orchestr

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gnacho/netpulse/agent/executor"
)

// DawnDesired es el estado deseado del módulo DAWN.
type DawnDesired struct {
	Enabled            bool   `json:"enabled"`
	SSID               string `json:"ssid,omitempty"`               // SSID a gestionar; vacío = todas las de modo AP
	MobilityDomain     string `json:"mobilityDomain,omitempty"`     // p. ej. "2025"
	BroadcastIP        string `json:"broadcastIp,omitempty"`        // p. ej. "192.168.1.255"
	TCPPort            string `json:"tcpPort,omitempty"`            // default "1026"
	SharedKey          string `json:"sharedKey,omitempty"`          // clave compartida de la malla DAWN
	IV                 string `json:"iv,omitempty"`                 // IV de la malla DAWN
	Kicking            string `json:"kicking,omitempty"`            // default "3"
	KickingThreshold   string `json:"kickingThreshold,omitempty"`   // default "20"
	Duration           string `json:"duration,omitempty"`           // default "150"
	MinNumberToKick    string `json:"minNumberToKick,omitempty"`    // default "3"
	BandwidthThreshold string `json:"bandwidthThreshold,omitempty"` // default "6"
	EvalAuthReq        string `json:"evalAuthReq,omitempty"`      // default "0"
	EvalAssocReq       string `json:"evalAssocReq,omitempty"`     // default "0"
	EvalProbeReq       string `json:"evalProbeReq,omitempty"`     // default "0"
	IEEE80211K         bool   `json:"ieee80211k,omitempty"`
	IEEE80211R         bool   `json:"ieee80211r,omitempty"`
	IEEE80211V         bool   `json:"ieee80211v,omitempty"`
	BSSTransition      bool   `json:"bssTransition,omitempty"`
	FTOverDS           bool   `json:"ftOverDs,omitempty"`
	FTPskGenerateLocal bool   `json:"ftPskGenerateLocal,omitempty"`
}

// DawnSSID describe una sección wifi-iface.
type DawnSSID struct {
	Section            string
	SSID               string
	Mode               string
	Device             string
	IEEE80211K         string
	IEEE80211R         string
	IEEE80211V         string
	BSSTransition      string
	FTOverDS           string
	FTPskGenerateLocal string
	MobilityDomain     string
}

// DawnRadio describe una sección wifi-device.
type DawnRadio struct {
	Section     string
	RandomBSSID string
}

// DawnScenario describe el estado detectado de DAWN en un router.
type DawnScenario struct {
	Manager       string        // apk | opkg | ""
	DawnInstalled bool
	DawnRunning   bool
	WpadVariant   string        // basic | mbedtls | wolfssl | openssl | other | ""
	HostapdDirOK  bool
	Global        map[string]string
	SSIDs         []DawnSSID
	Radios        []DawnRadio
}

const probeDawnCmd = `echo '===PKG_MGR==='
[ -x /usr/bin/apk ] && echo apk || echo opkg
echo '===DAWN_INST==='
[ -x /etc/init.d/dawn ] && echo yes || echo no
echo '===DAWN_RUN==='
/etc/init.d/dawn running >/dev/null 2>&1 && echo yes || echo no
echo '===WPAD==='
opkg list-installed 2>/dev/null | grep -E '^wpad-' || true
apk list --installed 2>/dev/null | grep -E '^wpad-' || true
echo '===HOSTAPD_DIR==='
[ -d /var/run/hostapd ] && echo yes || echo no
echo '===DAWN_UCI==='
uci show dawn 2>/dev/null
echo '===WIRELESS_UCI==='
uci show wireless 2>/dev/null
echo '===END==='`

// DetectDawn ejecuta el probe combinado vía SSH y devuelve el escenario.
func DetectDawn(run CommandRunner, host string) (DawnScenario, error) {
	out, err := run.Run(host, probeDawnCmd, 10*time.Second)
	if err != nil {
		return DawnScenario{}, err
	}
	return parseDawn(out), nil
}

func parseDawn(out string) DawnScenario {
	sc := DawnScenario{Global: map[string]string{}}
	sections := splitSections(out)

	sc.Manager = strings.TrimSpace(firstNonEmpty(sections["PKG_MGR"]))
	if sc.Manager == "" {
		sc.Manager = "opkg"
	}
	sc.DawnInstalled = strings.TrimSpace(firstNonEmpty(sections["DAWN_INST"])) == "yes"
	sc.DawnRunning = strings.TrimSpace(firstNonEmpty(sections["DAWN_RUN"])) == "yes"
	sc.HostapdDirOK = strings.TrimSpace(firstNonEmpty(sections["HOSTAPD_DIR"])) == "yes"
	sc.WpadVariant = parseWpadVariant(sections["WPAD"])

	for _, line := range sections["DAWN_UCI"] {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "dawn.global.") {
			continue
		}
		rest := strings.TrimPrefix(line, "dawn.global.")
		parts := strings.SplitN(rest, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		val := strings.Trim(parts[1], "'")
		sc.Global[key] = val
	}

	sc.SSIDs, sc.Radios = parseWirelessDawn(sections["WIRELESS_UCI"])
	return sc
}

var reWpadLine = regexp.MustCompile(`^wpad-([a-z]+)`)

func parseWpadVariant(lines []string) string {
	for _, line := range lines {
		line = strings.ToLower(strings.TrimSpace(line))
		if m := reWpadLine.FindStringSubmatch(line); m != nil {
			return m[1]
		}
	}
	return ""
}

func parseWirelessDawn(lines []string) (ssids []DawnSSID, radios []DawnRadio) {
	uci := strings.Join(lines, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "wireless.") {
			continue
		}
		rest := strings.TrimPrefix(line, "wireless.")
		parts := strings.SplitN(rest, "=", 2)
		if len(parts) != 2 {
			continue
		}
		secFull := parts[0]
		typ := strings.Trim(parts[1], "'")
		if typ == "wifi-iface" {
			ssids = append(ssids, DawnSSID{
				Section:            secFull,
				SSID:               wirelessOpt(uci, secFull, "ssid"),
				Mode:               wirelessOpt(uci, secFull, "mode"),
				Device:             wirelessOpt(uci, secFull, "device"),
				IEEE80211K:         wirelessOpt(uci, secFull, "ieee80211k"),
				IEEE80211R:         wirelessOpt(uci, secFull, "ieee80211r"),
				IEEE80211V:         wirelessOpt(uci, secFull, "ieee80211v"),
				BSSTransition:      wirelessOpt(uci, secFull, "bss_transition"),
				FTOverDS:           wirelessOpt(uci, secFull, "ft_over_ds"),
				FTPskGenerateLocal: wirelessOpt(uci, secFull, "ft_psk_generate_local"),
				MobilityDomain:     wirelessOpt(uci, secFull, "mobility_domain"),
			})
		} else if typ == "wifi-device" {
			radios = append(radios, DawnRadio{
				Section:     secFull,
				RandomBSSID: wirelessOpt(uci, secFull, "random_bssid"),
			})
		}
	}
	return
}

func wirelessOpt(uci, sec, opt string) string {
	prefix := "wireless." + sec + "." + opt + "="
	for _, line := range strings.Split(uci, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.Trim(strings.TrimPrefix(line, prefix), "'")
		}
	}
	return ""
}

// DawnOps genera las Ops para reconciliar el estado deseado de DAWN.
func DawnOps(desired DawnDesired, sc DawnScenario) []executor.Op {
	if !desired.Enabled {
		return dawnDisableOps(sc)
	}
	d := normalizeDawnDesired(desired)
	var ops []executor.Op

	// 1. Instalar DAWN si falta.
	if !sc.DawnInstalled {
		if sc.Manager == "apk" {
			ops = append(ops, executor.Op{Kind: "apk_install", Args: map[string]string{"package": "dawn"}, Desc: "Install DAWN (apk)"})
			ops = append(ops, executor.Op{Kind: "apk_install", Args: map[string]string{"package": "luci-app-dawn"}, Desc: "Install LuCI DAWN (apk)"})
		} else {
			ops = append(ops, executor.Op{Kind: "install", Args: map[string]string{"package": "dawn"}, Desc: "Install DAWN (opkg)"})
			ops = append(ops, executor.Op{Kind: "install", Args: map[string]string{"package": "luci-app-dawn"}, Desc: "Install LuCI DAWN (opkg)"})
		}
	}

	// 2. Asegurar wpad completo si solo hay wpad-basic/mini.
	if sc.WpadVariant == "basic" || sc.WpadVariant == "mini" || sc.WpadVariant == "" {
		pkg := "wpad-wolfssl"
		if sc.Manager == "apk" {
			pkg = "wpad-mbedtls"
		}
		ops = append(ops, executor.Op{Kind: installKind(sc.Manager), Args: map[string]string{"package": pkg}, Desc: "Install full wpad for DAWN"})
	}

	// 3. Asegurar la sección dawn.global (named) y options.
	if len(sc.Global) == 0 {
		ops = append(ops, executor.Op{Kind: "uci_set_named", Args: map[string]string{"config": "dawn", "section": "global", "type": "global"}, Desc: "Ensure DAWN global section"})
	}
	setDawnGlobalIfDiffers(&ops, sc, "kicking", d.Kicking)
	setDawnGlobalIfDiffers(&ops, sc, "kicking_threshold", d.KickingThreshold)
	setDawnGlobalIfDiffers(&ops, sc, "duration", d.Duration)
	setDawnGlobalIfDiffers(&ops, sc, "min_number_to_kick", d.MinNumberToKick)
	setDawnGlobalIfDiffers(&ops, sc, "bandwidth_threshold", d.BandwidthThreshold)
	setDawnGlobalIfDiffers(&ops, sc, "eval_auth_req", d.EvalAuthReq)
	setDawnGlobalIfDiffers(&ops, sc, "eval_assoc_req", d.EvalAssocReq)
	setDawnGlobalIfDiffers(&ops, sc, "eval_probe_req", d.EvalProbeReq)
	setDawnGlobalIfDiffers(&ops, sc, "network_option", "2")
	setDawnGlobalIfDiffers(&ops, sc, "hostapd_dir", "/var/run/hostapd")
	setDawnGlobalIfDiffers(&ops, sc, "set_hostapd_nr", "1")
	setDawnGlobalIfDiffers(&ops, sc, "rrm_mode", "pat")
	setDawnGlobalIfDiffers(&ops, sc, "tcp_port", d.TCPPort)
	setDawnGlobalIfDiffers(&ops, sc, "shared_key", d.SharedKey)
	setDawnGlobalIfDiffers(&ops, sc, "iv", d.IV)
	if d.BroadcastIP != "" {
		setDawnGlobalIfDiffers(&ops, sc, "broadcast_ip", d.BroadcastIP)
	}

	// 4. Wireless: aplicar baseline a cada SSID de modo AP.
	for _, s := range sc.SSIDs {
		if s.Mode != "ap" {
			continue
		}
		if d.SSID != "" && s.SSID != d.SSID {
			continue
		}
		sec := s.Section
		setWirelessBoolIfDiffers(&ops, s.IEEE80211K, sec, "ieee80211k", d.IEEE80211K, true)
		setWirelessBoolIfDiffers(&ops, s.IEEE80211R, sec, "ieee80211r", d.IEEE80211R, true)
		setWirelessBoolIfDiffers(&ops, s.IEEE80211V, sec, "ieee80211v", d.IEEE80211V, true)
		setWirelessBoolIfDiffers(&ops, s.BSSTransition, sec, "bss_transition", d.BSSTransition, true)
		setWirelessBoolIfDiffers(&ops, s.FTOverDS, sec, "ft_over_ds", d.FTOverDS, false)
		setWirelessBoolIfDiffers(&ops, s.FTPskGenerateLocal, sec, "ft_psk_generate_local", d.FTPskGenerateLocal, true)
		if d.MobilityDomain != "" && s.MobilityDomain != d.MobilityDomain {
			ops = append(ops, executor.Op{Kind: "uci_set", Args: map[string]string{"config": "wireless", "section": sec, "option": "mobility_domain", "value": d.MobilityDomain}, Desc: "Set mobility domain on " + sec})
		}
	}

	// 5. Radios: random_bssid=0.
	for _, r := range sc.Radios {
		if r.RandomBSSID != "0" {
			ops = append(ops, executor.Op{Kind: "uci_set", Args: map[string]string{"config": "wireless", "section": r.Section, "option": "random_bssid", "value": "0"}, Desc: "Disable random BSSID on " + r.Section})
		}
	}

	if len(ops) == 0 {
		return nil
	}

	ops = append(ops, executor.Op{Kind: "uci_commit", Args: map[string]string{"config": "dawn"}, Desc: "Commit DAWN config"})
	ops = append(ops, executor.Op{Kind: "uci_commit", Args: map[string]string{"config": "wireless"}, Desc: "Commit wireless config"})
	ops = append(ops, executor.Op{Kind: "service", Args: map[string]string{"name": "dawn", "action": "enable"}, Desc: "Enable DAWN on boot"})
	ops = append(ops, executor.Op{Kind: "service", Args: map[string]string{"name": "dawn", "action": "start"}, Desc: "Start DAWN"})
	return ops
}

func setDawnGlobalIfDiffers(ops *[]executor.Op, sc DawnScenario, option, want string) {
	if want == "" {
		return
	}
	if sc.Global[option] == want {
		return
	}
	*ops = append(*ops, executor.Op{Kind: "uci_set", Args: map[string]string{"config": "dawn", "section": "global", "option": option, "value": want}, Desc: "Set DAWN " + option})
}

func setWirelessBoolIfDiffers(ops *[]executor.Op, current, sec, option string, want, defaultVal bool) {
	val := boolString(want, defaultVal)
	if current == val {
		return
	}
	*ops = append(*ops, executor.Op{Kind: "uci_set", Args: map[string]string{"config": "wireless", "section": sec, "option": option, "value": val}, Desc: "Set " + option + " on " + sec})
}

func boolString(want, defaultVal bool) string {
	if want || defaultVal {
		return "1"
	}
	return "0"
}

func dawnDisableOps(sc DawnScenario) []executor.Op {
	if !sc.DawnInstalled && len(sc.Global) == 0 {
		return nil
	}
	return []executor.Op{
		{Kind: "uci_set", Args: map[string]string{"config": "dawn", "section": "global", "option": "enabled", "value": "0"}, Desc: "Disable DAWN"},
		{Kind: "uci_commit", Args: map[string]string{"config": "dawn"}, Desc: "Commit DAWN config"},
		{Kind: "service", Args: map[string]string{"name": "dawn", "action": "stop"}, Desc: "Stop DAWN"},
		{Kind: "service", Args: map[string]string{"name": "dawn", "action": "disable"}, Desc: "Disable DAWN on boot"},
	}
}

func normalizeDawnDesired(d DawnDesired) DawnDesired {
	if d.Kicking == "" {
		d.Kicking = "3"
	}
	if d.KickingThreshold == "" {
		d.KickingThreshold = "20"
	}
	if d.Duration == "" {
		d.Duration = "150"
	}
	if d.MinNumberToKick == "" {
		d.MinNumberToKick = "3"
	}
	if d.BandwidthThreshold == "" {
		d.BandwidthThreshold = "6"
	}
	if d.EvalAuthReq == "" {
		d.EvalAuthReq = "0"
	}
	if d.EvalAssocReq == "" {
		d.EvalAssocReq = "0"
	}
	if d.EvalProbeReq == "" {
		d.EvalProbeReq = "0"
	}
	if d.TCPPort == "" {
		d.TCPPort = "1026"
	}
	return d
}
func installKind(manager string) string {
	if manager == "apk" {
		return "apk_install"
	}
	return "install"
}

// DawnHealthcheck verifica que DAWN responde y ve vecinos tras el apply.
func DawnHealthcheck(run CommandRunner, host string) error {
	out, err := run.Run(host, "ubus call dawn get_network", 5*time.Second)
	if err != nil {
		return fmt.Errorf("dawn get_network failed: %w", err)
	}
	out = strings.TrimSpace(out)
	if out == "" || out == "{}" {
		return fmt.Errorf("dawn get_network returned empty mesh")
	}
	return nil
}

// DawnDriftWarnings devuelve advertencias de inconsistencia legibles para la UI.
func DawnDriftWarnings(d DawnDesired, sc DawnScenario) []string {
	d = normalizeDawnDesired(d)
	var warns []string
	if !sc.DawnInstalled {
		warns = append(warns, "DAWN no está instalado")
	}
	if !sc.HostapdDirOK {
		warns = append(warns, "El directorio /var/run/hostapd no existe")
	}
	mdom := d.MobilityDomain
	for _, s := range sc.SSIDs {
		if s.Mode != "ap" {
			continue
		}
		if d.SSID != "" && s.SSID != d.SSID {
			continue
		}
		if mdom != "" && s.MobilityDomain != mdom {
			warns = append(warns, "Dominio de movilidad inconsistente en "+s.Section)
		}
		if s.IEEE80211K != "1" {
			warns = append(warns, "802.11k desactivado en "+s.Section)
		}
		if s.IEEE80211R != "1" {
			warns = append(warns, "802.11r desactivado en "+s.Section)
		}
		if s.IEEE80211V != "1" {
			warns = append(warns, "802.11v desactivado en "+s.Section)
		}
	}
	for _, r := range sc.Radios {
		if r.RandomBSSID != "0" {
			warns = append(warns, "random_bssid activo en "+r.Section)
		}
	}
	return warns
}

// DawnMethod devuelve active/inactive para la UI.
func DawnMethod(sc DawnScenario) string {
	if sc.DawnRunning {
		return "active"
	}
	return "inactive"
}

// Int helpers para comparar valores numéricos si fuese necesario.
func dawnInt(s string) int {
	v, _ := strconv.Atoi(strings.TrimSpace(s))
	return v
}

func validateDawnOps(ops []executor.Op) error {
	for _, op := range ops {
		if err := executor.Validate(op); err != nil {
			return fmt.Errorf("%s: %w", op.Desc, err)
		}
	}
	return nil
}
