package adapters

import (
	"github.com/gnacho/netpulse/agent/macaddr"
	"github.com/gnacho/netpulse/agent/probe"
)

// normalizePayloadMACs pasa a forma canónica (AA:BB:CC:DD:EE:FF) todas las MAC
// del payload, en sitio, al ingerirlo. El agente nativo ya las emite así, pero
// un pusher externo puede mandarlas en minúsculas o con guiones, y el resto del
// servidor compara claves de forma exacta. Un valor que no es una MAC (p. ej.
// un chassis ID LLDP de tipo nombre) se deja tal cual; nunca se vacía nada.
func normalizePayloadMACs(p *probe.Payload) {
	if p == nil {
		return
	}
	d := &p.Data
	if s := d.System; s != nil {
		s.BridgeMAC = canonMAC(s.BridgeMAC)
		canonMACSlice(s.IfaceMACs)
	}
	if w := d.Wireless; w != nil {
		w.Clients = canonMACKeys(w.Clients)
		for i := range w.Radios {
			w.Radios[i].BSSID = canonMAC(w.Radios[i].BSSID)
		}
		for i := range w.OwnBssids {
			w.OwnBssids[i].BSSID = canonMAC(w.OwnBssids[i].BSSID)
		}
		for i := range w.Scans {
			w.Scans[i].BSSID = canonMAC(w.Scans[i].BSSID)
		}
	}
	if dh := d.DHCP; dh != nil {
		for i := range dh.Leases {
			dh.Leases[i].MAC = canonMAC(dh.Leases[i].MAC)
		}
		for i := range dh.GlClients {
			dh.GlClients[i].MAC = canonMAC(dh.GlClients[i].MAC)
		}
		for i := range dh.Reservations {
			dh.Reservations[i].MAC = canonMAC(dh.Reservations[i].MAC)
		}
	}
	if f := d.FDB; f != nil {
		f.MACs = canonMACKeys(f.MACs)
		f.WirelessSide = canonMACKeys(f.WirelessSide)
		f.VLANs = canonMACKeys(f.VLANs)
	}
	d.Arp = canonMACKeys(d.Arp)
	d.ArpDevs = canonMACKeys(d.ArpDevs)
	canonMACSlice(d.ArpStale)
	if l := d.Lldp; l != nil {
		for i := range l.Neighbors {
			l.Neighbors[i].ChassisMac = canonMAC(l.Neighbors[i].ChassisMac)
		}
	}
	if dw := d.Dawn; dw != nil {
		// Las claves de SSIDs son nombres de red, no MAC: se reescribe el
		// valor (struct) bajo la misma clave.
		for k, v := range dw.SSIDs {
			for i := range v.APs {
				v.APs[i].BSSID = canonMAC(v.APs[i].BSSID)
			}
			for mac, c := range v.Clients {
				c.BSSID = canonMAC(c.BSSID)
				v.Clients[mac] = c
			}
			v.Clients = canonMACKeys(v.Clients)
			dw.SSIDs[k] = v
		}
	}
	if us := d.Usteer; us != nil {
		for k, v := range us.SSIDs {
			for i := range v.APs {
				v.APs[i].BSSID = canonMAC(v.APs[i].BSSID)
			}
			v.Clients = canonMACKeys(v.Clients)
			us.SSIDs[k] = v
		}
	}
	if cb := d.ClientBw; cb != nil {
		cb.Hosts = canonMACKeys(cb.Hosts)
	}
	if dc := d.Discovery; dc != nil {
		canonMACSlice(dc.RandomMACs)
	}
}

// canonMAC devuelve la forma canónica de s, o s sin tocar si no es una MAC.
func canonMAC(s string) string {
	if c := macaddr.Normalize(s); c != "" {
		return c
	}
	return s
}

// canonMACSlice normaliza en sitio cada elemento de ss.
func canonMACSlice(ss []string) {
	for i := range ss {
		ss[i] = canonMAC(ss[i])
	}
}

// canonMACKeys devuelve m con las claves en forma canónica. Conserva nil y {}
// tal cual (nil = sonda fallida, {} = sin entradas: el servidor los distingue).
// Si dos claves colapsan en la misma MAC, gana la que ya venía canónica.
func canonMACKeys[V any](m map[string]V) map[string]V {
	if m == nil {
		return nil
	}
	changed := false
	for k := range m {
		if canonMAC(k) != k {
			changed = true
			break
		}
	}
	if !changed {
		return m
	}
	out := make(map[string]V, len(m))
	// origKey guarda la clave original que ocupa cada clave canónica, para
	// resolver colisiones sin depender del orden de iteración: gana la que ya
	// era canónica y, entre no canónicas, la menor lexicográficamente.
	origKey := make(map[string]string, len(m))
	for k, v := range m {
		c := canonMAC(k)
		if prev, ok := origKey[c]; ok {
			if prev == c || (k != c && prev < k) {
				continue
			}
		}
		origKey[c] = k
		out[c] = v
	}
	return out
}
