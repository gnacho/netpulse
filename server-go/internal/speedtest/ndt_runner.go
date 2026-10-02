// ndt_runner.go — proveedor M-Lab NDT (#1037): implementación del Runner
// sobre github.com/m-lab/ndt7-client-go (cliente oficial ndt7, Go puro).
// El descubrimiento del servidor lo hace la Locate API de M-Lab (el más
// cercano sano); serverURL, si se indica, se interpreta como FQDN de un
// servidor concreto (mismo contrato que el resto de runners).
package speedtest

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	ndt7 "github.com/m-lab/ndt7-client-go"
	"github.com/m-lab/ndt7-client-go/spec"
)

// NDTRunner es la implementación real (m-lab/ndt7-client-go). Sin estado:
// segura para llamarla en serie desde el scheduler. El descubrimiento del
// servidor lo hace la Locate API de M-Lab (el más cercano sano); serverURL,
// si se indica, se interpreta como FQDN de un servidor concreto (mismo
// contrato que el resto de runners).
type NDTRunner struct{}

// userAgentNDT identifica la app en las peticiones a M-Lab (locate + ndt7).
const userAgentNDT = "netpulse-wan-monitor"

func (NDTRunner) Run(ctx context.Context, serverURL string) (Result, error) {
	client := ndt7.NewClient(userAgentNDT, userAgentNDT)
	if h := ndtServerHost(serverURL); h != "" {
		client.Server = h
	}

	down, rttUs, err := ndtRunPhase(ctx, client.StartDownload)
	if err != nil {
		return Result{}, fmt.Errorf("ndt7 download: %w", err)
	}
	up, _, err := ndtRunPhase(ctx, client.StartUpload)
	if err != nil {
		return Result{}, fmt.Errorf("ndt7 upload: %w", err)
	}

	res := Result{
		TS:         time.Now(),
		DownMbps:   down,
		UpMbps:     up,
		ServerName: client.FQDN,
		Origin:     ProviderNDT,
	}
	if rttUs > 0 {
		ms := float64(rttUs) / 1000.0
		res.PingMs = &ms
	}
	return res, nil
}

// ndtServerHost normaliza serverURL a host:port usable por el cliente ndt7
// (acepta "host", "host:port" o "https://host/path"; vacío = autodetectar).
func ndtServerHost(serverURL string) string {
	s := strings.TrimSpace(serverURL)
	if s == "" {
		return ""
	}
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		return u.Host
	}
	return s
}

// ndtRunPhase consume un phase (download o upload) y devuelve la tasa en
// Mbps calculada sobre la ÚLTIMA medición AppInfo (bytes acumulados / tiempo
// transcurrido, microsegundos) más el último RTT (TCP_INFO.MinRTT,
// microsegundos) visto, para no depender del reloj local.
func ndtRunPhase(
	ctx context.Context,
	start func(context.Context) (<-chan spec.Measurement, error),
) (mbps float64, minRTTus float64, err error) {
	ch, err := start(ctx)
	if err != nil {
		return 0, 0, err
	}
	var last *spec.AppInfo
	var rtt float64
	for m := range ch {
		if m.AppInfo != nil {
			cp := *m.AppInfo
			last = &cp
		}
		if m.TCPInfo != nil && m.TCPInfo.MinRTT > 0 {
			rtt = float64(m.TCPInfo.MinRTT)
		}
	}
	if last == nil || last.ElapsedTime <= 0 {
		return 0, 0, fmt.Errorf("sin mediciones del servidor")
	}
	bps := float64(last.NumBytes) * 8 / (float64(last.ElapsedTime) / 1e6)
	return bps / 1e6, rtt, nil
}
