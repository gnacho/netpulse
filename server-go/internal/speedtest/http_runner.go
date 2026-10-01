// http_runner.go - metodos de medicion por HTTP directo (#976): alternativas
// reales al runner Ookla para lineas donde speedtest.net mide mal (peering
// pobre con los servidores cercanos, CGNAT, etc.).
//
//   - cloudflare: speed.cloudflare.com, los endpoints publicos de su test
//     web (__down?bytes=N para bajada, __up para subida).
//   - librespeed: cualquier instancia LibreSpeed (autoalojada o publica);
//     serverURL es la URL base de la instancia y se usan sus endpoints
//     backend/garbage.php (bajada) y backend/empty.php (subida y ping).
//
// Ambos miden con bucles de chunks acotados por tiempo y por bytes para no
// castigar lineas lentas ni subestimar las rapidas.
package speedtest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Proveedores soportados en settings.speedtest.provider (#976). "" equivale
// a ProviderOokla (instalaciones anteriores sin la clave kv).
const (
	ProviderOokla      = "ookla"      // speedtest.net via showwin/speedtest-go (default)
	ProviderCloudflare = "cloudflare" // speed.cloudflare.com por HTTP directo
	ProviderLibrespeed = "librespeed" // instancia LibreSpeed (serverURL = base)
)

func validProvider(v string) bool {
	return v == ProviderOokla || v == ProviderCloudflare || v == ProviderLibrespeed
}

// Limites de la medicion HTTP: al menos minDur midiendo (precision en
// lineas rapidas) y como mucho maxBytes (techo en lineas lentas; el ctx del
// scheduler -3 min- sigue siendo la ultima palabra).
const (
	httpDownMinDur   = 4 * time.Second
	httpDownMaxBytes = 128 << 20 // 128 MiB
	httpUpMinDur     = 3 * time.Second
	httpUpMaxBytes   = 64 << 20 // 64 MiB
	httpDownChunk    = 8 << 20  // 8 MiB por peticion de bajada
	httpUpChunk      = 4 << 20  // 4 MiB por peticion de subida
	httpPings        = 5
)

// HTTPRunner ejecuta el test contra endpoints HTTP directos. Cumple la
// interfaz Runner: para cloudflare el serverURL se ignora (endpoints
// fijos); para librespeed es la URL base de la instancia.
type HTTPRunner struct {
	Provider string

	// DownURL/UpURL/PingURL pisan los endpoints por defecto (tests con
	// httptest); "" = los del proveedor.
	DownURL string
	UpURL   string
	PingURL string

	// Client opcional (tests); nil = uno con timeouts razonables.
	Client *http.Client
}

func (r HTTPRunner) httpClient() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return &http.Client{Timeout: 60 * time.Second}
}

// endpoints resuelve las URLs de bajada, subida y ping del proveedor.
func (r HTTPRunner) endpoints(serverURL string) (down, up, ping string, err error) {
	switch r.Provider {
	case ProviderCloudflare:
		down, up, ping = "https://speed.cloudflare.com/__down", "https://speed.cloudflare.com/__up", "https://speed.cloudflare.com/__down?bytes=0"
	case ProviderLibrespeed:
		base := strings.TrimRight(strings.TrimSpace(serverURL), "/")
		if base == "" {
			return "", "", "", fmt.Errorf("librespeed exige la URL base de la instancia")
		}
		u, perr := url.Parse(base)
		if perr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "", "", "", fmt.Errorf("URL de instancia librespeed invalida: %q", serverURL)
		}
		down, up, ping = base+"/backend/garbage.php", base+"/backend/empty.php", base+"/backend/empty.php"
	default:
		return "", "", "", fmt.Errorf("proveedor HTTP desconocido %q", r.Provider)
	}
	if r.DownURL != "" {
		down = r.DownURL
	}
	if r.UpURL != "" {
		up = r.UpURL
	}
	if r.PingURL != "" {
		ping = r.PingURL
	}
	return down, up, ping, nil
}

// Run mide ping, bajada y subida contra los endpoints del proveedor.
func (r HTTPRunner) Run(ctx context.Context, serverURL string) (Result, error) {
	down, up, ping, err := r.endpoints(serverURL)
	if err != nil {
		return Result{}, err
	}
	c := r.httpClient()

	// Ping: minimo de N peticiones ligeras (como el runner Ookla, ping antes
	// de las mediciones pesadas).
	var best time.Duration
	for i := 0; i < httpPings; i++ {
		start := time.Now()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ping, nil)
		resp, perr := c.Do(req)
		if perr != nil {
			return Result{}, fmt.Errorf("ping: %w", perr)
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return Result{}, fmt.Errorf("ping: HTTP %d", resp.StatusCode)
		}
		if d := time.Since(start); best == 0 || d < best {
			best = d
		}
	}

	downMbps, err := r.measureDown(ctx, c, down)
	if err != nil {
		return Result{}, fmt.Errorf("download: %w", err)
	}
	upMbps, err := r.measureUp(ctx, c, up)
	if err != nil {
		return Result{}, fmt.Errorf("upload: %w", err)
	}

	res := Result{
		TS:       time.Now(),
		DownMbps: downMbps,
		UpMbps:   upMbps,
	}
	pingMs := float64(best.Microseconds()) / 1000.0
	res.PingMs = &pingMs
	if u, uerr := url.Parse(down); uerr == nil {
		res.ServerName = u.Host
		res.ServerID = r.Provider
	}
	return res, nil
}

// measureDown descarga chunks hasta cumplir la duracion minima o el techo de
// bytes. Cloudflare acota por query (?bytes=N); librespeed por ckSize
// (bloques de 1 KiB, max 1 MiB por peticion).
func (r HTTPRunner) measureDown(ctx context.Context, c *http.Client, downURL string) (float64, error) {
	var total int64
	start := time.Now()
	for total < httpDownMaxBytes {
		u := downURL
		if r.Provider == ProviderLibrespeed {
			u += "?ckSize=1024"
		} else if !strings.Contains(u, "bytes=") {
			u = u + "?bytes=" + fmt.Sprint(httpDownChunk)
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		resp, err := c.Do(req)
		if err != nil {
			return 0, err
		}
		n, err := io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if err != nil {
			return 0, err
		}
		if resp.StatusCode != http.StatusOK {
			return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		if n == 0 {
			return 0, fmt.Errorf("endpoint de bajada devolvio 0 bytes")
		}
		total += n
		if time.Since(start) >= httpDownMinDur {
			break
		}
	}
	return mbps(total, time.Since(start)), nil
}

// measureUp sube chunks de ceros hasta cumplir la duracion minima o el
// techo. Los cuerpos se reutilizan de un buffer unico (sin allocs por chunk).
func (r HTTPRunner) measureUp(ctx context.Context, c *http.Client, upURL string) (float64, error) {
	chunk := make([]byte, httpUpChunk)
	var total int64
	start := time.Now()
	for total < httpUpMaxBytes {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, upURL, bytes.NewReader(chunk))
		req.ContentLength = int64(len(chunk))
		req.Header.Set("Content-Type", "application/octet-stream")
		resp, err := c.Do(req)
		if err != nil {
			return 0, err
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		total += int64(len(chunk))
		if time.Since(start) >= httpUpMinDur {
			break
		}
	}
	return mbps(total, time.Since(start)), nil
}

func mbps(n int64, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return float64(n) * 8 / d.Seconds() / 1e6
}
