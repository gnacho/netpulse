package adapters

import (
	"sync"
	"testing"
	"time"
)

// TestFdbMemoConcurrentReaders actualiza y poda la memo sticky del FDB desde una
// goroutine mientras otra la lee como lo hacen el overview y los endpoints HTTP
// de dispositivos: vía buildDevices (paso 5) y vía overlayStickyFdb sobre el
// mapa que devolvió updateFdbMemo. Antes de copiar la memo bajo l.mu estos
// lectores competían con el escritor: `go test -race` lo detecta, y aun sin
// -race puede tumbar el servidor con "concurrent map iteration and map write".
func TestFdbMemoConcurrentReaders(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{{ID: "gateway", Name: "GW", IsGateway: true}}, nil)
	baseNow := time.Now().UnixMilli()
	expiredNow := baseNow + fdbStickyTTL.Milliseconds() + 5_000

	// El AP ve dos clientes cableados en bocas locales: ambos entran en la memo.
	polledInsert := map[string]*routerPolled{
		"gateway": {
			cfg:   RouterConfig{ID: "gateway", Name: "GW", IsGateway: true},
			brMac: "GG:GG:GG:GG:GG:GG",
			fdb:   map[string]string{"GG:GG:GG:GG:GG:GG": "lan1", "AA:AA:AA:AA:AA:AA": "lan4"},
			arp:   map[string]string{"44:44:44:44:44:44": "192.168.13.123"},
		},
		"ap": {
			cfg:   RouterConfig{ID: "ap", Name: "AP"},
			brMac: "AA:AA:AA:AA:AA:AA",
			fdb: map[string]string{
				"AA:AA:AA:AA:AA:AA": "lan1",
				"44:44:44:44:44:44": "lan3",
				"55:55:55:55:55:55": "lan2",
			},
		},
	}
	// Ambos clientes caducaron del FDB del AP: 44:… sigue online por el ARP
	// del gateway (su entrada se refresca y el paso 5 la consulta) y 55:… está
	// offline y se poda.
	polledPrune := map[string]*routerPolled{
		"gateway": {
			cfg:   RouterConfig{ID: "gateway", Name: "GW", IsGateway: true},
			brMac: "GG:GG:GG:GG:GG:GG",
			fdb:   map[string]string{"GG:GG:GG:GG:GG:GG": "lan1", "AA:AA:AA:AA:AA:AA": "lan4"},
			arp:   map[string]string{"44:44:44:44:44:44": "192.168.13.123"},
		},
		"ap": {
			cfg:   RouterConfig{ID: "ap", Name: "AP"},
			brMac: "AA:AA:AA:AA:AA:AA",
			fdb:   map[string]string{"AA:AA:AA:AA:AA:AA": "lan1"},
		},
	}

	// whileWriting ejecuta read 200 veces mientras otra goroutine sigue
	// insertando, refrescando y podando entradas de la memo.
	whileWriting := func(read func()) {
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
					l.updateFdbMemo(polledInsert, baseNow)
					l.updateFdbMemo(polledPrune, expiredNow)
				}
			}
		}()
		for i := 0; i < 200; i++ {
			read()
		}
		close(stop)
		wg.Wait()
	}

	t.Run("buildDevices", func(t *testing.T) {
		whileWriting(func() { _ = l.buildDevices(polledPrune, "") })
	})
	t.Run("overlayStickyFdb", func(t *testing.T) {
		memo := l.updateFdbMemo(polledInsert, baseNow)
		whileWriting(func() { _ = overlayStickyFdb(polledPrune, memo, baseNow) })
	})
}
