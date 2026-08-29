// helpers.go: utilidades HTTP compartidas por apply y upgrade.
package runtime

import (
	"bytes"
	"encoding/json"
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
	req, err := http.NewRequest("POST", opts.Server+path, bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+opts.Token)
	hc := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		opts.logger().Warn("[netpulse-agent] POST falló", "path", path, "err", err)
		return
	}
	resp.Body.Close()
}
