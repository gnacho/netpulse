package httpapi

// #1937: un PUT parcial reiniciaba gateway y agent_only porque los dos campos
// del body eran bool y "ausente" se leia como false. El resto de campos ya son
// punteros (nil = no tocar) y el store tambien los espera asi.

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
)

func newRouterUpdateServer(t *testing.T) *server {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return &server{db: d}
}

func putConfigRouter(t *testing.T, s *server, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("PUT", "/api/config/routers/"+id, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	s.handleUpdateConfigRouter(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	return rec
}

func routerByID(t *testing.T, s *server, id string) adapters.RouterConfig {
	t.Helper()
	for _, rt := range routerstore.ListRouters(s.db.DB) {
		if rt.ID == id {
			return rt
		}
	}
	t.Fatalf("router %s no encontrado", id)
	return adapters.RouterConfig{}
}

func TestUpdateRouterKeepsFlagsItWasNotSent(t *testing.T) {
	s := newRouterUpdateServer(t)
	gw, err := routerstore.AddRouter(s.db.DB, routerstore.AddInput{
		Name: "gateway", Host: "10.0.0.1", Type: "openwrt", IsGateway: true,
	})
	if err != nil {
		t.Fatalf("AddRouter: %v", err)
	}
	ap, err := routerstore.AddRouter(s.db.DB, routerstore.AddInput{
		Name: "ap", Host: "10.0.0.2", Type: "openwrt", AgentOnly: true,
	})
	if err != nil {
		t.Fatalf("AddRouter: %v", err)
	}

	// El PUT que solo cambia el modo de inventario no debe tocar nada mas.
	putConfigRouter(t, s, gw.ID, `{"inventory_mode":"ssh"}`)
	if got := routerByID(t, s, gw.ID); !got.IsGateway {
		t.Fatal("el PUT parcial demovio el gateway")
	}
	putConfigRouter(t, s, ap.ID, `{"name":"ap2"}`)
	if got := routerByID(t, s, ap.ID); !got.AgentOnly {
		t.Fatal("el PUT parcial se llevo agent_only")
	}

	// Enviado a proposito, el flag sigue mandando: mover el gateway lo quita
	// de la unidad anterior.
	putConfigRouter(t, s, ap.ID, `{"gateway":true}`)
	if got := routerByID(t, s, gw.ID); got.IsGateway {
		t.Fatal("el gateway anterior sigue marcado")
	}
	if got := routerByID(t, s, ap.ID); !got.IsGateway {
		t.Fatal("el gateway nuevo no quedo marcado")
	}
	putConfigRouter(t, s, ap.ID, `{"gateway":false}`)
	if got := routerByID(t, s, ap.ID); got.IsGateway {
		t.Fatal("gateway:false no se aplico")
	}
}
