// Package iwevents — suscripción a eventos nl80211 en tiempo real (Fase 7.1).
// Spawnea `iw event -t` como proceso hijo persistente, parsea eventos de
// estaciones wifi (new/del station) y llama a un callback inmediato.
//
// Más fiable que ubus listen (los eventos hostapd son invocations internas
// de ubus, no broadcast) y alineado con Fase 7.2 (netlink nativo).
package iwevents

import (
	"bufio"
	"context"
	"log"
	"os/exec"
	"strings"
)

// Event representa una conexión/desconexión de cliente wifi.
type Event struct {
	// Connected: true = new station, false = del station.
	Connected bool
	// MAC: dirección del cliente.
	MAC string
	// Iface: interfaz wifi (p. ej. "phy1-ap0").
	Iface string
}

// Listen spawnea `iw event -t`, parsea eventos new/del station y llama a
// onEvent inmediatamente. Bloquea hasta que ctx se cancele o el proceso hijo
// termine. Si iw no está disponible, devuelve error.
func Listen(ctx context.Context, onEvent func(Event)) error {
	// -t: timestamp unix, -f: follow (no sale)
	cmd := exec.CommandContext(ctx, "iw", "event", "-t")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = log.Writer()

	if err := cmd.Start(); err != nil {
		return err
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(nil, 64<<10)

	for scanner.Scan() {
		line := scanner.Text()
		ev, ok := parseLine(line)
		if !ok {
			continue
		}
		onEvent(ev)
	}
	_ = cmd.Wait()
	return nil
}

// parseLine extrae un Event de una línea de iw event -t.
// Formatos:
//
//	<ts>: <iface>: del station <mac>
//	<ts>: <iface>: new station <mac>
func parseLine(line string) (Event, bool) {
	// Buscar "del station" o "new station"
	var connected bool
	var marker string
	if i := strings.Index(line, "del station "); i >= 0 {
		connected = false
		marker = line[i+len("del station "):]
	} else if i := strings.Index(line, "new station "); i >= 0 {
		connected = true
		marker = line[i+len("new station "):]
	} else {
		return Event{}, false
	}

	mac := strings.TrimSpace(marker)
	if len(mac) < 17 {
		return Event{}, false
	}
	mac = mac[:17] // aa:bb:cc:dd:ee:ff

	// Extraer iface: lo que va entre el timestamp y ":" antes de "del/new station"
	iface := ""
	if idx := strings.Index(line, ": "); idx > 17 {
		rest := line[17:] // saltar timestamp (10 dígitos + punto + microsegundos)
		if idx2 := strings.Index(rest, ": "); idx2 > 0 {
			iface = strings.TrimSpace(rest[:idx2])
		}
	}

	return Event{Connected: connected, MAC: mac, Iface: iface}, true
}

// Available comprueba si iw está presente.
func Available() bool {
	_, err := exec.LookPath("iw")
	return err == nil
}
