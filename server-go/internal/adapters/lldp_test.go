// lldp_test.go — fixture realista de `lldpcli -f json show neighbors`
// (contrato C2): switch gestionado GS308E con mgmt-ip y caps Bridge, un AP
// anunciándose (mgmt-ip como string suelto), un equipo sin mgmt-ip ni
// descr de puerto, y formas degeneradas que NUNCA deben romper el parseo.
package adapters

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
)

// Fixture realista (lldpd 1.0.x en OpenWrt).
const lldpcliFixture = `{
  "lldp": {
    "interface": [
      {
        "name": "lan3",
        "via": "LLDP",
        "rid": "2",
        "age": "0 day, 00:01:12",
        "chassis": {
          "GS308E": {
            "id": {"type": "mac", "value": "28:c6:8e:1d:90:44"},
            "descr": "Netgear GS308E 8-Port Gigabit Smart Managed Plus Switch",
            "mgmt-ip": ["192.168.8.13", "fe80::2ac6:8eff:fe1d:9044"],
            "capability": [
              {"type": "Bridge", "enabled": true},
              {"type": "Router", "enabled": false}
            ]
          }
        },
        "port": {
          "id": {"type": "ifname", "value": "ge5"},
          "descr": "ge5",
          "ttl": "120"
        }
      },
      {
        "name": "lan1",
        "via": "LLDP",
        "rid": "1",
        "age": "0 day, 00:00:45",
        "chassis": {
          "EAP225": {
            "id": {"type": "mac", "value": "B0:4A:39:2E:77:10"},
            "descr": "TP-Link EAP225 (Outdoor)",
            "mgmt-ip": "192.168.8.4",
            "capability": [
              {"type": "Bridge", "enabled": true},
              {"type": "Wlan", "enabled": true}
            ]
          }
        },
        "port": {
          "id": {"type": "mac", "value": "B0:4A:39:2E:77:11"},
          "descr": "eth0",
          "ttl": "120"
        }
      },
      {
        "name": "lan2",
        "via": "LLDP",
        "rid": "3",
        "age": "0 day, 00:02:03",
        "chassis": {
          "mini-pc": {
            "id": {"type": "mac", "value": "3C:52:82:10:20:30"},
            "descr": "Debian GNU/Linux",
            "capability": [
              {"type": "Station", "enabled": true}
            ]
          }
        },
        "port": {
          "id": {"type": "ifname", "value": "eno1"},
          "ttl": "120"
        }
      }
    ]
  }
}`

// El parser vive en agent/probe desde #489 (mismo comando para la sonda del
// agente y la SSH); aquí solo queda la conversión de tipos.
func TestLldpFromProbe(t *testing.T) {
	nbs, err := probe.ParseLldpNeighbors([]byte(lldpcliFixture))
	if err != nil {
		t.Fatal(err)
	}
	got := lldpFromProbe(nbs)
	if len(got) != len(nbs) {
		t.Fatalf("longitud: %d vs %d", len(got), len(nbs))
	}
	if got[0].Port != "lan3" || got[0].Chassis != "GS308E" || got[0].ChassisMac != "28:C6:8E:1D:90:44" {
		t.Fatalf("conversión n[0]: %+v", got[0])
	}
	// métodos del tipo adapters intactos tras la conversión
	if info := got[0].info(); info.Chassis != "GS308E" {
		t.Fatalf("info(): %+v", info)
	}
	if got[0].displayName() != "GS308E" {
		t.Fatalf("displayName(): %q", got[0].displayName())
	}
	// nil probe → slice vacío no nil (contrato: "sin datos", no panic)
	if empty := lldpFromProbe(nil); empty == nil || len(empty) != 0 {
		t.Fatalf("nil: %+v", empty)
	}
}

func TestIsLldpUnavailable(t *testing.T) {
	// lldpcli no existe: exit 127 del shell (mensaje de ssh.ExitError envuelto)
	err127 := fmt.Errorf("ssh 192.168.8.2: %w", errors.New("Process exited with status 127"))
	if !isLldpUnavailable(err127) {
		t.Fatalf("exit 127: %v", err127)
	}
	if !isLldpUnavailable(errors.New("ssh h: ash: lldpcli: not found")) {
		t.Fatal("not found")
	}
	// Otros fallos NO son indisponibilidad cacheable
	for _, e := range []error{
		errors.New("ssh h: timeout (5s)"),
		errors.New("ssh h: Process exited with status 1"),
		nil,
	} {
		if isLldpUnavailable(e) {
			t.Fatalf("no debería ser unavailable: %v", e)
		}
	}
}

// Router.Lldp live (ítem 4 de C2): si los vecinos LLDP de un router incluyen
// en su puerto de uplink a OTRO router conocido (chassis-MAC = bridge MAC de
// otro router de la config), la tarjeta lleva Lldp; si no, se omite.
// Issue #247: cuando lldpd NO está instalado (ErrLldpUnavailable), el
// view-model expone `lldpAvailable:false` para que la UI muestre el hint
// "instala lldpd". Con lldpd presente (aunque no haya vecinos) el campo se
// omite (nil → ausente en el JSON).
func TestBuildRouterLldpUnavailable(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{{ID: "r1", Host: "192.168.8.2"}}, nil)
	p := &routerPolled{
		cfg:             RouterConfig{ID: "r1", Name: "Switch", Host: "192.168.8.2"},
		lldp:            nil,
		lldpUnavailable: true,
	}
	r := l.buildRouter(p, nil)
	if r.LldpAvailable == nil || *r.LldpAvailable {
		t.Fatalf("lldpAvailable debería ser false explícito: %+v", r.LldpAvailable)
	}
	p2 := &routerPolled{
		cfg:  RouterConfig{ID: "r1", Name: "Switch", Host: "192.168.8.2"},
		lldp: []LldpNeighbor{},
	}
	if r2 := l.buildRouter(p2, nil); r2.LldpAvailable != nil {
		t.Fatalf("con lldpd presente el campo debería omitirse: %+v", r2.LldpAvailable)
	}
}

func TestBuildRouterLldpUplink(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "flint2", Host: "192.168.8.1", IsGateway: true},
		{ID: "living", Host: "192.168.8.2"},
	}, nil)
	gw := &routerPolled{cfg: RouterConfig{ID: "flint2", Host: "192.168.8.1", IsGateway: true},
		brMac: "94:83:C4:00:00:01", fdb: map[string]string{}, lldp: []LldpNeighbor{}}
	ap := &routerPolled{cfg: RouterConfig{ID: "living", Host: "192.168.8.2"},
		brMac: "94:83:C4:00:00:02",
		fdb:   map[string]string{"94:83:C4:00:00:01": "lan1"},
		lldp: []LldpNeighbor{{
			Port: "lan1", Chassis: "Flint 2", ChassisMac: "94:83:C4:00:00:01",
			Mgmt: "192.168.8.1", Caps: []string{"Bridge", "Router"}, PortDesc: "lan3",
		}}}
	l.lastPolled = map[string]*routerPolled{"flint2": gw, "living": ap}

	// AP: uplink al gateway identificado por LLDP (chassis-MAC = brMac del
	// gateway, anunciada en el puerto donde el FDB la aprende)
	r := l.buildRouter(ap, nil)
	if r.Lldp == nil {
		t.Fatal("living debería llevar Lldp (uplink identificado)")
	}
	if r.Lldp.Chassis != "Flint 2" || r.Lldp.Mgmt != "192.168.8.1" || r.Lldp.Caps != "Bridge, Router" || r.Lldp.PortDesc != "lan3" {
		t.Fatalf("Router.Lldp: %+v", r.Lldp)
	}
	// Gateway: sin vecinos que sean routers → sin campo
	if rgw := l.buildRouter(gw, nil); rgw.Lldp != nil {
		t.Fatalf("gateway no debería llevar Lldp: %+v", rgw.Lldp)
	}
	// El anuncio llega por OTRO puerto (no es el uplink) → sin campo
	ap.fdb["94:83:C4:00:00:01"] = "lan2"
	if r := l.buildRouter(ap, nil); r.Lldp != nil {
		t.Fatalf("anuncio por puerto distinto al del FDB: %+v", r.Lldp)
	}
	// Vecino LLDP que NO es un router de la config (ni MAC, ni IP, ni nombre)
	// → sin campo
	ap.fdb["94:83:C4:00:00:01"] = "lan1"
	ap.lldp[0].ChassisMac = "28:C6:8E:1D:90:44"
	ap.lldp[0].Chassis = "GS308E"
	ap.lldp[0].Mgmt = "192.168.8.13"
	if r := l.buildRouter(ap, nil); r.Lldp != nil {
		t.Fatalf("vecino no-router: %+v", r.Lldp)
	}
	// Sin datos LLDP → sin campo (comportamiento intacto)
	ap.lldp = nil
	if r := l.buildRouter(ap, nil); r.Lldp != nil {
		t.Fatalf("sin LLDP: %+v", r.Lldp)
	}
}

// TestLldpDownCacheConcurrente: la caché de indisponibilidad de lldpd
// (lldpDownUntil) tolera lectura+escritura concurrentes (issue #208): el
// poller y un handler HTTP (p.ej. GetSurvey) pueden sondear el mismo cliente
// en paralelo. Bajo -race este test falla sin el mutex.
func TestLldpDownCacheConcurrente(t *testing.T) {
	c := &OpenWrtClient{}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = c.lldpDownCached()
				c.cacheLldpDown()
			}
		}()
	}
	wg.Wait()
	if !c.lldpDownCached() {
		t.Fatal("tras cacheLldpDown, lldpDownCached debería ser true")
	}
}

// TestLldpCacheDownExpira: la caché expira pasados lldpDownTTL (fija el TTL
// desde el momento de la llamada, no acumula).
func TestLldpCacheDownExpira(t *testing.T) {
	c := &OpenWrtClient{}
	c.cacheLldpDown()
	if !c.lldpDownCached() {
		t.Fatal("el cache debería estar activo justo tras cacheLldpDown")
	}
	c.mu.Lock()
	c.lldpDownUntil = time.Now().Add(-time.Second)
	c.mu.Unlock()
	if c.lldpDownCached() {
		t.Fatal("el cache debería expirar pasados lldpDownTTL")
	}
}

// Issue #252: en OpenWrt/GLuON lldpd suele anunciar como chassis-ID la MAC de
// la primera interfaz (eth0/WAN), distinta de la br-lan que aprende el FDB del
// vecino. El matching por identidad (mgmt-IP = Host del router, o nombre del
// chasis) descubre el uplink aunque la chassis-MAC no esté entre las bridge
// MACs de la config.
func TestUplinkLldpIdentityFallback(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "flint2", Host: "192.168.8.1", IsGateway: true},
		{ID: "living", Host: "192.168.8.2"},
	}, nil)
	gw := &routerPolled{cfg: RouterConfig{ID: "flint2", Name: "Flint 2", Host: "192.168.8.1", IsGateway: true},
		brMac: "94:83:C4:00:00:01", fdb: map[string]string{}, lldp: []LldpNeighbor{}}
	// El AP anuncia el gateway con chassis-ID = MAC de eth0 (NO la br-lan),
	// pero su mgmt-IP coincide con el Host del gateway → uplink identificado.
	ap := &routerPolled{cfg: RouterConfig{ID: "living", Name: "Living", Host: "192.168.8.2"},
		brMac: "94:83:C4:00:00:02",
		fdb:   map[string]string{},
		lldp: []LldpNeighbor{{
			Port: "lan1", Chassis: "Flint 2", ChassisMac: "94:83:C4:99:99:99",
			Mgmt: "192.168.8.1", Caps: []string{"Bridge", "Router"}, PortDesc: "lan3",
		}}}
	l.lastPolled = map[string]*routerPolled{"flint2": gw, "living": ap}
	r := l.buildRouter(ap, nil)
	if r.Lldp == nil {
		t.Fatal("uplink por mgmt-IP debería identificarse (fallback #252)")
	}
	if r.Lldp.Chassis != "Flint 2" || r.Lldp.Mgmt != "192.168.8.1" {
		t.Fatalf("Router.Lldp: %+v", r.Lldp)
	}
	// Fallback por nombre del chasis (sin mgmt-IP anunciada).
	ap.lldp[0].Mgmt = ""
	ap.lldp[0].Chassis = "living-2" // hostname del router, no su Name config
	ap.lldp[0].ChassisMac = "94:83:C4:99:99:98"
	if r := l.buildRouter(ap, nil); r.Lldp != nil {
		t.Fatalf("nombre sin coincidir no debe casar: %+v", r.Lldp)
	}
	ap.lldp[0].Chassis = "Flint 2"
	ap.lldp[0].ChassisMac = "94:83:C4:99:99:98"
	if r := l.buildRouter(ap, nil); r.Lldp == nil {
		t.Fatal("uplink por nombre del chasis debería identificarse (fallback #252)")
	}
}
