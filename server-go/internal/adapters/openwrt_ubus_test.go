package adapters

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// #1893: el transporte ubus HTTP apuntaba al destino SSH (`ip:22`), así que
// cada UbusCall mandaba un POST HTTP al puerto de dropbear antes de caer a
// SSH. En una unidad sondeada solo por SSH eso deja tres conexiones
// "Exit before auth" por ciclo en el log del router.
func TestUbusHTTPUsesWebPortNotSSH(t *testing.T) {
	cfg := RouterConfig{ID: "gw", Host: "192.168.1.1", SSHPort: 2222}
	c := NewOpenWrtClient(cfg, nil, "", "")

	if c.Host != "192.168.1.1:2222" {
		t.Fatalf("destino SSH: %q, want 192.168.1.1:2222", c.Host)
	}
	got := c.ubusURL()
	want := "http://192.168.1.1:80/ubus"
	if got != want {
		t.Fatalf("ubus URL: %q, want %q (el puerto SSH no es un endpoint HTTP)", got, want)
	}
	if strings.Contains(got, "2222") {
		t.Fatalf("la URL de ubus lleva el puerto SSH: %q", got)
	}

	// Puerto SSH por defecto: tampoco debe colarse el 22.
	c = NewOpenWrtClient(RouterConfig{Host: "192.168.1.1"}, nil, "", "")
	if got := c.ubusURL(); got != "http://192.168.1.1:80/ubus" {
		t.Fatalf("ubus URL por defecto: %q", got)
	}
	// IPv6 literal: el host va entre corchetes.
	c = NewOpenWrtClient(RouterConfig{Host: "fd00::1", SSHPort: 22}, nil, "", "")
	if got := c.ubusURL(); got != "http://[fd00::1]:80/ubus" {
		t.Fatalf("ubus URL IPv6: %q", got)
	}
}

// #1893: tras un fallo de transporte HTTP no se vuelve a probar en cada
// llamada; el cliente cae a SSH sin sumar peticiones al puerto web.
func TestUbusHTTPStopsProbingAfterTransportFailure(t *testing.T) {
	c := &OpenWrtClient{Host: "127.0.0.1:1", httpHost: "127.0.0.1:1"}

	if _, err := c.ubusHTTPRpc(strings.Repeat("0", 32), "session", "login", nil); err == nil {
		t.Fatal("un endpoint cerrado debe fallar")
	}
	if c.ubusHTTPReady() {
		t.Fatal("tras un fallo de transporte el HTTP debe quedar en ventana de indisponibilidad")
	}

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":[0,{}]}`))
	}))
	defer srv.Close()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	c.httpHost = u.Host

	if _, err := c.ubusHTTPRpc(strings.Repeat("0", 32), "session", "login", nil); err == nil {
		t.Fatal("con el HTTP marcado caído la llamada debe devolver error")
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("el cliente golpeó el puerto web %d veces estando caído, want 0", n)
	}

	// Gate negativo del test: levantando la ventana el mismo endpoint sí se
	// golpea, así que el cero anterior es la caché y no un handler mudo.
	c.mu.Lock()
	c.ubusHTTPDownUntil = time.Time{}
	c.mu.Unlock()
	if _, err := c.ubusHTTPRpc(strings.Repeat("0", 32), "session", "login", nil); err != nil {
		t.Fatalf("con la ventana levantada la llamada debe funcionar: %v", err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("peticiones al endpoint sano: %d, want 1", n)
	}
}

// El camino feliz del transporte sigue intacto: un ubus JSON-RPC válido
// devuelve el payload y no abre ninguna ventana de indisponibilidad.
func TestUbusHTTPValidEndpointStillWorks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ubus" {
			t.Errorf("path: %q, want /ubus", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":[0,{"ok":true}]}`))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)

	c := &OpenWrtClient{Host: "192.168.1.1:22", httpHost: u.Host}
	out, err := c.ubusHTTPRpc(strings.Repeat("0", 32), "system", "board", nil)
	if err != nil {
		t.Fatalf("ubus HTTP válido: %v", err)
	}
	if string(out) != `{"ok":true}` {
		t.Fatalf("payload: %s", out)
	}
	if !c.ubusHTTPReady() {
		t.Fatal("un endpoint sano no debe abrir la ventana de indisponibilidad")
	}
}

// El host sin puerto configurado se resuelve al puerto web incluso si el
// cliente se construye a mano (los tests y el modo agente no pasan por el
// constructor).
func TestUbusURLFallsBackToWebPort(t *testing.T) {
	c := &OpenWrtClient{Host: "192.168.1.1"}
	if got := c.ubusURL(); got != "http://"+net.JoinHostPort("192.168.1.1", "80")+"/ubus" {
		t.Fatalf("ubus URL sin httpHost: %q", got)
	}
}
