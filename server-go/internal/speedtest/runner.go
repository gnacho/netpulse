// runner.go — motor del test: interfaz pequeña (testeable con fakes) y la
// implementación real contra speedtest.net vía showwin/speedtest-go.
package speedtest

import (
	"context"
	"fmt"
	"time"

	"github.com/showwin/speedtest-go/speedtest"
)

// Runner ejecuta una medición completa. serverID > 0 fija el servidor de
// speedtest.net; 0 = autoselección del más cercano por latencia.
type Runner interface {
	Run(ctx context.Context, serverID int) (Result, error)
}

// SpeedtestNetRunner es la implementación real (showwin/speedtest-go, MIT).
// Sin estado: seguro para llamarlo en serie desde el scheduler.
type SpeedtestNetRunner struct{}

// userAgent identifica la app en las peticiones a los servidores Ookla.
const userAgent = "netpulse-wan-monitor"

func (SpeedtestNetRunner) Run(ctx context.Context, serverID int) (Result, error) {
	client := speedtest.New(
		speedtest.WithUserConfig(&speedtest.UserConfig{UserAgent: userAgent}))
	list, err := client.FetchServers()
	if err != nil {
		return Result{}, fmt.Errorf("fetch servers: %w", err)
	}
	var ids []int
	if serverID > 0 {
		ids = []int{serverID}
	}
	targets, err := list.FindServer(ids)
	if err != nil {
		return Result{}, fmt.Errorf("find server: %w", err)
	}
	if len(targets) == 0 {
		return Result{}, fmt.Errorf("no speedtest servers available")
	}
	srv := targets[0]

	// Ping primero (barato) y luego las mediciones pesadas. Los tres
	// métodos respetan ctx: el timeout del scheduler corta el test.
	if err := srv.PingTestContext(ctx, nil); err != nil {
		return Result{}, fmt.Errorf("ping: %w", err)
	}
	if err := srv.DownloadTestContext(ctx); err != nil {
		return Result{}, fmt.Errorf("download: %w", err)
	}
	if err := srv.UploadTestContext(ctx); err != nil {
		return Result{}, fmt.Errorf("upload: %w", err)
	}

	res := Result{
		TS:         time.Now(),
		DownMbps:   srv.DLSpeed.Mbps(),
		UpMbps:     srv.ULSpeed.Mbps(),
		ServerName: srv.Name,
		ServerID:   srv.ID,
	}
	if srv.Latency > 0 {
		v := float64(srv.Latency.Microseconds()) / 1000.0
		res.PingMs = &v
	}
	if srv.Jitter > 0 {
		v := float64(srv.Jitter.Microseconds()) / 1000.0
		res.JitterMs = &v
	}
	// La pérdida del analyzer UDP solo es fiable sin proxy; -1 significa
	// "sin datos" y se omite (LossPercent ya viene en %).
	if pct := srv.PacketLoss.LossPercent(); pct >= 0 {
		res.LossPct = &pct
	}
	return res, nil
}
