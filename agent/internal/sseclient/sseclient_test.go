package sseclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// #1859: un stream que llegó a traer datos reinicia el backoff aunque el corte
// sea sucio (reinicio del servidor = EOF inesperado). Antes solo lo reiniciaba
// un cierre limpio, así que cada despliegue doblaba la espera hasta 5 minutos.
func TestConnectMarksEstablishedWhenTheStreamCarriesData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "event: connected\ndata: {}\n\n")
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler) // corta la conexión como un reinicio
	}))
	defer srv.Close()

	c := &Client{
		url:      srv.URL + "/api/agents/rt1/stream",
		token:    "t",
		hc:       srv.Client(),
		onEvent:  func(Event) {},
		minRetry: 5 * time.Second,
		maxRetry: 5 * time.Minute,
		logf:     func(string, ...any) {},
	}

	established, err := c.connect(context.Background())
	if !established {
		t.Fatal("un stream que trajo datos debe contar como establecido")
	}
	if err == nil {
		t.Fatal("un corte sucio debe devolver error de lectura")
	}
	if delay, next := c.retryDelay(5*time.Minute, established); delay != c.minRetry || next != c.minRetry {
		t.Fatalf("tras un stream establecido el backoff debe volver a %s, salió delay=%s next=%s",
			c.minRetry, delay, next)
	}
}

// Un cierre limpio también cuenta como establecido, y no hay error.
func TestConnectMarksEstablishedOnCleanClose(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "event: connected\ndata: {}\n\n")
	}))
	defer srv.Close()

	c := &Client{
		url:      srv.URL + "/api/agents/rt1/stream",
		token:    "t",
		hc:       srv.Client(),
		onEvent:  func(Event) {},
		minRetry: 5 * time.Second,
		maxRetry: 5 * time.Minute,
		logf:     func(string, ...any) {},
	}

	established, err := c.connect(context.Background())
	if !established || err != nil {
		t.Fatalf("cierre limpio: established=%v err=%v, quería true/nil", established, err)
	}
}

// Si el servidor no responde no hubo stream: el backoff debe seguir creciendo.
func TestConnectNotEstablishedWhenTheServerIsDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	c := &Client{
		url:      url + "/api/agents/rt1/stream",
		token:    "t",
		hc:       &http.Client{Timeout: time.Second},
		onEvent:  func(Event) {},
		minRetry: 5 * time.Second,
		maxRetry: 5 * time.Minute,
		logf:     func(string, ...any) {},
	}

	established, err := c.connect(context.Background())
	if established {
		t.Fatal("sin servidor no hay stream establecido")
	}
	if err == nil {
		t.Fatal("sin servidor debe devolver error")
	}
	if delay, _ := c.retryDelay(10*time.Second, established); delay != 10*time.Second {
		t.Fatalf("un fallo debe mantener la espera actual, salió %s", delay)
	}
}

// #1563: una conexión que llegó a establecerse reinicia el backoff, sin
// importar cuántos cortes limpios hubiera antes.
func TestRetryDelayResetsAfterCleanDisconnect(t *testing.T) {
	c := &Client{minRetry: 5 * time.Second, maxRetry: 5 * time.Minute}

	for _, current := range []time.Duration{5 * time.Second, 40 * time.Second, 5 * time.Minute} {
		delay, next := c.retryDelay(current, true)
		if delay != c.minRetry || next != c.minRetry {
			t.Fatalf("corte limpio con current=%s: delay=%s next=%s, quería %s/%s",
				current, delay, next, c.minRetry, c.minRetry)
		}
	}
}

// Solo los fallos consecutivos hacen crecer la espera, y nunca por encima de
// maxRetry.
func TestRetryDelayGrowsOnFailures(t *testing.T) {
	c := &Client{minRetry: 5 * time.Second, maxRetry: 5 * time.Minute}

	cases := []struct {
		current time.Duration
		delay   time.Duration
		next    time.Duration
	}{
		{5 * time.Second, 5 * time.Second, 10 * time.Second},
		{10 * time.Second, 10 * time.Second, 20 * time.Second},
		{4 * time.Minute, 4 * time.Minute, 5 * time.Minute},
		{5 * time.Minute, 5 * time.Minute, 5 * time.Minute},
	}
	for _, tc := range cases {
		delay, next := c.retryDelay(tc.current, false)
		if delay != tc.delay || next != tc.next {
			t.Fatalf("fallo con current=%s: delay=%s next=%s, quería %s/%s",
				tc.current, delay, next, tc.delay, tc.next)
		}
	}
}
