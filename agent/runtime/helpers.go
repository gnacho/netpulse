// helpers.go: utilidades HTTP compartidas por apply y upgrade.
package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gnacho/netpulse/agent/internal/tlspin"
)

func buildTransport(opts Options) (*http.Transport, error) {
	return tlspin.BuildTransport(opts.Server, opts.ServerFP)
}

func jsonUnmarshal(data string, v any) error {
	return json.Unmarshal([]byte(data), v)
}

// postJSON envía un POST JSON autenticado (Bearer) al servidor; los
// fallos se loguean y se descartan (fire-and-forget).
func postJSON(opts Options, transport http.RoundTripper, path string, body any) {
	payload, err := json.Marshal(body)
	if err != nil {
		return
	}
	if err := postJSONBytes(opts, transport, path, payload); err != nil {
		opts.logger().Warn("[netpulse-agent] POST falló", "path", path, "err", err)
	}
}

// postJSONBytes hace el POST y devuelve el error de transporte/estado.
func postJSONBytes(opts Options, transport http.RoundTripper, path string, payload []byte) error {
	req, err := http.NewRequest("POST", opts.Server+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+opts.Token)
	hc := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

// postJSONWithRetry reintenta un POST crítico con backoff. Para el
// apply-result: un plan cuyo resultado no se reporta queda "applying" para
// siempre en el server, y aplicar ops como `service network restart` corta
// la red del propio agente justo cuando toca reportar (#500). Corre en
// goroutine: no bloquea el stream SSE.
func postJSONWithRetry(opts Options, transport http.RoundTripper, path string, body any, what string) {
	payload, err := json.Marshal(body)
	if err != nil {
		return
	}
	go func() {
		backoff := []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second,
			time.Minute, time.Minute, time.Minute, time.Minute, time.Minute}
		for i, wait := range backoff {
			if err := postJSONBytes(opts, transport, path, payload); err == nil {
				return
			}
			opts.logger().Warn("[netpulse-agent] "+what+" falló, reintentando", "intento", i+1, "espera", wait.String())
			time.Sleep(wait)
		}
		opts.logger().Error("[netpulse-agent] " + what + " no reportado tras reintentos")
	}()
}
