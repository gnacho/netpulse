// longwrite_test.go — issue #2112: los handlers que esperan a una unidad
// (reinstall, uninstall, reemit, instaladores de integración, copia de
// configuración, acciones UCI) superan el WriteTimeout global de 15 s. Sin
// extender el deadline, la respuesta se pierde y el navegador la pinta como
// "no se alcanza el servidor".
package httpapi

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// globalWriteTimeout replica el WriteTimeout de cmd/netpulse/main.go. Si allí
// cambia, este número tiene que seguirlo: es la referencia contra la que se
// comprueba que ningún techo se queda corto.
const globalWriteTimeout = 15 * time.Second

func TestLongWriteDeadlineOutlivesTheWriteTimeout(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		setLongWriteDeadline(w, 2*time.Second)
		time.Sleep(300 * time.Millisecond)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))
	srv.Config.WriteTimeout = 100 * time.Millisecond
	srv.Start()
	defer srv.Close()

	res, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("el cliente no recibió respuesta: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"ok":true`)) {
		t.Fatalf("respuesta = %d %q, quiero 200 con ok:true", res.StatusCode, body)
	}
}

// Gate negativo: el mismo handler sin extender el deadline pierde la respuesta.
// Si este test deja de fallar, el positivo no demuestra nada.
func TestWithoutLongWriteDeadlineTheResponseIsLost(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))
	srv.Config.WriteTimeout = 100 * time.Millisecond
	srv.Start()
	defer srv.Close()

	res, err := http.Get(srv.URL)
	if err == nil {
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		if bytes.Contains(body, []byte(`"ok":true`)) {
			t.Fatalf("llegó la respuesta sin extender el deadline: el gate no prueba nada")
		}
	}
}

func TestWriteCeilingsExceedTheGlobalTimeout(t *testing.T) {
	ceilings := map[string]time.Duration{
		"reinstall":           reinstallWriteCeiling,
		"uninstall":           uninstallWriteCeiling,
		"rearm":               rearmWriteCeiling,
		"integration install": integrationWriteCeiling,
		"config backup":       configBackupWriteCeiling,
		"device action":       deviceActionWriteCeiling,
		"owut download":       owutDownloadWriteCeiling,
		"reemit (1 agente)":   reemitWriteCeiling(1),
	}
	for name, d := range ceilings {
		if d <= globalWriteTimeout {
			t.Errorf("%s: techo %s, el WriteTimeout global es %s", name, d, globalWriteTimeout)
		}
	}
}

func TestReemitWriteCeilingScalesWithTheFleetAndCaps(t *testing.T) {
	// Sin agentes sigue habiendo un margen mínimo.
	if got := reemitWriteCeiling(0); got <= globalWriteTimeout {
		t.Errorf("0 agentes: %s", got)
	}
	one, many := reemitWriteCeiling(1), reemitWriteCeiling(6)
	if many <= one {
		t.Fatalf("6 agentes (%s) debe dar más margen que 1 (%s)", many, one)
	}
	if got := reemitWriteCeiling(1000); got != reemitMaxCeiling {
		t.Errorf("1000 agentes: %s, quiero el tope %s", got, reemitMaxCeiling)
	}
}
