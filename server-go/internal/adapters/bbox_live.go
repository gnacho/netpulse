package adapters

// Native read-only client for Bouygues Telecom Bbox routers.  The published
// OpenAPI document describes the paths and the X-BBOX-ID header, but leaves
// response schemas open. The parser therefore accepts the documented
// envelope (an array containing an object) and consumes fields verified from
// API examples and this router's responses. Unknown or missing sections are
// deliberately ignored.

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
)

const bboxTimeout = 10 * time.Second

type bboxClient struct {
	base, password                          string
	http                                    *http.Client
	mu                                      sync.Mutex
	bboxID                                  string
	lastCPU, lastIdle, lastWANRx, lastWANTx uint64
	lastAt                                  time.Time
	lastEndpointFailures                    string
}

func newBboxClient(cfg RouterConfig) *bboxClient {
	return newBboxClientURL("https://"+cfg.Host+"/api/v1", cfg.BboxPassword, cfg.BboxInsecure)
}

func newBboxClientURL(base, password string, insecure bool) *bboxClient {
	jar, _ := cookiejar.New(nil)
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit per-router opt-in
	}
	return &bboxClient{base: strings.TrimRight(base, "/"), password: password,
		http: &http.Client{Jar: jar, Timeout: bboxTimeout, Transport: tr}}
}

func (c *bboxClient) login(ctx context.Context) error {
	v := url.Values{"password": {c.password}, "remember": {"1"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/login", strings.NewReader(v.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("bbox login: HTTP %d", resp.StatusCode)
	}
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "BBOX_ID" {
			c.bboxID = cookie.Value
		}
	}
	if c.bboxID == "" {
		return fmt.Errorf("bbox login: missing BBOX_ID session")
	}
	return nil
}

func (c *bboxClient) get(ctx context.Context, path string, dst any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for attempt := 0; attempt < 2; attempt++ {
		if c.bboxID == "" {
			if err := c.login(ctx); err != nil {
				return err
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
		if err != nil {
			return err
		}
		req.Header.Set("X-BBOX-ID", c.bboxID)
		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			c.bboxID = ""
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("bbox GET %s: HTTP %d", path, resp.StatusCode)
		}
		if err := json.Unmarshal(body, dst); err != nil {
			return fmt.Errorf("bbox GET %s: invalid JSON: %w", path, err)
		}
		return nil
	}
	return fmt.Errorf("bbox GET %s: session expired", path)
}

func (c *bboxClient) logout(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.bboxID == "" {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/logout", bytes.NewReader(nil))
	if err != nil {
		return
	}
	req.Header.Set("X-BBOX-ID", c.bboxID)
	resp, err := c.http.Do(req)
	if err == nil {
		resp.Body.Close()
	}
	c.bboxID = ""
}

func bboxMap(v any) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		return x
	case []any:
		for _, item := range x {
			if m := bboxMap(item); m != nil {
				return m
			}
		}
	}
	return nil
}

func bboxFind(v any, key string) any {
	if m, ok := v.(map[string]any); ok {
		if x, ok := m[key]; ok {
			return x
		}
		for _, x := range m {
			if found := bboxFind(x, key); found != nil {
				return found
			}
		}
	} else if a, ok := v.([]any); ok {
		for _, x := range a {
			if found := bboxFind(x, key); found != nil {
				return found
			}
		}
	}
	return nil
}

func bboxString(v any, keys ...string) string {
	for _, key := range keys {
		if x := bboxFind(v, key); x != nil {
			if s, ok := x.(string); ok {
				return s
			}
			return fmt.Sprint(x)
		}
	}
	return ""
}

func bboxDirectString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if x, ok := m[key]; ok {
			if s, ok := x.(string); ok {
				return s
			}
			return fmt.Sprint(x)
		}
	}
	return ""
}

func parseBboxWANIPs(v any) (ipv4, ipv6 string) {
	var priority4, priority6 int
	walk := func(any) {}
	walk = func(value any) {
		switch item := value.(type) {
		case map[string]any:
			for key, child := range item {
				name := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
				priority := 0
				switch name {
				case "ipaddress", "publicip", "address":
					priority = 1
				case "ipv4", "ipv4address", "publicipv4", "publicipv4address", "ip4address":
					priority = 2
				case "ipv6", "ipv6address", "publicipv6", "publicipv6address", "ip6address":
					priority = 2
				}
				if priority > 0 {
					if address, ok := child.(string); ok {
						parsed := net.ParseIP(strings.TrimSpace(address))
						if parsed != nil {
							if parsed.To4() != nil && priority > priority4 {
								ipv4, priority4 = parsed.String(), priority
							} else if parsed.To4() == nil && priority > priority6 {
								ipv6, priority6 = parsed.String(), priority
							}
						}
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range item {
				walk(child)
			}
		}
	}
	walk(v)
	return ipv4, ipv6
}

func parseBboxWANDNSServers(v any) []string {
	ip := bboxMap(bboxFind(bboxMap(v), "ip"))
	seen := make(map[string]bool)
	var servers []string
	for _, key := range []string{"dnsservers", "dnsserversv6"} {
		for _, raw := range strings.Split(bboxDirectString(ip, key), ",") {
			address := strings.TrimSpace(raw)
			parsed := net.ParseIP(address)
			if parsed == nil {
				continue
			}
			address = parsed.String()
			if !seen[address] {
				servers = append(servers, address)
				seen[address] = true
			}
		}
	}
	return servers
}

func bboxUint(v any, keys ...string) uint64 {
	var raw any
	for _, key := range keys {
		if found := bboxFind(v, key); found != nil {
			raw = found
			break
		}
	}
	value, _ := bboxUnsigned(raw)
	return value
}

func bboxUnsigned(raw any) (uint64, bool) {
	var s string
	switch n := raw.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || math.Trunc(n) != n {
			return 0, false
		}
		s = strconv.FormatFloat(n, 'f', 0, 64)
	case json.Number:
		s = n.String()
	case string:
		s = n
	default:
		s = fmt.Sprint(raw)
	}
	value, err := strconv.ParseUint(s, 10, 64)
	return value, err == nil
}

func bboxInt(v any, keys ...string) int {
	n, _ := strconv.Atoi(bboxString(v, keys...))
	return n
}

type bboxHost struct {
	MAC, IP, Name, Band string
	Signal              int
}

func bboxRadioBand(v any) string {
	s := strings.ToLower(strings.TrimSpace(bboxString(v, "band")))
	switch s {
	case "2.4", "2.4ghz", "2.4 ghz":
		return "2.4 GHz"
	case "5", "5ghz", "5 ghz":
		return "5 GHz"
	case "6", "6ghz", "6 ghz":
		return "6 GHz"
	default:
		return ""
	}
}

func parseBboxHosts(v any) []bboxHost {
	var out []bboxHost
	root := bboxMap(v)
	if root == nil {
		return out
	}
	var records []any
	hosts := root["hosts"]
	if envelope := bboxMap(hosts); envelope != nil {
		if list, ok := envelope["list"].([]any); ok {
			records = list
		}
	}
	if records == nil {
		records, _ = hosts.([]any)
	}
	// Some variants return the list directly under the top-level array.
	if records == nil {
		records, _ = root["list"].([]any)
	}
	for _, item := range records {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if active, exists := record["active"]; exists && bboxInt(map[string]any{"active": active}, "active") != 1 {
			continue
		}
		mac := strings.ToUpper(strings.ReplaceAll(bboxDirectString(record, "macaddress", "mac"), "-", ":"))
		if mac == "" || !strings.Contains(mac, ":") {
			continue
		}
		wireless := bboxMap(record["wireless"])
		band := bboxRadioBand(wireless)
		if band == "" {
			band = bboxRadioBand(record)
		}
		signal := bboxInt(wireless, "rssi0", "signal", "signalstrength")
		if signal == 0 {
			signal = bboxInt(record, "signal", "signalstrength")
		}
		out = append(out, bboxHost{MAC: mac, IP: bboxDirectString(record, "ipaddress", "ip"), Name: bboxDirectString(record, "hostname", "name"), Band: band, Signal: signal})
	}
	return out
}

// parseBboxRadios consumes radio state only. In particular it never walks the
// SSID security objects, which contain Wi-Fi passphrases in some firmware.
func parseBboxRadios(v any) []Radio {
	root := bboxMap(v)
	radioConfig := bboxMap(bboxFind(root, "radio"))
	ssidConfig := bboxMap(bboxFind(root, "ssid"))
	bands := []struct{ key, name string }{{"24", "2.4 GHz"}, {"5", "5 GHz"}, {"6", "6 GHz"}}
	falseKnown := false
	var out []Radio
	for _, band := range bands {
		cfg := bboxMap(radioConfig[band.key])
		if cfg == nil || bboxInt(cfg, "enable") == 0 || bboxInt(cfg, "state") == 0 {
			continue
		}
		channel := bboxInt(cfg, "current_channel")
		if channel == 0 {
			channel = bboxInt(cfg, "channel")
		}
		ssid := bboxMap(ssidConfig[band.key])
		out = append(out, Radio{
			Name: band.name, Channel: channel, WidthMhz: bboxInt(cfg, "current_bandwidth"),
			BSSID: bboxDirectString(ssid, "bssid"), PowerKnown: &falseKnown, ClientsKnown: &falseKnown,
		})
	}
	return out
}

func parseBboxRadioStats(responses ...any) map[string]map[string]*uint64 {
	keys := []string{"rxBytes", "txBytes", "rxPackets", "txPackets", "rxErrors", "txErrors", "rxDiscards", "txDiscards"}
	bands := []string{"2.4 GHz", "5 GHz", "6 GHz"}
	out := make(map[string]map[string]*uint64, len(responses))
	for i, response := range responses {
		if i >= len(bands) {
			break
		}
		ssid := bboxMap(bboxFind(bboxMap(response), "ssid"))
		stats := bboxMap(ssid["stats"])
		if stats == nil {
			continue
		}
		values := make(map[string]*uint64, len(keys))
		for _, direction := range []struct{ api, prefix string }{{"rx", "rx"}, {"tx", "tx"}} {
			m := bboxMap(stats[direction.api])
			for _, field := range []struct{ api, suffix string }{{"bytes", "Bytes"}, {"packets", "Packets"}, {"packetserrors", "Errors"}, {"packetsdiscards", "Discards"}} {
				raw, exists := m[field.api]
				if !exists || raw == nil {
					continue
				}
				n, ok := bboxUnsigned(raw)
				if !ok {
					continue
				}
				key := direction.prefix + field.suffix
				values[key] = &n
			}
		}
		if len(values) > 0 {
			out[bands[i]] = values
		}
	}
	return out
}

func bboxScaledMeasurement(v any, key string, scale, min, max float64) *float64 {
	n, err := strconv.ParseFloat(bboxString(v, key), 64)
	if err != nil {
		return nil
	}
	n /= scale
	if n < min || n > max {
		return nil
	}
	return &n
}

func bboxCPUTemperatureC(v any) (int, bool) {
	cpu := bboxMap(bboxFind(bboxMap(v), "cpu"))
	temperature := bboxMap(cpu["temperature"])
	value := bboxScaledMeasurement(temperature, "main", 1000, 0, 150)
	if value == nil {
		return 0, false
	}
	return int(math.Round(*value)), true
}

// Bbox's time.total is cumulative busy time (the exporter treats it as the
// sum of user/nice/system/io/irq); idle is a separate cumulative counter.
func bboxCPUUsagePercent(total, idle uint64, c *bboxClient) int {
	percent := 0
	if !c.lastAt.IsZero() && total >= c.lastCPU && idle >= c.lastIdle {
		busyDelta := total - c.lastCPU
		idleDelta := idle - c.lastIdle
		elapsed := float64(busyDelta) + float64(idleDelta)
		if elapsed > 0 {
			percent = int(float64(busyDelta) / elapsed * 100)
		}
	}
	c.lastCPU, c.lastIdle = total, idle
	return percent
}

func parseBboxFTTH(sfpResponse, ftthResponse any) *probe.FTTHInfo {
	sfp := bboxMap(bboxFind(bboxMap(sfpResponse), "sfp"))
	access := bboxMap(bboxFind(bboxMap(ftthResponse), "ftth"))
	if sfp == nil && access == nil {
		return nil
	}
	var pon map[string]any
	var mode, moduleClass, state, status string
	if sfp != nil {
		pon = bboxMap(sfp["pon_mode"])
		mode, _ = sfp["pon_mode"].(string)
		moduleClass = bboxDirectString(sfp, "pon_moduleclass")
		state = bboxDirectString(sfp, "pon_internalstatus")
		status = bboxDirectString(sfp, "status_sfp")
	}
	if pon != nil {
		if mode == "" {
			mode = bboxDirectString(pon, "type")
		}
		if moduleClass == "" {
			moduleClass = bboxDirectString(pon, "moduleclass")
		}
		if state == "" {
			state = bboxDirectString(pon, "internalstatus")
		}
	}
	out := &probe.FTTHInfo{
		Mode: mode, ModuleClass: moduleClass, State: state, SFPStatus: status,
		AccessMode: bboxDirectString(access, "mode"), AccessState: bboxDirectString(access, "state"),
	}
	if sfp != nil && strings.EqualFold(status, "up") && bboxInt(sfp, "enable") != 0 {
		// Firmware examples consistently use milli-dBm for optical power,
		// tenths of a degree Celsius, and tenths of a volt. Reject out-of-range
		// sentinel readings (often returned while the PON link is down).
		out.ReceivePowerDbm = bboxScaledMeasurement(sfp, "receive_power", 1000, -40, 10)
		out.TransmitPowerDbm = bboxScaledMeasurement(sfp, "transmit_power", 1000, -40, 20)
		out.TemperatureC = bboxScaledMeasurement(sfp, "temperature", 10, -40, 125)
		out.VoltageV = bboxScaledMeasurement(sfp, "voltage", 10, 0.5, 6)
	}
	return out
}

// parseBboxPorts maps the physical switch ports reported by the Bbox API.
// Only the API-reported negotiated link mode is exposed as speed; hardware
// capacity is not inferred from port IDs or model names.
func parseBboxPorts(v, statsResponse any) []EthPort {
	lan := bboxMap(bboxFind(bboxMap(v), "switch"))
	rawPorts, _ := lan["ports"].([]any)
	portStats := bboxMap(bboxFind(bboxMap(statsResponse), "stats"))
	rawStats, _ := portStats["port"].([]any)
	rateByPort := make(map[int]NetDevBps, len(rawStats))
	for _, raw := range rawStats {
		item := bboxMap(raw)
		id := bboxInt(item, "index")
		rx, rxOK := bboxBandwidthBps(bboxMap(item["rx"]))
		tx, txOK := bboxBandwidthBps(bboxMap(item["tx"]))
		rate := NetDevBps{}
		if rxOK {
			rate.RxBps = &rx
		}
		if txOK {
			rate.TxBps = &tx
		}
		rateByPort[id] = rate
	}
	out := make([]EthPort, 0, len(rawPorts))
	for _, raw := range rawPorts {
		p := bboxMap(raw)
		if p == nil {
			continue
		}
		id := bboxInt(p, "id")
		if id < 1 {
			continue
		}
		state := strings.EqualFold(bboxDirectString(p, "state"), "up")
		port := EthPort{ID: strconv.Itoa(id), Label: fmt.Sprintf("LAN %d", id), Up: state}
		if state {
			port.Speed = bboxLinkMode(bboxDirectString(p, "link_mode"))
		}
		if rate, ok := rateByPort[id]; ok {
			if rate.RxBps != nil {
				port.RxBps = *rate.RxBps
			}
			if rate.TxBps != nil {
				port.TxBps = *rate.TxBps
			}
		}
		out = append(out, port)
	}
	return out
}

func bboxLinkMode(mode string) string {
	s := strings.ToLower(strings.TrimSpace(mode))
	for _, rate := range []struct{ prefixes []string; display string }{
		{[]string{"10baset"}, "10 Mbps"},
		{[]string{"100baset"}, "100 Mbps"},
		{[]string{"1000baset", "1000base"}, "1 Gbps"},
		{[]string{"2500baset", "2.5gbase"}, "2.5 Gbps"},
		{[]string{"5000baset", "5gbase"}, "5 Gbps"},
		{[]string{"10000baset", "10gbase"}, "10 Gbps"},
	} {
		for _, prefix := range rate.prefixes {
			if strings.HasPrefix(s, prefix) {
				return rate.display
			}
		}
	}
	return ""
}

// bboxBandwidthBps converts the Bbox WAN bandwidth field (kbit/s) to NetPulse's
// normalized bit/s. It returns false when the field is absent or malformed.
func bboxBandwidthBps(v any) (float64, bool) {
	m := bboxMap(v)
	raw, ok := m["bandwidth"]
	if !ok || raw == nil {
		return 0, false
	}
	n, err := strconv.ParseFloat(fmt.Sprint(raw), 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return 0, false
	}
	return n * 1000, true
}

func bboxContractMbps(stats any, direction string) *float64 {
	entry := bboxMap(bboxFind(bboxMap(stats), direction))
	raw, ok := entry["contractualBandwidth"]
	if !ok {
		return nil
	}
	kbit, err := strconv.ParseFloat(fmt.Sprint(raw), 64)
	if err != nil || math.IsNaN(kbit) || math.IsInf(kbit, 0) || kbit <= 0 {
		return nil
	}
	mbps := kbit / 1000
	return &mbps
}

func bboxWANRates(stats any, c *bboxClient, now time.Time) *NetDevBps {
	rxStats := bboxMap(bboxFind(stats, "rx"))
	txStats := bboxMap(bboxFind(stats, "tx"))
	rx, tx := bboxUint(rxStats, "bytes"), bboxUint(txStats, "bytes")
	var rxBps, txBps *float64
	if rate, ok := bboxBandwidthBps(rxStats); ok {
		rxBps = &rate
	}
	if rate, ok := bboxBandwidthBps(txStats); ok {
		txBps = &rate
	}
	if !c.lastAt.IsZero() && now.After(c.lastAt) {
		d := now.Sub(c.lastAt).Seconds()
		if rxBps == nil && rx >= c.lastWANRx {
			rate := float64(rx-c.lastWANRx) * 8 / d
			rxBps = &rate
		}
		if txBps == nil && tx >= c.lastWANTx {
			rate := float64(tx-c.lastWANTx) * 8 / d
			txBps = &rate
		}
	}
	c.lastWANRx, c.lastWANTx, c.lastAt = rx, tx, now
	if rxBps == nil && txBps == nil {
		return nil
	}
	return &NetDevBps{RxBps: rxBps, TxBps: txBps}
}

func parseBboxPoll(cfg RouterConfig, device, cpu, mem, hosts, wanIP, wanStats, lanIP, lanStats, wirelessInfo, wifi24Stats, wifi5Stats, wifi6Stats, sfpInfo, ftthStats any, now time.Time, c *bboxClient) (*routerPolled, error) {
	dev := bboxMap(device)
	if nested := bboxMap(dev["device"]); nested != nil {
		dev = nested
	}
	model := bboxDirectString(dev, "modelname", "model")
	if model == "" {
		model = "Bbox"
	}
	version := bboxDirectString(bboxMap(dev["main"]), "version")
	if version == "" {
		version = bboxDirectString(bboxMap(dev["running"]), "version")
	}
	if version == "" {
		version = bboxDirectString(dev, "version")
	}
	uptime := float64(bboxUint(dev, "uptime"))
	soc := bboxDirectString(dev, "modelclass")
	if soc == "" {
		soc = "bbox"
	}
	board := &BoardInfo{Model: model, Hostname: bboxDirectString(dev, "hostname"), System: soc}
	board.Release.Version = version
	board.Release.Description = version

	// The API exposes cumulative busy and idle CPU time, so usage is derived
	// from their deltas. A first sample is intentionally reported as zero.
	cm := bboxMap(cpu)
	total := bboxUint(cm, "total")
	idle := bboxUint(cm, "idle")
	cpuTemp, _ := bboxCPUTemperatureC(cpu)
	cpuPct := bboxCPUUsagePercent(total, idle, c)

	// Bbox memory counters are reported in KiB. NetPulse stores RAM as a
	// percentage, using its shared accounting convention that excludes cached
	// memory from used memory.
	mm := bboxMap(mem)
	memory := bboxMap(bboxFind(mm, "mem"))
	ram := 0
	memTotal, memFree, memCached := bboxUint(memory, "total"), bboxUint(memory, "free"), bboxUint(memory, "cached")
	if memTotal > 0 {
		ram = memUsagePct(float64(memTotal), float64(memFree), 0, float64(memCached), 0, 0)
	}

	var leases []DhcpLease
	var wireless map[string]WirelessClient
	for _, h := range parseBboxHosts(hosts) {
		leases = append(leases, DhcpLease{MAC: h.MAC, IP: h.IP, Hostname: h.Name})
		if wireless == nil {
			wireless = map[string]WirelessClient{}
		}
		if h.Signal != 0 || h.Band != "" {
			wireless[h.MAC] = WirelessClient{SignalDbm: h.Signal, Band: h.Band}
		}
	}
	wanIPv4, wanIPv6 := parseBboxWANIPs(wanIP)
	publicIP := ""
	switch {
	case wanIPv4 != "" && wanIPv6 != "":
		publicIP = wanIPv4 + " · " + wanIPv6
	case wanIPv4 != "":
		publicIP = wanIPv4
	case wanIPv6 != "":
		publicIP = fmt.Sprintf("IPv6 %s", wanIPv6)
	}
	wanInfo := probe.WanInfo{IP: publicIP, DNS: parseBboxWANDNSServers(wanIP), FTTH: parseBboxFTTH(sfpInfo, ftthStats)}
	wanNet := bboxWANRates(wanStats, c, now)
	if wireless == nil {
		wireless = map[string]WirelessClient{}
	}
	radios := parseBboxRadios(wirelessInfo)
	bandStats := parseBboxRadioStats(wifi24Stats, wifi5Stats, wifi6Stats)
	for i := range radios {
		if counters := bandStats[radios[i].Name]; counters != nil {
			radios[i].RxBytes, radios[i].TxBytes = counters["rxBytes"], counters["txBytes"]
			radios[i].RxPackets, radios[i].TxPackets = counters["rxPackets"], counters["txPackets"]
			radios[i].RxErrors, radios[i].TxErrors = counters["rxErrors"], counters["txErrors"]
			radios[i].RxDiscards, radios[i].TxDiscards = counters["rxDiscards"], counters["txDiscards"]
		}
	}
	lanIPInfo := bboxMap(bboxFind(bboxMap(lanIP), "ip"))
	brMac := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(bboxDirectString(lanIPInfo, "mac")), "-", ":"))
	localIP := ""
	if parsed := net.ParseIP(strings.TrimSpace(bboxDirectString(lanIPInfo, "ipaddress"))); parsed != nil {
		localIP = parsed.String()
	}
	return &routerPolled{cfg: cfg, board: board, cpu: cpuPct, ram: ram, temp: cpuTemp, uptimeSec: uptime, net: wanNet, wanContractDownMbps: bboxContractMbps(wanStats, "rx"), wanContractUpMbps: bboxContractMbps(wanStats, "tx"), leases: leases, wireless: wireless, ports: parseBboxPorts(lanIP, lanStats), radios: radios, fdb: map[string]string{}, brMac: brMac, lanIP: localIP, wanInfo: wanInfo, polledAt: now.UnixMilli()}, nil
}


func (l *Live) pollRouterBbox(ctx context.Context, cfg RouterConfig) (*routerPolled, error) {
	l.mu.Lock()
	c := l.bboxClients[cfg.ID]
	l.mu.Unlock()
	if c == nil {
		return nil, fmt.Errorf("bbox %s: client unavailable", cfg.ID)
	}
	var device, cpu, mem, hosts, wanIP, wanStats, lanIP, lanStats, wireless, wifi24Stats, wifi5Stats, wifi6Stats, sfp, ftthStats any
	if err := c.get(ctx, "/device", &device); err != nil {
		return nil, err
	}
	// Optional sections are best effort: firmware variants can omit them. Keep
	// only endpoint paths for diagnostics; never log response bodies or secrets.
	optional := []struct {
		path string
		dst  any
	}{
		{"/device/cpu", &cpu}, {"/device/mem", &mem}, {"/hosts", &hosts},
		{"/wan/ip", &wanIP}, {"/wan/ip/stats", &wanStats},
		{"/lan/ip", &lanIP}, {"/lan/stats", &lanStats}, {"/wireless", &wireless},
		{"/wireless/24/stats", &wifi24Stats}, {"/wireless/5/stats", &wifi5Stats},
		{"/wireless/6/stats", &wifi6Stats}, {"/wan/sfp", &sfp}, {"/wan/ftth/stats", &ftthStats},
	}
	var failedEndpoints []string
	for _, endpoint := range optional {
		if err := c.get(ctx, endpoint.path, endpoint.dst); err != nil {
			failedEndpoints = append(failedEndpoints, endpoint.path)
		}
	}
	failureSummary := strings.Join(failedEndpoints, ", ")
	c.mu.Lock()
	previousFailures := c.lastEndpointFailures
	c.lastEndpointFailures = failureSummary
	c.mu.Unlock()
	if failureSummary != previousFailures {
		if failureSummary == "" {
			log.Printf("bbox %s: optional API endpoints recovered", cfg.LogLabel())
		} else {
			log.Printf("bbox %s: optional API endpoints unavailable: %s", cfg.LogLabel(), failureSummary)
		}
	}
	c.mu.Lock()
	p, err := parseBboxPoll(cfg, device, cpu, mem, hosts, wanIP, wanStats, lanIP, lanStats, wireless, wifi24Stats, wifi5Stats, wifi6Stats, sfp, ftthStats, time.Now(), c)
	c.mu.Unlock()
	if err == nil && p != nil {
		l.recordPortSamples(cfg.ID, p.ports)
	}
	return p, err
}
