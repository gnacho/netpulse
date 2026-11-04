package adapters

// #1818: una MAC que aparece y desaparece en segundos no es un cliente, es
// ruido de asociacion (moviles que rotan direccion, sondas, reasociaciones del
// mismo equipo). El reportero pedia justo esto: ignorar la direccion si se va
// dentro de los primeros 60 s y avisar solo de lo que persiste.
const (
	// transientDwellMs es el tiempo minimo que una MAC nueva tiene que seguir
	// viendose para considerarse un cliente.
	transientDwellMs = 60_000
	// transientExpiryMs olvida las MAC que llevan este tiempo sin aparecer, para
	// que el mapa no crezca sin limite con equipos de paso.
	transientExpiryMs = 600_000
)

// transientSeen guarda cuando se vio por primera vez y por ultima una MAC
// todavia provisional.
type transientSeen struct {
	first int64
	last  int64
}

// settleTransient filtra los dispositivos que aun no han demostrado que se
// quedan. Un MAC que no esta en device_seen ni en el mapa provisional entra en
// el mapa y desaparece de la lista; en cuanto sigue viendose pasados 60 s desde
// la primera vez, se deja pasar con su first_seen real. Los que ya estan en el
// registro pasan siempre.
//
// El estado vive en memoria a proposito: si el proceso reinicia, los
// provisionales vuelven a empezar la cuenta, que es lo correcto (no se pierde
// ningun cliente real, solo se retrasa un minuto su primera aparicion). Con la
// BD no disponible se devuelve la lista intacta: mejor mostrar un cliente
// efimero que ocultar la red entera.
func (l *Live) settleTransient(devices []Device, nowMs int64) []Device {
	if l.db == nil {
		return devices
	}
	known, err := l.seenByMac()
	if err != nil {
		return devices
	}
	l.transientMu.Lock()
	defer l.transientMu.Unlock()
	if l.transient == nil {
		l.transient = map[string]transientSeen{}
	}
	for mac, t := range l.transient {
		if nowMs-t.last > transientExpiryMs {
			delete(l.transient, mac)
		}
	}
	out := make([]Device, 0, len(devices))
	for _, d := range devices {
		mac := normSeenMAC(d.MAC)
		if mac == "" || !d.Online {
			out = append(out, d)
			continue
		}
		if _, ok := known[mac]; ok {
			out = append(out, d)
			continue
		}
		t, seen := l.transient[mac]
		if !seen {
			l.transient[mac] = transientSeen{first: nowMs, last: nowMs}
			continue
		}
		t.last = nowMs
		if nowMs-t.first < transientDwellMs {
			l.transient[mac] = t
			continue
		}
		delete(l.transient, mac)
		d.FirstSeenMs = t.first
		out = append(out, d)
	}
	return out
}
