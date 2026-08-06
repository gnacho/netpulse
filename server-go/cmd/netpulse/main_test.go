// main_test.go — regresión issue #57: notifierChain no debe paniquear con un
// nil-encapsulado (interfaz con tipo pero valor nil) entre sus miembros.
package main

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

// nilNotifier implementa alerts.Notifier; un puntero nil a este tipo,
// empaquetado en la interfaz, simula el bug (interfaz no-nil, valor nil).
// Notify accede a un campo del receiver (como webhook.Notifier.Notify accede
// a n.done/n.ch): con receiver nil esto paniquea si no se filtra.
type nilNotifier struct {
	done chan struct{}
}

func (n *nilNotifier) Notify(alerts.AlertEvent) {
	_ = n.done // accede al receiver: con n nil → panic (como webhook.Notify)
}

func TestNotifierChainIgnoraNilEncapsulado(t *testing.T) {
	// Cadena con: notifier real + puntero nil a *nilNotifier empaquetado en
	// la interfaz. Sin el fix, n.Notify paniquea (receiver nil).
	var real alerts.Notifier = &nilNotifier{}
	var nilPtr *nilNotifier
	chain := notifierChain{real, nilPtr}

	ev := alerts.AlertEvent{ID: "test", Title: "x", Urgent: true}
	chain.Notify(ev) // no debe paniquear
}

func TestNotifierChainConNilPlano(t *testing.T) {
	var real alerts.Notifier = &nilNotifier{}
	chain := notifierChain{real, nil}
	chain.Notify(alerts.AlertEvent{ID: "test2"}) // no debe paniquear
}
