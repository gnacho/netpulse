// http_runner_test.go - contrato del runner HTTP (#976) con httptest: los
// endpoints se sobreescriben con URLs locales y los handlers sirven bytes
// conocidos para verificar el calculo de Mbps y el contrato Runner.
package speedtest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

// fixedRunner devuelve el resultado indicado (para verificar que el
// scheduler ejecuta el runner HTTP y no el Ookla).
type fixedRunner struct{ down, up float64 }

func (f fixedRunner) Run(context.Context, string) (Result, error) {
	return Result{DownMbps: f.down, UpMbps: f.up, ServerName: "http-fake"}, nil
}

// speedHTTPServer imita los endpoints de Cloudflare/LibreSpeed: bajada
// (bytes fijos), subida (200 sin cuerpo) y ping (200 minusculo).
func speedHTTPServer(t *testing.T) *httptest.Server {
	t.Helper()
	payload := make([]byte, 1<<20) // 1 MiB por peticion
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusOK)
		case strings.Contains(r.URL.RawQuery, "bytes=0") || r.URL.Path == "/backend/empty.php":
			w.WriteHeader(http.StatusOK)
		default:
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(payload)
		}
	}))
}

func TestHTTPRunnerCloudflare(t *testing.T) {
	srv := speedHTTPServer(t)
	defer srv.Close()

	r := HTTPRunner{
		Provider: ProviderCloudflare,
		DownURL:  srv.URL + "/__down",
		UpURL:    srv.URL + "/__up",
		PingURL:  srv.URL + "/__down?bytes=0",
	}
	res, err := r.Run(context.Background(), "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.DownMbps <= 0 || res.UpMbps <= 0 {
		t.Fatalf("mediciones no positivas: %+v", res)
	}
	if res.PingMs == nil || *res.PingMs <= 0 {
		t.Fatalf("ping ausente: %+v", res)
	}
	if res.ServerID != ProviderCloudflare {
		t.Fatalf("ServerID = %q, want %q", res.ServerID, ProviderCloudflare)
	}
	if res.TS.IsZero() {
		t.Fatal("TS vacio")
	}
}

func TestHTTPRunnerLibrespeedEndpoints(t *testing.T) {
	srv := speedHTTPServer(t)
	defer srv.Close()

	// Sin overrides: las URLs se derivan de la URL base de la instancia.
	r := HTTPRunner{Provider: ProviderLibrespeed}
	down, up, ping, err := r.endpoints(srv.URL)
	if err != nil {
		t.Fatalf("endpoints: %v", err)
	}
	if down != srv.URL+"/backend/garbage.php" || up != srv.URL+"/backend/empty.php" || ping != srv.URL+"/backend/empty.php" {
		t.Fatalf("endpoints derivados: %q %q %q", down, up, ping)
	}

	res, err := r.Run(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.DownMbps <= 0 || res.UpMbps <= 0 {
		t.Fatalf("mediciones no positivas: %+v", res)
	}
}

func TestHTTPRunnerCustom(t *testing.T) {
	srv := speedHTTPServer(t)
	defer srv.Close()

	// #1001: la URL dada se usa TAL CUAL (sin derivar paths ni anadir
	// query) para bajada, subida y ping.
	r := HTTPRunner{Provider: ProviderCustom}
	down, up, ping, err := r.endpoints(srv.URL + "/endpoint")
	if err != nil {
		t.Fatalf("endpoints: %v", err)
	}
	if down != srv.URL+"/endpoint" || up != down || ping != down {
		t.Fatalf("endpoints custom: %q %q %q", down, up, ping)
	}

	res, err := r.Run(context.Background(), srv.URL+"/endpoint")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.DownMbps <= 0 || res.UpMbps <= 0 {
		t.Fatalf("mediciones no positivas: %+v", res)
	}
	if res.ServerID != ProviderCustom {
		t.Fatalf("ServerID = %q, want %q", res.ServerID, ProviderCustom)
	}
}

func TestHTTPRunnerCustomErrores(t *testing.T) {
	// Sin URL de endpoint.
	if _, err := (HTTPRunner{Provider: ProviderCustom}).Run(context.Background(), ""); err == nil {
		t.Fatal("custom sin URL aceptado")
	}
	// URL que no es http(s).
	if _, err := (HTTPRunner{Provider: ProviderCustom}).Run(context.Background(), "ftp://x"); err == nil {
		t.Fatal("custom con esquema no-http aceptado")
	}
	// 2xx distinto de 200 vale en custom (endpoint libre).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(make([]byte, 1<<20))
	}))
	defer srv.Close()
	if _, err := (HTTPRunner{Provider: ProviderCustom}).Run(context.Background(), srv.URL); err != nil {
		t.Fatalf("custom con 2xx no-200 rechazado: %v", err)
	}
}

func TestHTTPRunnerErrores(t *testing.T) {
	// Provider desconocido.
	if _, err := (HTTPRunner{Provider: "nope"}).Run(context.Background(), ""); err == nil {
		t.Fatal("provider desconocido aceptado")
	}
	// LibreSpeed sin URL base.
	if _, err := (HTTPRunner{Provider: ProviderLibrespeed}).Run(context.Background(), ""); err == nil {
		t.Fatal("librespeed sin URL base aceptado")
	}
	// LibreSpeed con URL que no es http(s).
	if _, err := (HTTPRunner{Provider: ProviderLibrespeed}).Run(context.Background(), "ftp://x"); err == nil {
		t.Fatal("librespeed con esquema no-http aceptado")
	}
	// Endpoint caido: error de bajada (no panic, no resultado basura).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	r := HTTPRunner{Provider: ProviderCloudflare, DownURL: srv.URL, UpURL: srv.URL, PingURL: srv.URL}
	if _, err := r.Run(context.Background(), ""); err == nil {
		t.Fatal("HTTP 502 del endpoint aceptado como medicion")
	}
}

func TestSpeedtestProviderSettings(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	st, err := NewStore(d.DB)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	sched := NewScheduler(st, d.DB, fakeRunner{})

	// Default: NDT (#1037) en instalaciones sin la clave (privacidad).
	if got := sched.LoadSettings(); got.Provider != ProviderNDT {
		t.Fatalf("provider default = %q, want ndt", got.Provider)
	}
	// Roundtrip cloudflare.
	if err := sched.SaveSettings(Settings{IntervalHours: 12, Provider: ProviderCloudflare}); err != nil {
		t.Fatalf("save cloudflare: %v", err)
	}
	if got := sched.LoadSettings(); got.Provider != ProviderCloudflare {
		t.Fatalf("provider = %q, want cloudflare", got.Provider)
	}
	// Provider invalido rechazado.
	if err := sched.SaveSettings(Settings{IntervalHours: 12, Provider: "fast.com"}); err == nil {
		t.Fatal("provider invalido aceptado")
	}
	// LibreSpeed exige la URL base.
	if err := sched.SaveSettings(Settings{IntervalHours: 12, Provider: ProviderLibrespeed}); err == nil {
		t.Fatal("librespeed sin serverUrl aceptado")
	}
	if err := sched.SaveSettings(Settings{IntervalHours: 12, Provider: ProviderLibrespeed, ServerURL: "https://speed.example.net"}); err != nil {
		t.Fatalf("save librespeed: %v", err)
	}
	if got := sched.LoadSettings(); got.Provider != ProviderLibrespeed || got.ServerURL != "https://speed.example.net" {
		t.Fatalf("roundtrip librespeed: %+v", got)
	}
	// Custom (#1001): exige la URL del endpoint y hace roundtrip.
	if err := sched.SaveSettings(Settings{IntervalHours: 12, Provider: ProviderCustom}); err == nil {
		t.Fatal("custom sin serverUrl aceptado")
	}
	if err := sched.SaveSettings(Settings{IntervalHours: 12, Provider: ProviderCustom, ServerURL: "https://speed.lan/endpoint"}); err != nil {
		t.Fatalf("save custom: %v", err)
	}
	if got := sched.LoadSettings(); got.Provider != ProviderCustom || got.ServerURL != "https://speed.lan/endpoint" {
		t.Fatalf("roundtrip custom: %+v", got)
	}
}

// TestExecuteUsaHTTPRunner: con provider != ookla el scheduler ejecuta el
// runner HTTP inyectado, no el runner Ookla.
func TestExecuteUsaHTTPRunner(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	st, err := NewStore(d.DB)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	sched := NewScheduler(st, d.DB, fakeRunner{})
	sched.SetHTTPRunner(fixedRunner{down: 555, up: 111})

	if err := sched.SaveSettings(Settings{Enabled: true, IntervalHours: 12, Provider: ProviderCloudflare}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := sched.RunNow(); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	// Espera activa corta al single-flight (RunNow ejecuta en goroutine).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		last, _ := sched.Store().Latest()
		if last != nil {
			if last.DownMbps != 555 || last.UpMbps != 111 {
				t.Fatalf("se uso el runner Ookla, no el HTTP: %+v", last)
			}
			return
		}
	time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("el test manual no persistio resultado")
}
