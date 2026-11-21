package adapters

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"golang.org/x/crypto/ssh"
)

// TestBuildOverviewLastPolledMapRaceWithSetRouters: pollAll devuelve a
// buildOverview el mismo mapa que publica en l.lastPolled, y buildOverview lo
// recorre sin l.mu. SetRouters borraba entradas de ese mapa en el sitio (al
// quitar un router); ahora lo reemplaza. Con -race daba WARNING: DATA RACE y,
// sin -race, podía tumbar el proceso con "concurrent map iteration and map
// write".
func TestBuildOverviewLastPolledMapRaceWithSetRouters(t *testing.T) {
	reg := NewAgentRegistry(5 * time.Minute)
	now := time.Now().Unix()
	reg.Ingest(&probe.Payload{
		Router: "rt1",
		Ts:     now,
		Kind:   "native",
		Data: probe.PayloadData{
			System: &probe.SystemData{BridgeMAC: "AA:BB:CC:DD:EE:01"},
			FDB: &probe.FDBData{
				MACs:  map[string]string{"11:22:33:44:55:01": "lan1"},
				Ports: []probe.EthPort{{ID: "lan1", Up: true}},
			},
		},
	})
	reg.Ingest(&probe.Payload{
		Router: "rt2",
		Ts:     now,
		Kind:   "native",
		Data: probe.PayloadData{
			System: &probe.SystemData{BridgeMAC: "AA:BB:CC:DD:EE:02"},
			FDB: &probe.FDBData{
				MACs:  map[string]string{"11:22:33:44:55:02": "lan1"},
				Ports: []probe.EthPort{{ID: "lan1", Up: true}},
			},
		},
	})

	rt1 := RouterConfig{ID: "rt1", Name: "Router 1", Host: "192.168.1.1", IsGateway: true, AgentOnly: true}
	rt2 := RouterConfig{ID: "rt2", Name: "Router 2", Host: "192.168.1.2", AgentOnly: true}

	pool := &SSHPool{
		conns: map[string]*sshConn{},
		creds: map[string]sshCred{},
		dialTCP: func(string, string, *ssh.ClientConfig) (*ssh.Client, error) {
			return nil, errors.New("sin ssh en test")
		},
	}
	l := NewLive(&config.Config{}, nil, nil, pool)
	l.usteerCheckedAt = time.Now()
	l.SetAgents(reg)
	l.SetRouters([]RouterConfig{rt1, rt2})

	ready := make(chan struct{})
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		close(ready)
		for {
			select {
			case <-stop:
				return
			default:
				// Quitar rt2 hace que SetRouters saque "rt2" de l.lastPolled bajo l.mu.
				l.SetRouters([]RouterConfig{rt1})
				l.SetRouters([]RouterConfig{rt1, rt2})
			}
		}
	}()

	<-ready
	ctx := context.Background()
	for i := 0; i < 60; i++ {
		if _, err := l.GetOverview(ctx); err != nil {
			close(stop)
			<-done
			t.Fatalf("GetOverview devolvió error inesperado: %v", err)
		}
	}

	close(stop)
	<-done
}
