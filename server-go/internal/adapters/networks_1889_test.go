package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

// #1889: la vista de redes lee el trafico del device de cada red, no el de sus
// puertos, y una red sin contadores se queda sin cifra en vez de inventarla.
func TestNetworkInfosTraficoDelDevice(t *testing.T) {
	nets := []probe.UciNetwork{
		{Name: "IOT", Device: "br-iot", Proto: "static", Subnet: "192.168.111.1/24", Vlan: 999, Ports: []string{"eth0.999"}},
		{Name: "Guest", Device: "br_switch.777", Proto: "none", Vlan: 777},
	}
	ports := []EthPort{
		{ID: "if-br-iot", Iface: "br-iot", RxBps: 1200, TxBps: 3400},
		{ID: "lan1", Iface: "lan1", RxBps: 99999, TxBps: 99999},
	}
	out := networkInfos(nets, ports)
	if len(out) != 2 {
		t.Fatalf("redes = %d", len(out))
	}
	iot := out[0]
	if iot.Name != "IOT" || iot.Vlan != 999 || iot.Subnet != "192.168.111.1/24" {
		t.Fatalf("IOT = %+v", iot)
	}
	if iot.RxBps != 1200 || iot.TxBps != 3400 {
		t.Fatalf("trafico de IOT = %v/%v, quiero el de br-iot", iot.RxBps, iot.TxBps)
	}
	if len(iot.Ports) != 1 || iot.Ports[0] != "eth0.999" {
		t.Fatalf("miembros de IOT = %v", iot.Ports)
	}
	guest := out[1]
	if guest.RxBps != 0 || guest.TxBps != 0 {
		t.Fatalf("Guest sin contadores = %v/%v", guest.RxBps, guest.TxBps)
	}
	if guest.Vlan != 777 || guest.Proto != "none" {
		t.Fatalf("Guest = %+v", guest)
	}
}
