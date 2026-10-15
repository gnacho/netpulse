package adapters

// impostor_test.go — #1358: alerta wifi-ssid-impostor desde wifi_scans +
// BSSIDs/SSIDs propios del registry. BD real en dir temporal (wifi_scans
// la crea db.Open), engine en memoria (l.engine de NewLive).

import (
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

// newImpostorLive: Live con db real (wifi_scans) y registry con la flota
// mínima: un agente ("ap1") cuyo SSID propio es "temiscira".
func newImpostorLive(t *testing.T) (*Live, *AgentRegistry) {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	reg := NewAgentRegistry(90 * time.Second)
	reg.Ingest(&probe.Payload{
		Router:  "ap1",
		Version: "3.0.16",
		Ts:      time.Now().Unix(),
		Data: probe.PayloadData{
			Wireless: &probe.WirelessData{
				OwnBssids: []probe.OwnBSSID{
					{Iface: "wlan0", BSSID: "AA:BB:CC:00:00:01", SSID: "temiscira"},
				},
			},
		},
	})
	l := NewLive(nil, d, []RouterConfig{{ID: "ap1", Name: "AP Uno"}}, nil)
	l.SetAgents(reg)
	return l, reg
}

// insertScan: sighting directo en wifi_scans (lo que haría SaveScan).
func insertScan(t *testing.T, l *Live, routerID, bssid, ssid string, ts time.Time) {
	t.Helper()
	_, err := l.db.Exec(
		`INSERT INTO wifi_scans (router_id, iface, bssid, ssid, channel, freq, signal_dbm, width_mhz, ts)
		 VALUES (?, 'wlan0', ?, ?, 6, 2437, -55, 20, ?)`,
		routerID, bssid, ssid, ts.Unix())
	if err != nil {
		t.Fatalf("insert scan: %v", err)
	}
}

func impostorAlerts(l *Live) []AlertEvent {
	var out []AlertEvent
	for _, a := range l.engine.List() {
		if a.Type == "wifi-ssid-impostor" {
			out = append(out, a)
		}
	}
	return out
}

func TestImpostorSingleSightingDoesNotAlert(t *testing.T) {
	l, _ := newImpostorLive(t)
	now := time.Now()
	insertScan(t, l, "ap1", "DE:AD:BE:EF:00:01", "temiscira", now)
	l.EvalSsidImpostors()
	if got := impostorAlerts(l); len(got) != 0 {
		t.Fatalf("un sighting suelto no debe alertar; alertas: %+v", got)
	}
}

func TestImpostorConfirmedAfterDwell(t *testing.T) {
	l, _ := newImpostorLive(t)
	now := time.Now()
	insertScan(t, l, "ap1", "DE:AD:BE:EF:00:02", "temiscira", now.Add(-30*time.Minute))
	l.EvalSsidImpostors()
	if got := impostorAlerts(l); len(got) != 0 {
		t.Fatalf("un solo scan real aunque sea viejo no debe alertar")
	}
	// Segundo scan real (otro ts) separado >= dwell: confirma.
	insertScan(t, l, "ap1", "DE:AD:BE:EF:00:02", "temiscira", now)
	l.EvalSsidImpostors()
	got := impostorAlerts(l)
	if len(got) != 1 {
		t.Fatalf("dwell cumplido debe alertar una vez; alertas: %d", len(got))
	}
	a := got[0]
	if !a.Urgent || a.Severity != "critical" || a.Category != "clients" {
		t.Fatalf("severidad/categoría/urgencia: %+v", a)
	}
	if a.Vars["bssid"] != "DE:AD:BE:EF:00:02" || a.Vars["ssid"] != "temiscira" || a.Vars["router"] != "AP Uno" {
		t.Fatalf("vars: %+v", a.Vars)
	}
	// Re-evaluar con la misma evidencia NO duplica (ID estable + tracking).
	l.EvalSsidImpostors()
	if got := impostorAlerts(l); len(got) != 1 {
		t.Fatalf("re-evaluación no debe duplicar; alertas: %d", len(got))
	}
}

func TestImpostorOwnBssidIgnored(t *testing.T) {
	l, _ := newImpostorLive(t)
	now := time.Now()
	// BSSID propio (del registry) anunciando el SSID propio: normal.
	insertScan(t, l, "ap1", "AA:BB:CC:00:00:01", "temiscira", now.Add(-40*time.Minute))
	insertScan(t, l, "ap1", "aa:bb:cc:00:00:01", "temiscira", now) // minúsculas
	l.EvalSsidImpostors()
	if got := impostorAlerts(l); len(got) != 0 {
		t.Fatalf("BSSID propio no es impostor; alertas: %+v", got)
	}
}

func TestImpostorForeignSsidIgnored(t *testing.T) {
	l, _ := newImpostorLive(t)
	now := time.Now()
	// SSID que no es de la flota: vecino normal.
	insertScan(t, l, "ap1", "DE:AD:BE:EF:00:03", "wifi-del-vecino", now.Add(-40*time.Minute))
	insertScan(t, l, "ap1", "DE:AD:BE:EF:00:03", "wifi-del-vecino", now)
	l.EvalSsidImpostors()
	if got := impostorAlerts(l); len(got) != 0 {
		t.Fatalf("SSID ajeno no alerta; alertas: %+v", got)
	}
}

func TestImpostorSamePayloadFrozenDoesNotConfirm(t *testing.T) {
	l, _ := newImpostorLive(t)
	now := time.Now()
	// El mismo scan (mismo ts) duplicado por re-push del payload congelado:
	// NO son dos scans reales.
	insertScan(t, l, "ap1", "DE:AD:BE:EF:00:04", "temiscira", now.Add(-30*time.Minute))
	insertScan(t, l, "ap2", "DE:AD:BE:EF:00:04", "temiscira", now.Add(-30*time.Minute))
	l.EvalSsidImpostors()
	if got := impostorAlerts(l); len(got) != 0 {
		t.Fatalf("mismo ts en dos routers no confirma; alertas: %+v", got)
	}
}

func TestImpostorAutoResolve(t *testing.T) {
	l, _ := newImpostorLive(t)
	now := time.Now()
	insertScan(t, l, "ap1", "DE:AD:BE:EF:00:05", "temiscira", now.Add(-90*time.Minute))
	insertScan(t, l, "ap1", "DE:AD:BE:EF:00:05", "temiscira", now.Add(-60*time.Minute))
	l.EvalSsidImpostors()
	if got := impostorAlerts(l); len(got) != 1 {
		t.Fatalf("debió alertar (dwell cumplido y reciente); alertas: %d", len(got))
	}
	// Pasan >impostorRecent sin nuevos sightings: resolve. La ventana de la
	// query usa el reloj real; simulamos el paso del tiempo retirando la
	// evidencia (equivale a que los scans envejecieron fuera de la ventana).
	_, _ = l.db.Exec(`DELETE FROM wifi_scans`)
	l.EvalSsidImpostors()
	if l.engine.Has("alert-ssid-impostor-DE:AD:BE:EF:00:05") {
		t.Fatal("impostor desaparecido debe auto-resolverse")
	}
	if len(l.impostorActive) != 0 {
		t.Fatalf("tracking residual: %+v", l.impostorActive)
	}
}

func TestLocalSsids(t *testing.T) {
	reg := NewAgentRegistry(90 * time.Second)
	reg.Ingest(&probe.Payload{
		Router: "ap1", Version: "3.0.16", Ts: time.Now().Unix(),
		Data: probe.PayloadData{
			Wireless: &probe.WirelessData{
				OwnBssids: []probe.OwnBSSID{
					{Iface: "wlan0", BSSID: "AA:BB:CC:00:00:01", SSID: "temiscira"},
					{Iface: "wlan1", BSSID: "AA:BB:CC:00:00:02", SSID: "temiscira-guest"},
					{Iface: "wlan2", BSSID: "AA:BB:CC:00:00:03", SSID: ""}, // sin SSID: fuera
				},
			},
			Dawn: &probe.DawnData{SSIDs: map[string]probe.DawnSSID{
				"temiscira": {APs: []probe.DawnAP{{BSSID: "AA:BB:CC:00:00:01", Local: true}}},
				"vecina":    {APs: []probe.DawnAP{{BSSID: "11:22:33:44:55:66", Local: false}}}, // no local: fuera
			}},
		},
	})
	got := reg.LocalSsids()
	want := map[string]bool{"temiscira": true, "temiscira-guest": true}
	if len(got) != len(want) {
		t.Fatalf("LocalSsids = %v", got)
	}
	for _, s := range got {
		if !want[s] {
			t.Fatalf("SSID inesperado: %q (got %v)", s, got)
		}
	}
}

func TestImpostorAlertUsesLatestSighting(t *testing.T) {
	l, _ := newImpostorLive(t)
	now := time.Now()
	// El sighting RECIENTE insertado primero (rowid menor: la BD lo
	// devuelve antes sin ORDER BY). Sin inicializar router/señal en la
	// creación del tracking, la alerta salía con esos campos vacíos.
	insertScan(t, l, "ap1", "DE:AD:BE:EF:00:06", "temiscira", now)
	insertScan(t, l, "ap1", "DE:AD:BE:EF:00:06", "temiscira", now.Add(-30*time.Minute))
	l.EvalSsidImpostors()
	got := impostorAlerts(l)
	if len(got) != 1 {
		t.Fatalf("debió alertar; alertas: %d", len(got))
	}
	if got[0].Vars["router"] != "AP Uno" || got[0].Vars["signal"] != "-55" {
		t.Fatalf("vars deben ser del sighting más reciente: %+v", got[0].Vars)
	}
}

func TestImpostorApprovedDoesNotAlert(t *testing.T) {
	l, _ := newImpostorLive(t)
	now := time.Now()
	// #1400: BSSID aprobado a mano (AP UniFi ajeno a la flota NetPulse).
	if err := l.ApproveImpostorBssid("de:ad:be:ef:00:10"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	insertScan(t, l, "ap1", "DE:AD:BE:EF:00:10", "temiscira", now.Add(-40*time.Minute))
	insertScan(t, l, "ap1", "DE:AD:BE:EF:00:10", "temiscira", now)
	l.EvalSsidImpostors()
	if got := impostorAlerts(l); len(got) != 0 {
		t.Fatalf("BSSID aprobado no debe alertar; alertas: %+v", got)
	}
	if got := l.ImpostorApproved(); len(got) != 1 || got[0] != "DE:AD:BE:EF:00:10" {
		t.Fatalf("ImpostorApproved = %v", got)
	}
}

func TestImpostorApproveResolvesActiveAlert(t *testing.T) {
	l, _ := newImpostorLive(t)
	now := time.Now()
	insertScan(t, l, "ap1", "DE:AD:BE:EF:00:11", "temiscira", now.Add(-40*time.Minute))
	insertScan(t, l, "ap1", "DE:AD:BE:EF:00:11", "temiscira", now)
	l.EvalSsidImpostors()
	if got := impostorAlerts(l); len(got) != 1 {
		t.Fatalf("debió alertar; alertas: %d", len(got))
	}
	if err := l.ApproveImpostorBssid("DE:AD:BE:EF:00:11"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if got := impostorAlerts(l); len(got) != 0 {
		t.Fatalf("aprobar debe resolver la alerta activa; alertas: %+v", got)
	}
	if len(l.impostorActive) != 0 {
		t.Fatalf("tracking residual tras aprobar: %+v", l.impostorActive)
	}
	// Re-evaluar con la evidencia intacta: no re-alerta (aprobado).
	l.EvalSsidImpostors()
	if got := impostorAlerts(l); len(got) != 0 {
		t.Fatalf("aprobado no debe re-alertar; alertas: %+v", got)
	}
}

func TestImpostorRevokeReAlerts(t *testing.T) {
	l, _ := newImpostorLive(t)
	now := time.Now()
	insertScan(t, l, "ap1", "DE:AD:BE:EF:00:12", "temiscira", now.Add(-40*time.Minute))
	insertScan(t, l, "ap1", "DE:AD:BE:EF:00:12", "temiscira", now)
	if err := l.ApproveImpostorBssid("DE:AD:BE:EF:00:12"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	l.EvalSsidImpostors()
	if got := impostorAlerts(l); len(got) != 0 {
		t.Fatalf("aprobado no debe alertar; alertas: %+v", got)
	}
	l.RevokeImpostorBssid("DE:AD:BE:EF:00:12")
	if got := l.ImpostorApproved(); len(got) != 0 {
		t.Fatalf("revoke debe vaciar la lista; got %v", got)
	}
	l.EvalSsidImpostors()
	if got := impostorAlerts(l); len(got) != 1 {
		t.Fatalf("revocado y visible debe re-alertar; alertas: %d", len(got))
	}
}

func TestImpostorApproveInvalidBssid(t *testing.T) {
	l, _ := newImpostorLive(t)
	for _, bad := range []string{"", "no-es-mac", "DE:AD:BE:EF:00", "zz:zz:zz:zz:zz:zz"} {
		if err := l.ApproveImpostorBssid(bad); err == nil {
			t.Fatalf("bssid inválido %q debe dar error", bad)
		}
	}
	if got := l.ImpostorApproved(); len(got) != 0 {
		t.Fatalf("ningún inválido debe entrar en la lista; got %v", got)
	}
}

func TestImpostorAllowlistPersistsAcrossRestart(t *testing.T) {
	l, _ := newImpostorLive(t)
	if err := l.ApproveImpostorBssid("DE:AD:BE:EF:00:13"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	// Simula un reinicio: Live NUEVO sobre la misma BD (mismo dir).
	dir := t.TempDir() // dummy para no confundir; la BD real es la de l
	_ = dir
	d2 := l.db // misma BD abierta
	l2 := NewLive(nil, d2, []RouterConfig{{ID: "ap1", Name: "AP Uno"}}, nil)
	if got := l2.ImpostorApproved(); len(got) != 1 || got[0] != "DE:AD:BE:EF:00:13" {
		t.Fatalf("la lista debe sobrevivir al reinicio; got %v", got)
	}
}

func TestImpostorRuleDisabledNoAlerts(t *testing.T) {
	l, _ := newImpostorLive(t)
	now := time.Now()
	insertScan(t, l, "ap1", "DE:AD:BE:EF:00:20", "temiscira", now.Add(-40*time.Minute))
	insertScan(t, l, "ap1", "DE:AD:BE:EF:00:20", "temiscira", now)
	l.SetImpostorRuleEnabled(false)
	if l.ImpostorRuleEnabled() {
		t.Fatal("la regla debe quedar desactivada")
	}
	l.EvalSsidImpostors()
	if got := impostorAlerts(l); len(got) != 0 {
		t.Fatalf("regla desactivada no debe alertar; alertas: %+v", got)
	}
	// Persistencia: un Live nuevo sobre la misma BD arranca desactivado.
	l2 := NewLive(nil, l.db, []RouterConfig{{ID: "ap1", Name: "AP Uno"}}, nil)
	if l2.ImpostorRuleEnabled() {
		t.Fatal("el interruptor debe sobrevivir al reinicio")
	}
}

func TestImpostorRuleDisableResolvesActive(t *testing.T) {
	l, _ := newImpostorLive(t)
	now := time.Now()
	insertScan(t, l, "ap1", "DE:AD:BE:EF:00:21", "temiscira", now.Add(-40*time.Minute))
	insertScan(t, l, "ap1", "DE:AD:BE:EF:00:21", "temiscira", now)
	l.EvalSsidImpostors()
	if got := impostorAlerts(l); len(got) != 1 {
		t.Fatalf("debió alertar; alertas: %d", len(got))
	}
	l.SetImpostorRuleEnabled(false)
	if got := impostorAlerts(l); len(got) != 0 {
		t.Fatalf("desactivar debe resolver las activas; alertas: %+v", got)
	}
	if len(l.impostorActive) != 0 {
		t.Fatalf("tracking residual tras desactivar: %+v", l.impostorActive)
	}
	// Re-activar: la evidencia sigue ahí, vuelve a alertar.
	l.SetImpostorRuleEnabled(true)
	l.EvalSsidImpostors()
	if got := impostorAlerts(l); len(got) != 1 {
		t.Fatalf("re-activar debe re-alertar con evidencia vigente; alertas: %d", len(got))
	}
}
