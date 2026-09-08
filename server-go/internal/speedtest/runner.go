// runner.go — motor del test: interfaz pequeña (testeable con fakes) y la
// implementación real contra speedtest.net vía showwin/speedtest-go.
package speedtest

import (
	"context"
	"fmt"
	"time"

	"github.com/showwin/speedtest-go/speedtest"
)

// Runner ejecuta una medición completa. serverURL vacío = autoselección del
// servidor más cercano por latencia; con URL = usar ese servidor concreto
// (p. ej. un servidor de speedtest remoto indicado por el admin).
type Runner interface {
	Run(ctx context.Context, serverURL string) (Result, error)
}

// userAgent identifica la app en las peticiones a los servidores Ookla.
const userAgent = "netpulse-wan-monitor"

// SpeedtestNetRunner es la implementación real (showwin/speedtest-go, MIT).
// Sin estado: seguro para llamarlo en serie desde el scheduler.
type SpeedtestNetRunner struct{}

func (SpeedtestNetRunner) Run(ctx context.Context, serverURL string) (Result, error) {
	client := speedtest.New(
		speedtest.WithUserConfig(&speedtest.UserConfig{UserAgent: userAgent}))

	var srv *speedtest.Server
	if serverURL != "" {
		// Servidor concreto indicado por el admin: se corre el test contra esa
		// URL sin pasar por el discovery global.
		srv = &speedtest.Server{URL: serverURL, Context: client}
	} else {
		list, err := client.FetchServers()
		if err != nil {
			return Result{}, fmt.Errorf("fetch servers: %w", err)
		}
		targets, err := list.FindServer(nil)
		if err != nil {
			return Result{}, fmt.Errorf("find server: %w", err)
		}
		if len(targets) == 0 {
			return Result{}, fmt.Errorf("no speedtest servers available")
		}
		srv = targets[0]
	}

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
