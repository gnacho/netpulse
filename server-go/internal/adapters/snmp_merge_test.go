package adapters

import (
	"testing"
	"time"
)

// #1726: SNMP como segunda fuente en unidades con pusher. Rellena lo que el
// payload no trae y no pisa nada de lo que sí viene.
func TestApplySNMPSurfaceFillsOnlyWhatPayloadLacks(t *testing.T) {
	entry := snmpSurfaceEntry{
		ok: true,
		byIface: map[string]EthPort{
			"sfp-sfpplus1": {Iface: "sfp-sfpplus1", Up: true, Speed: "10 Gbps", RxErrs: 5, TxErrs: 3},
			"ether1":       {Iface: "ether1", Up: true, Speed: "1 Gbps", RxErrs: 9, TxErrs: 0},
		},
		lldp: []LldpNeighbor{{Port: "sfp-sfpplus1", Chassis: "XS708T"}},
	}
	p := &routerPolled{
		ports: []EthPort{
			// El pusher de RouterOS no da velocidad de enlace: se rellena.
			{ID: "sfp-sfpplus1", Iface: "sfp-sfpplus1", Up: true},
			// Aquí el payload sí trae velocidad y un contador de errores: no se pisa.
			{ID: "ether1", Iface: "ether1", Up: true, Speed: "1 Gbps", RxErrs: 2},
		},
		lldpUnavailable: true,
	}
	applySNMPSurface(p, entry)

	if got := p.ports[0].Speed; got != "10 Gbps" {
		t.Fatalf("velocidad de la boca sin dato: quiero %q, tengo %q", "10 Gbps", got)
	}
	if p.ports[0].RxErrs != 5 || p.ports[0].TxErrs != 3 {
		t.Fatalf("contadores de error: quiero 5/3, tengo %d/%d", p.ports[0].RxErrs, p.ports[0].TxErrs)
	}
	if got := p.ports[1].Speed; got != "1 Gbps" {
		t.Fatalf("no debe pisar la velocidad del payload: tengo %q", got)
	}
	if p.ports[1].RxErrs != 2 {
		t.Fatalf("no debe pisar errores del payload: tengo %d", p.ports[1].RxErrs)
	}
	if len(p.lldp) != 1 || p.lldp[0].Chassis != "XS708T" {
		t.Fatalf("los vecinos LLDP de SNMP deben entrar cuando el payload no trae: %+v", p.lldp)
	}
	if p.lldpUnavailable {
		t.Fatal("con vecinos LLDP la sonda no debe quedar marcada como no disponible")
	}
}

func TestApplySNMPSurfaceIgnoresDownLinkSpeed(t *testing.T) {
	entry := snmpSurfaceEntry{
		ok: true,
		byIface: map[string]EthPort{
			// ifHighSpeed=0 en un puerto down: no es una velocidad.
			"sfp-sfpplus3": {Iface: "sfp-sfpplus3", Speed: ""},
		},
	}
	p := &routerPolled{ports: []EthPort{{ID: "sfp-sfpplus3", Iface: "sfp-sfpplus3"}}}
	applySNMPSurface(p, entry)
	if got := p.ports[0].Speed; got != "" {
		t.Fatalf("una boca sin enlace no debe heredar velocidad: tengo %q", got)
	}
}

func TestApplySNMPSurfaceKeepsPayloadLldp(t *testing.T) {
	entry := snmpSurfaceEntry{
		ok:      true,
		byIface: map[string]EthPort{},
		lldp:    []LldpNeighbor{{Port: "sfp-sfpplus1", Chassis: "desde-snmp"}},
	}
	p := &routerPolled{lldp: []LldpNeighbor{{Port: "sfp-sfpplus1", Chassis: "desde-payload"}}}
	applySNMPSurface(p, entry)
	if len(p.lldp) != 1 || p.lldp[0].Chassis != "desde-payload" {
		t.Fatalf("los vecinos del payload mandan: %+v", p.lldp)
	}
}

// Una entrada fresca en caché se aplica sin volver a sondear SNMP: el host de
// prueba es inalcanzable, así que un sondeo real dejaría el puerto sin datos.
func TestMergeSNMPSurfaceUsesFreshCache(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	cfg := RouterConfig{ID: "crs305", Host: "127.0.0.1", SnmpEnabled: true, SnmpPort: 1}
	l.snmpSurface[cfg.ID] = snmpSurfaceEntry{
		at:      time.Now(),
		ok:      true,
		byIface: map[string]EthPort{"sfp-sfpplus1": {Iface: "sfp-sfpplus1", Speed: "10 Gbps"}},
	}
	p := &routerPolled{cfg: cfg, ports: []EthPort{{ID: "sfp-sfpplus1", Iface: "sfp-sfpplus1"}}}
	l.mergeSNMPSurface(cfg, p)
	if got := p.ports[0].Speed; got != "10 Gbps" {
		t.Fatalf("la caché fresca debe aplicarse sin sondear: tengo %q", got)
	}
}

// Sin snmp_enabled no se toca nada ni se siembra la caché.
func TestMergeSNMPSurfaceSkipsWhenDisabled(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	cfg := RouterConfig{ID: "crs305", Host: "127.0.0.1", SnmpPort: 1}
	p := &routerPolled{cfg: cfg, ports: []EthPort{{ID: "sfp-sfpplus1", Iface: "sfp-sfpplus1"}}}
	l.mergeSNMPSurface(cfg, p)
	if _, ok := l.snmpSurface[cfg.ID]; ok {
		t.Fatal("sin snmp_enabled no debe sondearse ni cachearse nada")
	}
	if p.ports[0].Speed != "" {
		t.Fatalf("sin SNMP la boca debe quedar como vino: %q", p.ports[0].Speed)
	}
}
