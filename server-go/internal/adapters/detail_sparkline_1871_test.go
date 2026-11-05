package adapters

import (
	"context"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/portseries"
)

// #1871: la tarjeta de flota ya rellenaba la línea de 24 h desde port_series,
// pero el detalle construye con buildRouter sin pasar por buildOverview, así
// que un pusher seguía viendo 25 buckets a cero al abrir la unidad. El
// reportero lo vio en los dos sitios, así que los dos tienen que rellenar.
func TestGetRouterDetailFillsSparklineFromPorts(t *testing.T) {
	d := openLiveTestDB(t)
	l := NewLive(nil, d, nil, nil)

	cfg := RouterConfig{ID: "crs305", Host: "192.168.2.15", Type: "external"}
	l.routers = []RouterConfig{cfg}
	l.lastPolled = map[string]*routerPolled{
		"crs305": {cfg: cfg, agentKind: "external"},
	}

	// Un pusher sin serie propia de throughput: metrics existe pero a cero,
	// que es lo que deja la sparkline llena de ceros en vez de vacía.
	now := time.Now()
	for i := 0; i < 25; i++ {
		insertMetric(t, d, "crs305", now.Add(-time.Duration(24-i)*time.Hour), 0)
	}
	if err := d.PortSeries.RecordSample(portseries.PortSample{
		RouterID: "crs305", PortID: "sfp-sfpplus1", TS: now, RxBps: 400e6, TxBps: 100e6,
	}); err != nil {
		t.Fatal(err)
	}

	det, err := l.GetRouterDetail(context.Background(), "crs305")
	if err != nil || det == nil {
		t.Fatalf("detalle: det=%v err=%v", det, err)
	}
	sp := det.Router.Sparkline
	if len(sp) == 0 {
		t.Fatal("el detalle no rellenó la sparkline desde port_series")
	}
	if last := sp[len(sp)-1]; last < 499e6 || last > 501e6 {
		t.Fatalf("última hora: %.0f bps, esperaba 5e8 (400M+100M)", last)
	}
}
