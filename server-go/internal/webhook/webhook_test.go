// webhook_test.go — notificador saliente (Fase 8.7b): firma HMAC, decisión de
// reintento (4xx no / 429-5xx sí) y DLQ.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

func testAlert() alerts.AlertEvent {
	return alerts.AlertEvent{
		ID: "evt-test-001", Category: "router", Urgent: true,
		Severity: "warn", Title: "Router caído", Description: "gateway no responde",
		Ts: time.Now().Unix(), RouterID: "gateway",
	}
}

func testConfig() config.Webhook {
	return config.Webhook{
		URL: "", Secret: "test-secret-32-bytes-minimum!!", Timeout: 3 * time.Second,
		Retries: 2, RetryDelay: 10 * time.Millisecond, Enabled: true,
	}
}

func openWebhookDB(t *testing.T) *db.DB {
	t.Helper()
	dataDir := t.TempDir()
	path := filepath.Join(dataDir, "netpulse.db")
	os.Remove(path)
	d, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// TestFirmaValida: el receptor debe poder verificar la firma con el secreto.
func TestFirmaValida(t *testing.T) {
	var gotSig atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotSig.Store(r.Header.Get("X-Webhook-Signature"))
		// Verificar con el secreto compartido (como haría el receptor)
		mac := hmac.New(sha256.New, []byte("test-secret-32-bytes-minimum!!"))
		mac.Write(body)
		expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if r.Header.Get("X-Webhook-Signature") != expected {
			t.Errorf("firma incorrecta: %q != %q", r.Header.Get("X-Webhook-Signature"), expected)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := testConfig()
	cfg.URL = srv.URL
	n := NewNotifier(cfg, nil)
	defer n.Close()
	n.Notify(testAlert())
	// El worker es asíncrono: dar margen a que se envíe
	time.Sleep(300 * time.Millisecond)
	if gotSig.Load() == nil {
		t.Fatal("no se recibió ninguna petición")
	}
}

// TestRetry4xxNoReintenta: 400 no se reintenta (1 solo request).
func TestRetry4xxNoReintenta(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	cfg := testConfig()
	cfg.URL = srv.URL
	n := NewNotifier(cfg, openWebhookDB(t))
	defer n.Close()
	n.Notify(testAlert())
	time.Sleep(300 * time.Millisecond)
	if c := calls.Load(); c != 1 {
		t.Fatalf("400 debería recibir 1 request, recibió %d", c)
	}
}

// TestRetry5xxReintentaYVaADLQ: 500 se reintenta (Retries+1 requests) y al
// agotar se guarda en DLQ.
func TestRetry5xxReintentaYVaADLQ(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	d := openWebhookDB(t)
	cfg := testConfig()
	cfg.URL = srv.URL
	n := NewNotifier(cfg, d)
	defer n.Close()
	n.Notify(testAlert())
	time.Sleep(500 * time.Millisecond)

	// Retries=2 → 1 envío inicial + 2 reintentos = 3 requests
	if c := calls.Load(); c != 3 {
		t.Fatalf("500 con retries=2 debería recibir 3 requests, recibió %d", c)
	}
	var cnt int
	if err := d.QueryRow("SELECT COUNT(*) FROM webhook_events").Scan(&cnt); err != nil {
		t.Fatalf("query DLQ: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("DLQ debería tener 1 evento, tiene %d", cnt)
	}
}

// TestOKNoVaADLQ: 2xx → éxito, sin DLQ.
func TestOKNoVaADLQ(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	d := openWebhookDB(t)
	cfg := testConfig()
	cfg.URL = srv.URL
	n := NewNotifier(cfg, d)
	defer n.Close()
	n.Notify(testAlert())
	time.Sleep(300 * time.Millisecond)
	var cnt int
	if err := d.QueryRow("SELECT COUNT(*) FROM webhook_events").Scan(&cnt); err != nil {
		t.Fatalf("query DLQ: %v", err)
	}
	if cnt != 0 {
		t.Fatalf("2xx no debería ir a DLQ, tiene %d", cnt)
	}
}

// TestTransportErrorReintentaConBackoff reproduce el bug #203: un error de
// transporte (conexión rehusada) antes se reintentaba EN RÁFAGA sin espera
// (retryable solo se ponía para respuestas HTTP, no para fallos de red).
// Tras el fix, entre reintentos debe haber backoff (>= RetryDelay), de modo
// que el envío completo de un transporte caído dura >= Retries*RetryDelay.
func TestTransportErrorReintentaConBackoff(t *testing.T) {
	// Puerto cerrado: el client.Do devuelve error de transporte.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := srv.URL
	srv.Close() // el puerto ya no acepta conexiones

	d := openWebhookDB(t)
	cfg := testConfig()
	cfg.URL = deadURL
	cfg.Retries = 2
	cfg.RetryDelay = 60 * time.Millisecond
	n := NewNotifier(cfg, d)
	defer n.Close()

	start := time.Now()
	n.Notify(testAlert())
	// El worker es asíncrono: esperamos hasta que la DLQ tenga la fila.
	var cnt int
	for i := 0; i < 40; i++ {
		time.Sleep(25 * time.Millisecond)
		_ = d.QueryRow("SELECT COUNT(*) FROM webhook_events").Scan(&cnt)
		if cnt == 1 {
			break
		}
	}
	elapsed := time.Since(start)

	if cnt != 1 {
		t.Fatalf("el transporte fallido debería acabar en DLQ (1 fila), tiene %d", cnt)
	}
	// Sin fix esto acababa casi instantáneo (ráfaga sin backoff). Con el fix,
	// 2 reintentos x 60ms + el intento inicial: >= 120ms de espera total.
	minExpected := time.Duration(cfg.Retries) * cfg.RetryDelay
	if elapsed < minExpected {
		t.Fatalf("los reintentos de transporte deberían tener backoff: tardó %v, esperaba >= %v", elapsed, minExpected)
	}
}

// TestCloseConNotifyConcurrentesNoPanic reproduce el bug #202: Close() cerraba
// el canal de la cola y un Notify concurrente podía elegir la rama de envío
// tras el close → panic "send on closed channel". Con -race y repeticiones
// debe quedar limpio (el worker sale por done, no por range sobre canal cerrado).
func TestCloseConNotifyConcurrentesNoPanic(t *testing.T) {
	d := openWebhookDB(t)
	n := NewNotifier(testConfig(), d)
	done := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					n.Notify(testAlert())
				}
			}
		}()
	}
	time.Sleep(5 * time.Millisecond)
	n.Close()
	close(done)
	wg.Wait()
}
