// usteer.go - modulo de orquestacion "usteer".
//
// Instala/configura usteer (daemon oficial de OpenWrt para roaming/steering)
// y alinea los parametros de roaming 802.11k/v/r en los APs. El modulo es
// declarativo: compara el estado detectado contra un baseline deseado y
// genera las Ops allowlistedas necesarias.
//
// Baseline por defecto (verificado en red Mandor):
//   - aggressiveness=3 (BSS-transition-request con disassociation imminent y timer)
//   - band_steering_threshold=5
//   - load_balancing_threshold=0
//   - debug_level=2, syslog=1, network=lan, enabled=1
//   - 802.11k=1, 802.11v=1, 802.11r=1, bss_transition=1, ft_over_ds=0,
//     ft_psk_generate_local=1, mobility_domain comun
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

// UsteerDesired es el estado deseado del modulo usteer.
type UsteerDesired struct {
	Enabled                bool   `json:"enabled"`
	SSID                   string `json:"ssid,omitempty"`               // SSID a gestionar; vacio = todas las de modo AP
	MobilityDomain         string `json:"mobilityDomain,omitempty"`       // p. ej. "2025"
	Aggressiveness         string `json:"aggressiveness,omitempty"`         // default "3"
	BandSteeringThreshold  string `json:"bandSteeringThreshold,omitempty"`  // default "5"
	LoadBalancingThreshold string `json:"loadBalancingThreshold,omitempty"` // default "0"
	DebugLevel             string `json:"debugLevel,omitempty"`             // default "2"
	IEEE80211K             bool   `json:"ieee80211k,omitempty"`
	IEEE80211R             bool   `json:"ieee80211r,omitempty"`
	IEEE80211V             bool   `json:"ieee80211v,omitempty"`
	BSSTransition          bool   `json:"bssTransition,omitempty"`
	FTOverDS               bool   `json:"ftOverDs,omitempty"`
	FTPskGenerateLocal     bool   `json:"ftPskGenerateLocal,omitempty"`
}

// UsteerSSID describe una seccion wifi-iface.
type UsteerSSID struct {
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

// UsteerRadio describe una seccion wifi-device.
type UsteerRadio struct {
	Section     string
	RandomBSSID string
}

// UsteerScenario describe el estado detectado de usteer en un router.
type UsteerScenario struct {
	Manager        string            // apk | opkg | ""
	UsteerInstalled bool
	UsteerRunning   bool
	WpadVariant    string            // basic | mbedtls | wolfssl | openssl | other | ""
	HostapdDirOK   bool
	Config         map[string]string // usteer.@usteer[-1] options
	SSIDs          []UsteerSSID
	Radios         []UsteerRadio
}

const probeUsteerCmd = `echo '===PKG_MGR==='
[ -x /usr/bin/apk ] && echo apk || echo opkg
echo '===USTEER_INST==='
[ -x /etc/init.d/usteer ] && echo yes || echo no
echo '===USTEER_RUN==='
/etc/init.d/usteer running >/dev/null 2>&1 && echo yes || echo no
echo '===WPAD==='
opkg list-installed 2>/dev/null | grep -E '^wpad-' || true
apk list --installed 2>/dev/null | grep -E '^wpad-' || true
echo '===HOSTAPD_DIR==='
[ -d /var/run/hostapd ] && echo yes || echo no
echo '===USTEER_UCI==='
uci show usteer 2>/dev/null
echo '===WIRELESS_UCI==='
uci show wireless 2>/dev/null
echo '===END==='`

// DetectUsteer ejecuta el probe combinado via SSH y devuelve el escenario.
func DetectUsteer(run CommandRunner, host string) (UsteerScenario, error) {
	out, err := run.Run(host, probeUsteerCmd, 10*time.Second)
	if err != nil {
		return UsteerScenario{}, err
	}
	return parseUsteer(out), nil
}

func parseUsteer(out string) UsteerScenario {
	sc := UsteerScenario{Config: map[string]string{}}
	sections := splitSections(out)

	sc.Manager = strings.TrimSpace(firstNonEmpty(sections["PKG_MGR"]))
	if sc.Manager == "" {
		sc.Manager = "opkg"
	}
	sc.UsteerInstalled = strings.TrimSpace(firstNonEmpty(sections["USTEER_INST"])) == "yes"
	sc.UsteerRunning = strings.TrimSpace(firstNonEmpty(sections["USTEER_RUN"])) == "yes"
	sc.HostapdDirOK = strings.TrimSpace(firstNonEmpty(sections["HOSTAPD_DIR"])) == "yes"
	sc.WpadVariant = parseWpadVariant(sections["WPAD"])

	for _, line := range sections["USTEER_UCI"] {
		line = strings.TrimSpace(line)
		// usteer.@usteer[N].option='value'
		m := reUsteerOption.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		sc.Config[m[1]] = strings.Trim(m[2], "'")
	}

	sc.SSIDs, sc.Radios = parseWirelessUsteer(sections["WIRELESS_UCI"])
	return sc
}

// reUsteerOption captura `usteer.@usteer[N].option='value'`. `uci show usteer`
// imprime la sección anónima con su índice real (`@usteer[0]`), no con la
// referencia `[-1]` que solo vale para `uci set`. Se aceptan ambas formas.
var reUsteerOption = regexp.MustCompile(`^usteer\.@usteer\[-?\d+\]\.([a-z0-9_]+)=(.+)$`)

func parseWirelessUsteer(lines []string) (ssids []UsteerSSID, radios []UsteerRadio) {
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
			ssids = append(ssids, UsteerSSID{
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
			radios = append(radios, UsteerRadio{
				Section:     secFull,
				RandomBSSID: wirelessOpt(uci, secFull, "random_bssid"),
			})
		}
	}
	return
}

// UsteerOps genera las Ops para reconciliar el estado deseado de usteer.
func UsteerOps(desired UsteerDesired, sc UsteerScenario) []executor.Op {
	if !desired.Enabled {
		return usteerDisableOps(sc)
	}
	d := normalizeUsteerDesired(desired)
	var ops []executor.Op

	// 1. Instalar usteer si falta.
	if !sc.UsteerInstalled {
		if sc.Manager == "apk" {
			ops = append(ops, executor.Op{Kind: "apk_install", Args: map[string]string{"package": "usteer"}, Desc: "Install usteer (apk)"})
			ops = append(ops, executor.Op{Kind: "apk_install", Args: map[string]string{"package": "luci-app-usteer"}, Desc: "Install LuCI usteer (apk)"})
		} else {
			ops = append(ops, executor.Op{Kind: "install", Args: map[string]string{"package": "usteer"}, Desc: "Install usteer (opkg)"})
			ops = append(ops, executor.Op{Kind: "install", Args: map[string]string{"package": "luci-app-usteer"}, Desc: "Install LuCI usteer (opkg)"})
		}
	}

	// 2. Asegurar wpad completo si solo hay wpad-basic/mini.
	if sc.WpadVariant == "basic" || sc.WpadVariant == "mini" || sc.WpadVariant == "" {
		pkg := "wpad-wolfssl"
		if sc.Manager == "apk" {
			pkg = "wpad-mbedtls"
		}
		ops = append(ops, executor.Op{Kind: installKind(sc.Manager), Args: map[string]string{"package": pkg}, Desc: "Install full wpad for usteer"})
	}

	// 3. Asegurar la seccion usteer y options.
	if len(sc.Config) == 0 {
		ops = append(ops, executor.Op{Kind: "uci_set_named", Args: map[string]string{"config": "usteer", "section": "usteer", "type": "usteer"}, Desc: "Ensure usteer section"})
	}
	setUsteerOptionIfDiffers(&ops, sc, "enabled", "1")
	setUsteerOptionIfDiffers(&ops, sc, "network", "lan")
	setUsteerOptionIfDiffers(&ops, sc, "syslog", "1")
	setUsteerOptionIfDiffers(&ops, sc, "debug_level", d.DebugLevel)
	setUsteerOptionIfDiffers(&ops, sc, "aggressiveness", d.Aggressiveness)
	setUsteerOptionIfDiffers(&ops, sc, "band_steering_threshold", d.BandSteeringThreshold)
	setUsteerOptionIfDiffers(&ops, sc, "load_balancing_threshold", d.LoadBalancingThreshold)
	setUsteerOptionIfDiffers(&ops, sc, "min_connect_snr", "0")
	setUsteerOptionIfDiffers(&ops, sc, "min_snr", "0")
	setUsteerOptionIfDiffers(&ops, sc, "roam_trigger_snr", "0")
	setUsteerOptionIfDiffers(&ops, sc, "roam_scan_snr", "0")

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

	ops = append(ops, executor.Op{Kind: "uci_commit", Args: map[string]string{"config": "usteer"}, Desc: "Commit usteer config"})
	ops = append(ops, executor.Op{Kind: "uci_commit", Args: map[string]string{"config": "wireless"}, Desc: "Commit wireless config"})
	ops = append(ops, executor.Op{Kind: "service", Args: map[string]string{"name": "usteer", "action": "enable"}, Desc: "Enable usteer on boot"})
	ops = append(ops, executor.Op{Kind: "service", Args: map[string]string{"name": "usteer", "action": "start"}, Desc: "Start usteer"})
	return ops
}

func setUsteerOptionIfDiffers(ops *[]executor.Op, sc UsteerScenario, option, want string) {
	if want == "" {
		return
	}
	if sc.Config[option] == want {
		return
	}
	*ops = append(*ops, executor.Op{Kind: "uci_set", Args: map[string]string{"config": "usteer", "section": "@usteer[-1]", "option": option, "value": want}, Desc: "Set usteer " + option})
}

func usteerDisableOps(sc UsteerScenario) []executor.Op {
	if !sc.UsteerInstalled && len(sc.Config) == 0 {
		return nil
	}
	return []executor.Op{
		{Kind: "uci_set", Args: map[string]string{"config": "usteer", "section": "@usteer[-1]", "option": "enabled", "value": "0"}, Desc: "Disable usteer"},
		{Kind: "uci_commit", Args: map[string]string{"config": "usteer"}, Desc: "Commit usteer config"},
		{Kind: "service", Args: map[string]string{"name": "usteer", "action": "stop"}, Desc: "Stop usteer"},
		{Kind: "service", Args: map[string]string{"name": "usteer", "action": "disable"}, Desc: "Disable usteer on boot"},
	}
}

func normalizeUsteerDesired(d UsteerDesired) UsteerDesired {
	if d.Aggressiveness == "" {
		d.Aggressiveness = "3"
	}
	if d.BandSteeringThreshold == "" {
		d.BandSteeringThreshold = "5"
	}
	if d.LoadBalancingThreshold == "" {
		d.LoadBalancingThreshold = "0"
	}
	if d.DebugLevel == "" {
		d.DebugLevel = "2"
	}
	return d
}

// UsteerHealthcheck verifica que usteer responde y ve vecinos tras el apply.
func UsteerHealthcheck(run CommandRunner, host string) error {
	out, err := run.Run(host, "ubus call usteer remote_hosts", 5*time.Second)
	if err != nil {
		return fmt.Errorf("usteer remote_hosts failed: %w", err)
	}
	out = strings.TrimSpace(out)
	if out == "" || out == "{}" {
		return fmt.Errorf("usteer remote_hosts returned empty mesh")
	}
	return nil
}

// UsteerDriftWarnings devuelve advertencias de inconsistencia legibles para la UI.
func UsteerDriftWarnings(d UsteerDesired, sc UsteerScenario) []string {
	d = normalizeUsteerDesired(d)
	var warns []string
	if !sc.UsteerInstalled {
		warns = append(warns, "usteer no esta instalado")
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

// UsteerMethod devuelve active/inactive para la UI.
func UsteerMethod(sc UsteerScenario) string {
	if sc.UsteerRunning {
		return "active"
	}
	return "inactive"
}

// Int helpers para comparar valores numericos si fuese necesario.
func usteerInt(s string) int {
	v, _ := strconv.Atoi(strings.TrimSpace(s))
	return v
}

func validateUsteerOps(ops []executor.Op) error {
	for _, op := range ops {
		if err := executor.Validate(op); err != nil {
			return fmt.Errorf("%s: %w", op.Desc, err)
		}
	}
	return nil
}
