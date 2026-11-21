package adapters

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
)

// TestPolledFromAgentFDBMapWriteRace: un switch RTLPlayground (beacon) manda
// el puerto del FDB como número ("1") y sus bocas como "lan1". polledFromAgent
// reescribía "1" -> "lan1" sobre fd.MACs, que es el mapa del payload publicado
// en AgentRegistry, mientras persistAgentState lo serializa con json.Marshal.
// Con -race es una carrera; sin -race puede acabar en "fatal error:
// concurrent map iteration and map write". La reescritura debe ir a una copia.
func TestPolledFromAgentFDBMapWriteRace(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	cfg := RouterConfig{ID: "sw1", Name: "Switch", Host: "192.168.1.20", AgentOnly: true}

	for iter := 0; iter < 50; iter++ {
		p := &probe.Payload{
			Router:   "sw1",
			Ts:       time.Now().Unix() + int64(iter),
			Kind:     "external",
			Interval: 30,
			Data: probe.PayloadData{
				FDB: &probe.FDBData{
					MACs:  map[string]string{"AA:BB:CC:DD:EE:01": "1"},
					Ports: []probe.EthPort{{ID: "lan1", Up: true}},
				},
			},
		}

		var wg sync.WaitGroup
		start := make(chan struct{})
		var out *routerPolled
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			// Lo mismo que hace persistAgentState con el payload publicado.
			_, _ = json.Marshal(p)
		}()
		go func() {
			defer wg.Done()
			<-start
			out = l.polledFromAgent(cfg, p)
		}()
		close(start)
		wg.Wait()

		if got := p.Data.FDB.MACs["AA:BB:CC:DD:EE:01"]; got != "1" {
			t.Fatalf("el payload publicado cambió: puerto %q, se esperaba \"1\"", got)
		}
		if got := out.fdb["AA:BB:CC:DD:EE:01"]; got != "lan1" {
			t.Fatalf("puerto normalizado %q, se esperaba \"lan1\"", got)
		}
	}
}

// TestPolledFromAgentCachedPortsInjectRatesRace: un push con NetIf pero sin
// FDB.Ports reutiliza cached.ports, que es el slice de un routerPolled ya
// publicado en lastPolled, e injectPortRates escribía las tasas en él mientras
// otros lo leían. Las tasas nuevas deben ir a una copia.
func TestPolledFromAgentCachedPortsInjectRatesRace(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	cfg := RouterConfig{ID: "rt1", Name: "Router", Host: "192.168.1.1", AgentOnly: true}

	t0 := time.Now().Add(-10 * time.Second).Truncate(time.Second)
	sharedPorts := []EthPort{{ID: "lan1", Iface: "lan1", Up: true}}
	l.mu.Lock()
	l.extrasCache[cfg.ID] = &extrasSnapshot{ports: sharedPorts}
	l.lastAgentIf[cfg.ID] = &agentIfSample{
		at:     t0,
		ifaces: map[string]probe.IfCounters{"lan1": {Rx: 1000, Tx: 1000}},
	}
	l.mu.Unlock()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				// Un lector del routerPolled anterior (detalle del router).
				_ = sharedPorts[0].RxBytes
				_ = sharedPorts[0].RxBps
			}
		}
	}()

	var last *routerPolled
	for i := 1; i <= 50; i++ {
		p := &probe.Payload{
			Router: cfg.ID,
			Ts:     t0.Unix() + int64(i*5),
			Kind:   "native",
			Data: probe.PayloadData{
				NetIf: map[string]probe.IfCounters{
					"lan1": {Rx: uint64(1000 + i*500), Tx: uint64(1000 + i*250)},
				},
			},
		}
		last = l.polledFromAgent(cfg, p)
	}

	close(stop)
	wg.Wait()

	if sharedPorts[0].RxBytes != 0 || sharedPorts[0].RxBps != 0 {
		t.Fatalf("el slice cacheado cambió: RxBytes=%d RxBps=%v", sharedPorts[0].RxBytes, sharedPorts[0].RxBps)
	}
	if last.ports[0].RxBytes == 0 {
		t.Fatal("las bocas del último sondeo no llevan contadores")
	}
}

// TestPolledFromAgentClientsMapRaceWithSetRouters: polledFromAgent leía
// l.clients[cfg.ID] fuera de l.mu mientras SetRouters reemplaza l.clients con
// el lock tomado.
func TestPolledFromAgentClientsMapRaceWithSetRouters(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	cfg := RouterConfig{ID: "rt1", Name: "Router", Host: "192.168.1.1"}
	p := &probe.Payload{Router: "rt1", Ts: time.Now().Unix(), Kind: "native"}

	stop := make(chan struct{})
	ready := make(chan struct{})
	var once sync.Once
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				l.SetRouters([]RouterConfig{cfg})
				// Aviso tras la primera llamada: así la goroutine ya está
				// corriendo antes de empezar las lecturas.
				once.Do(func() { close(ready) })
			}
		}
	}()

	<-ready
	for i := 0; i < 100; i++ {
		_ = l.polledFromAgent(cfg, p)
	}

	close(stop)
	wg.Wait()
}
