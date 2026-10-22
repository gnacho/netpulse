package adapters

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBboxClientLoginCookieHeaderAndExpiredSession(t *testing.T) {
	logins, gets := 0, 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/login":
			logins++
			if got := r.FormValue("password"); got != "secret" {
				t.Errorf("password = %q", got)
			}
			http.SetCookie(w, &http.Cookie{Name: "BBOX_ID", Value: "session-" + string(rune('0'+logins))})
		case "/api/v1/device":
			gets++
			if r.Header.Get("X-BBOX-ID") == "" {
				t.Error("missing X-BBOX-ID")
			}
			if gets == 1 {
				http.Error(w, "expired", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"device":{"modelname":"Pure Fibre","uptime":42}}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	c := newBboxClientURL(ts.URL+"/api/v1", "secret", false)
	var out any
	if err := c.get(context.Background(), "/device", &out); err != nil {
		t.Fatal(err)
	}
	if err := c.get(context.Background(), "/device", &out); err != nil {
		t.Fatal(err)
	}
	if logins != 2 {
		t.Fatalf("login count = %d, want 2", logins)
	}
}

func TestPollRouterBboxPersistsPerPortRateHistory(t *testing.T) {
	d := openLiveTestDB(t)
	var port5Tx string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body string
		switch r.URL.Path {
		case "/api/v1/device":
			body = `[{"device":{"modelname":"F@st5688b","uptime":100}}]`
		case "/api/v1/lan/ip":
			body = `[{"lan":{"ip":{"ipaddress":"192.168.0.254"},"switch":{"ports":[` +
				`{"id":1,"state":"Down"},{"id":2,"state":"Down"},{"id":3,"state":"Down"},{"id":4,"state":"Down"},` +
				`{"id":5,"state":"Up","link_mode":"2500baseT-FD"}]}}}]`
		case "/api/v1/lan/stats":
			body = `[{"lan":{"stats":{"port":[{"index":5,"rx":{"bandwidth":33},"tx":{"bandwidth":` + port5Tx + `}}]}}}]`
		default:
			body = `[]`
		}
		_, _ = w.Write([]byte(body))
	}))
	defer ts.Close()

	l := NewLive(nil, d, nil, nil)
	c := newBboxClientURL(ts.URL+"/api/v1", "", false)
	c.bboxID = "test-session"
	l.bboxClients = map[string]*bboxClient{"bbox1": c}
	for _, tx := range []string{"100", "200"} {
		port5Tx = tx
		if _, err := l.pollRouterBbox(context.Background(), RouterConfig{ID: "bbox1", Type: "bbox"}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}

	points, err := d.PortSeries.GetSeries("bbox1", "5", time.Now().Add(-time.Minute), time.Now().Add(time.Minute), "raw")
	if err != nil {
		t.Fatal(err)
	}
	if len(points) < 2 {
		t.Fatalf("Bbox port history has %d points, want at least 2", len(points))
	}
	if points[len(points)-1].RxBps != 33000 || points[len(points)-1].TxBps != 200000 {
		t.Fatalf("latest port rate = RX %.0f / TX %.0f bps, want 33000 / 200000", points[len(points)-1].RxBps, points[len(points)-1].TxBps)
	}
}

func TestPollRouterBboxRecordsOptionalEndpointFailuresAndRecovery(t *testing.T) {
	optionalUnavailable := true
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/device" {
			_, _ = w.Write([]byte(`[{"device":{"modelname":"Pure Fibre"}}]`))
			return
		}
		if optionalUnavailable {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	l := NewLive(nil, openLiveTestDB(t), nil, nil)
	c := newBboxClientURL(ts.URL+"/api/v1", "do-not-log", false)
	c.bboxID = "test-session"
	l.bboxClients = map[string]*bboxClient{"bbox1": c}

	if _, err := l.pollRouterBbox(context.Background(), RouterConfig{ID: "bbox1", Name: "Living room Bbox", Type: "bbox"}); err != nil {
		t.Fatalf("poll with missing optional endpoints: %v", err)
	}
	if !strings.Contains(c.lastEndpointFailures, "/wan/ip") || !strings.Contains(c.lastEndpointFailures, "/hosts") {
		t.Fatalf("optional endpoint failures = %q", c.lastEndpointFailures)
	}
	if strings.Contains(c.lastEndpointFailures, "do-not-log") {
		t.Fatalf("endpoint diagnostics contain a secret: %q", c.lastEndpointFailures)
	}

	optionalUnavailable = false
	if _, err := l.pollRouterBbox(context.Background(), RouterConfig{ID: "bbox1", Name: "Living room Bbox", Type: "bbox"}); err != nil {
		t.Fatalf("poll after optional endpoints recover: %v", err)
	}
	if c.lastEndpointFailures != "" {
		t.Fatalf("endpoint failures after recovery = %q", c.lastEndpointFailures)
	}
}

func TestBboxCPUUsageUsesBusyAndIdleDeltas(t *testing.T) {
	const busy0 = uint64(63461424)
	const idle0 = uint64(2135901115)
	c := &bboxClient{lastCPU: busy0, lastIdle: idle0, lastAt: time.Unix(100, 0)}
	if got := bboxCPUUsagePercent(busy0+20, idle0+80, c); got != 20 {
		t.Fatalf("CPU usage = %d%%, want 20%%", got)
	}
	if got := bboxCPUUsagePercent(busy0+20, idle0+180, c); got != 0 {
		t.Fatalf("idle-only interval CPU usage = %d%%, want 0%%", got)
	}
	// A router reboot resets counters; treat this sample as a fresh baseline.
	if got := bboxCPUUsagePercent(10, 20, c); got != 0 {
		t.Fatalf("counter-reset CPU usage = %d%%, want baseline 0%%", got)
	}
}

func TestParseBboxPollMapsVerifiedFields(t *testing.T) {
	c := &bboxClient{}
	p, err := parseBboxPoll(RouterConfig{ID: "bbox1", Type: "bbox"},
		[]any{map[string]any{"device": map[string]any{
			"modelname": "F@st5688b", "modelclass": "F5688b-v2-osin", "serialnumber": "must-not-be-exposed",
			"main": map[string]any{"version": "24.6.12"}, "reco": map[string]any{"version": "99.0-recommended"},
			"running": map[string]any{"version": "88.0-running"}, "spl": map[string]any{"version": "3.6.2"}, "uptime": float64(126747),
		}}},
		[]any{map[string]any{"device": map[string]any{"cpu": map[string]any{
			"time":        map[string]any{"total": float64(200), "idle": float64(100)},
			"temperature": map[string]any{"main": float64(52837)},
		}}}},
		[]any{map[string]any{"device": map[string]any{"mem": map[string]any{"total": float64(958672), "free": float64(407576), "cached": float64(116392), "committedas": float64(433416)}}}},
		[]any{map[string]any{"hosts": []any{map[string]any{"macaddress": "aa:bb:cc:dd:ee:ff", "ipaddress": "192.168.1.10", "hostname": "phone"}}}},
		[]any{map[string]any{"wan": map[string]any{"ip": map[string]any{
			"address": "128.79.199.106", "portrange": "24576:32767", "dnsservers": "192.0.2.53, 192.0.2.54",
			"dnsserversv6": "2001:db8::53,not-an-ip", "ip6address": []any{map[string]any{"ipaddress": "2001:db8::12"}},
		}}}},
		[]any{map[string]any{"wan": map[string]any{"ip": map[string]any{"stats": map[string]any{
			"rx": map[string]any{"bytes": "1000", "bandwidth": float64(641057), "contractualBandwidth": float64(8000000)},
			"tx": map[string]any{"bytes": "2000", "bandwidth": "1567", "contractualBandwidth": "1000000"},
		}}}}},
		[]any{map[string]any{"lan": map[string]any{
			"ip": map[string]any{"ipaddress": "192.168.0.254", "mac": "9c:24:72:a8:fa:95"},
			"switch": map[string]any{"ports": []any{
				map[string]any{"id": float64(1), "state": "Down", "link_mode": "100baseTx-HD"},
				map[string]any{"id": float64(2), "state": "Down", "link_mode": "100baseTx-HD"},
				map[string]any{"id": float64(3), "state": "Down", "link_mode": "100baseTx-HD"},
				map[string]any{"id": float64(4), "state": "Down", "link_mode": "100baseTx-HD"},
				map[string]any{"id": float64(5), "state": "Up", "link_mode": "100baseTx-FD", "blocked": float64(0), "flickering": float64(0)},
			}}}}},
		[]any{map[string]any{"lan": map[string]any{"stats": map[string]any{"port": []any{
			map[string]any{"index": float64(5), "rx": map[string]any{"bandwidth": float64(1), "bytes": "29273399572041276"}, "tx": map[string]any{"bandwidth": float64(139115), "bytes": "5264709460184467850"}},
		}}}}},
		[]any{map[string]any{"wireless": map[string]any{"radio": map[string]any{
			"24":    map[string]any{"enable": float64(1), "state": float64(1), "current_channel": float64(5), "current_bandwidth": float64(20)},
			"5":     map[string]any{"enable": float64(1), "state": float64(1), "current_channel": float64(100), "current_bandwidth": float64(160)},
			"6":     map[string]any{"enable": float64(1), "state": float64(1), "current_channel": float64(5), "current_bandwidth": float64(160)},
			"guest": map[string]any{"enable": float64(0)},
		}, "ssid": map[string]any{
			"24": map[string]any{"bssid": "7A:24:72:A8:FA:9B", "security": map[string]any{"passphrase": "do-not-return-this"}},
			"5":  map[string]any{"bssid": "8A:24:72:A8:FA:9D"},
			"6":  map[string]any{"bssid": "9C:24:72:A8:FA:9C"},
		}}}},
		[]any{map[string]any{"wireless": map[string]any{"ssid": map[string]any{"id": float64(24), "stats": map[string]any{
			"rx": map[string]any{"packets": float64(30987666), "bytes": "14366926992", "packetserrors": float64(0), "packetsdiscards": float64(3)},
			"tx": map[string]any{"packets": float64(27028842), "bytes": "21835972248", "packetserrors": float64(0), "packetsdiscards": float64(4754391)},
		}}}}},
		nil,
		nil,
		[]any{map[string]any{"wan": map[string]any{"sfp": map[string]any{
			"enable": float64(1), "pon_mode": "XGSPON", "pon_moduleclass": "B+", "pon_internalstatus": "Up", "status_sfp": "Up",
			"receive_power": "-19392", "transmit_power": float64(6060), "temperature": float64(410), "voltage": float64(32),
			"serial_number": "must-not-be-exposed", "sfpserial": "also-secret",
		}}}},
		[]any{map[string]any{"wan": map[string]any{"ftth": map[string]any{"mode": "1000baseT-FD", "state": "Up"}}}},
		time.Unix(100, 0), c)
	if err != nil {
		t.Fatal(err)
	}
	if p.board.Model != "F@st5688b" || p.board.System != "F5688b-v2-osin" || p.board.Release.Version != "24.6.12" || p.board.Release.Description != "24.6.12" {
		t.Fatalf("identity = %+v", p.board)
	}
	if p.uptimeSec != 126747 || p.brMac != "9C:24:72:A8:FA:95" || p.lanIP != "192.168.0.254" {
		t.Fatalf("uptime/MAC/LAN IP = %v / %q / %q", p.uptimeSec, p.brMac, p.lanIP)
	}
	boardJSON, err := json.Marshal(p.board)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(boardJSON), "must-not-be-exposed") || strings.Contains(string(boardJSON), "serialnumber") {
		t.Fatalf("device serial leaked into normalized board: %s", boardJSON)
	}
	if p.ram != 45 {
		t.Fatalf("RAM usage = %d%%, want 45%%", p.ram)
	}
	if p.cpu != 0 {
		t.Fatalf("first-sample CPU = %d%%, want no calculated delta", p.cpu)
	}
	if p.temp != 53 {
		t.Fatalf("CPU temperature = %d°C, want 53°C rounded from 52.837°C", p.temp)
	}
	if len(p.leases) != 1 || p.leases[0].MAC != "AA:BB:CC:DD:EE:FF" || p.leases[0].IP != "192.168.1.10" {
		t.Fatalf("leases = %+v", p.leases)
	}
	if p.wanInfo.IP != "128.79.199.106 · 2001:db8::12" {
		t.Fatalf("combined WAN IPs = %q", p.wanInfo.IP)
	}
	if got, want := strings.Join(p.wanInfo.DNS, ","), "192.0.2.53,192.0.2.54,2001:db8::53"; got != want {
		t.Fatalf("WAN DNS servers = %q, want %q", got, want)
	}
	wan := (&Live{lastPolled: map[string]*routerPolled{"bbox1": p}}).defaultWan(&RouterConfig{ID: "bbox1"})
	if wan.PublicIP != "128.79.199.106 · 2001:db8::12" {
		t.Fatalf("normalized WAN IPs = %q", wan.PublicIP)
	}
	if wan.Isp != "Bouygues" {
		t.Fatalf("WAN ISP = %q, want Bouygues", wan.Isp)
	}
	if p.net == nil || p.net.RxBps == nil || *p.net.RxBps != 641057000 || p.net.TxBps == nil || *p.net.TxBps != 1567000 {
		t.Fatalf("WAN rates (bps) = %+v", p.net)
	}
	if p.wanContractDownMbps == nil || *p.wanContractDownMbps != 8000 || p.wanContractUpMbps == nil || *p.wanContractUpMbps != 1000 {
		t.Fatalf("contract speeds (Mbps) = %v / %v", p.wanContractDownMbps, p.wanContractUpMbps)
	}
	if len(p.ports) != 5 || p.ports[0].ID != "1" || p.ports[0].Up || p.ports[0].Label != "LAN 1" || p.ports[4].ID != "5" || !p.ports[4].Up || p.ports[4].Label != "LAN 5" || p.ports[4].Speed != "100 Mbps" {
		t.Fatalf("Bbox Ethernet ports = %+v", p.ports)
	}
	if p.ports[4].RxBps != 1000 || p.ports[4].TxBps != 139115000 {
		t.Fatalf("port 5 instantaneous rate = rx %v / tx %v bps", p.ports[4].RxBps, p.ports[4].TxBps)
	}
	if ftth := p.wanInfo.FTTH; ftth == nil || ftth.Mode != "XGSPON" || ftth.State != "Up" || ftth.AccessMode != "1000baseT-FD" || ftth.AccessState != "Up" || ftth.ReceivePowerDbm == nil || *ftth.ReceivePowerDbm != -19.392 || ftth.TransmitPowerDbm == nil || *ftth.TransmitPowerDbm != 6.06 || ftth.TemperatureC == nil || *ftth.TemperatureC != 41 || ftth.VoltageV == nil || *ftth.VoltageV != 3.2 {
		t.Fatalf("FTTH diagnostics = %+v", p.wanInfo.FTTH)
	}
	if len(p.radios) != 3 || p.radios[0].Name != "2.4 GHz" || p.radios[0].Channel != 5 || p.radios[0].WidthMhz != 20 || p.radios[0].BSSID != "7A:24:72:A8:FA:9B" {
		t.Fatalf("radios = %+v", p.radios)
	}
	if p.radios[0].PowerKnown == nil || *p.radios[0].PowerKnown || p.radios[0].ClientsKnown == nil || *p.radios[0].ClientsKnown {
		t.Fatalf("unknown radio metrics should be marked unavailable: %+v", p.radios[0])
	}
	if p.radios[0].RxBytes == nil || *p.radios[0].RxBytes != 14366926992 || p.radios[0].TxBytes == nil || *p.radios[0].TxBytes != 21835972248 || p.radios[0].TxDiscards == nil || *p.radios[0].TxDiscards != 4754391 || p.radios[1].RxBytes != nil {
		t.Fatalf("per-band counters = %+v", p.radios)
	}
	encoded, err := json.Marshal(p.radios)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "do-not-return-this") || strings.Contains(string(encoded), "passphrase") {
		t.Fatalf("wireless secrets leaked from radio mapping: %s", encoded)
	}
	encoded, err = json.Marshal(p.wanInfo.FTTH)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "serial") || strings.Contains(string(encoded), "must-not-be-exposed") {
		t.Fatalf("ONT serial data leaked from FTTH mapping: %s", encoded)
	}
}

func TestBboxWANRatesFallsBackToByteCounterDeltaInBitsPerSecond(t *testing.T) {
	c := &bboxClient{}
	first := map[string]any{"wan": map[string]any{"ip": map[string]any{"stats": map[string]any{
		"rx": map[string]any{"bytes": "1000", "bandwidth": "unavailable"},
		"tx": map[string]any{"bytes": "2000"},
	}}}}
	if got := bboxWANRates(first, c, time.Unix(100, 0)); got != nil {
		t.Fatalf("first counter-only sample should have no rate: %+v", got)
	}
	second := map[string]any{"wan": map[string]any{"ip": map[string]any{"stats": map[string]any{
		"rx": map[string]any{"bytes": "2000", "bandwidth": "unavailable"},
		"tx": map[string]any{"bytes": "4000"},
	}}}}
	got := bboxWANRates(second, c, time.Unix(102, 0))
	if got == nil || got.RxBps == nil || *got.RxBps != 4000 || got.TxBps == nil || *got.TxBps != 8000 {
		t.Fatalf("counter fallback rates (bps) = %+v, want rx=4000 tx=8000", got)
	}
}

func TestBboxMalformedJSONAndMissingSession(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/login") {
			http.SetCookie(w, &http.Cookie{Name: "BBOX_ID", Value: "s"})
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-json"))
	}))
	defer ts.Close()
	c := newBboxClientURL(ts.URL+"/api/v1", "secret", false)
	var out any
	if err := c.get(context.Background(), "/device", &out); err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseBboxPollOptionalSectionsMayBeMissing(t *testing.T) {
	p, err := parseBboxPoll(RouterConfig{ID: "bbox1"}, map[string]any{"device": map[string]any{"modelname": "Pure Fibre"}}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, time.Unix(100, 0), &bboxClient{})
	if err != nil {
		t.Fatal(err)
	}
	if p.board.Model != "Pure Fibre" || len(p.leases) != 0 || len(p.wireless) != 0 {
		t.Fatalf("unexpected optional-field result: %+v", p)
	}
}

func TestParseBboxPortsUsesAPIReportedIDsAndNegotiatedModes(t *testing.T) {
	got := parseBboxPorts(map[string]any{"lan": map[string]any{"switch": map[string]any{"ports": []any{
		map[string]any{"id": float64(5), "state": "Up", "link_mode": "future-mode"},
		map[string]any{"id": float64(8), "state": "Up", "link_mode": "10Gbase-T"},
		map[string]any{"id": float64(0), "state": "Up", "link_mode": "1000baseT-FD"},
	}}}}, nil)
	if len(got) != 2 || got[0].ID != "5" || got[0].Speed != "" || got[0].Label != "LAN 5" || got[1].ID != "8" || got[1].Speed != "10 Gbps" || got[1].Label != "LAN 8" {
		t.Fatalf("ports = %+v", got)
	}
}

func TestParseBboxHostsUsesActiveHostListAndWirelessMetadata(t *testing.T) {
	response := map[string]any{
		"hosts": map[string]any{"list": []any{
			map[string]any{
				"active": float64(1), "macaddress": "00:11:22:33:44:55", "ipaddress": "192.168.0.20", "hostname": "phone",
				"wireless": map[string]any{"band": float64(5), "rssi0": "-42"},
			},
			map[string]any{"active": float64(0), "macaddress": "00:11:22:33:44:66", "ipaddress": "192.168.0.21", "hostname": "offline"},
		}},
		"wirelesshosts": []any{
			map[string]any{"stations": []any{map[string]any{"macaddress": "00:11:22:33:44:55", "rssi": "-42"}}},
		},
	}
	got := parseBboxHosts(response)
	if len(got) != 1 || got[0].MAC != "00:11:22:33:44:55" || got[0].Band != "5 GHz" || got[0].Signal != -42 {
		t.Fatalf("parsed active host metadata = %+v", got)
	}
}

func TestBboxHTTPErrorDoesNotExposeCredentials(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/login") {
			http.SetCookie(w, &http.Cookie{Name: "BBOX_ID", Value: "s"})
			return
		}
		http.Error(w, "backend failed", http.StatusBadGateway)
	}))
	defer ts.Close()
	c := newBboxClientURL(ts.URL+"/api/v1", "do-not-log", false)
	var out any
	err := c.get(context.Background(), "/device", &out)
	if err == nil || strings.Contains(err.Error(), "do-not-log") {
		t.Fatalf("error = %v", err)
	}
}
