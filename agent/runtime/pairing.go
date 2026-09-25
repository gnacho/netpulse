// pairing.go: modo bootstrap (Fase 9 R3). Con pairing_token en vez de
// token, el agente contacta al servidor una vez, obtiene el token real, lo
// escribe al env file y sale. procd (o el embedder) reinicia, y el agente
// arranca en modo normal.
package runtime

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// pairWithServer contacta POST /api/agents/pair con el pairing token,
// recibe el token real del agente y lo escribe al env file. Usa el
// server_fp para validar TLS (el admin lo proporciona junto con el
// pairing token).
func pairWithServer(opts Options) error {
	// FORK: over https with no pin, prove the server's key with the pairing
	// token first, then pair over a connection pinned to it.
	learnedFP := ""
	if strings.HasPrefix(opts.Server, "https://") && opts.ServerFP == "" {
		fp, err := ProveServerKey(opts.Server, opts.PairingToken)
		if err != nil {
			return err
		}
		opts.ServerFP, learnedFP = fp, fp
	}
	transport, err := buildTransport(opts)
	if err != nil {
		return err
	}

	body := fmt.Sprintf(`{"pairing_token":%q,"slug":%q}`, opts.PairingToken, opts.Slug)
	req, err := http.NewRequest("POST", opts.Server+"/api/agents/pair", strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	hc := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	log := opts.logger()
	log.Info("[netpulse-agent] pairing", "server", opts.Server, "slug", opts.Slug)
	res, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("pairing: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("pairing fallido (HTTP %d): %s", res.StatusCode, respBody)
	}

	var pr struct {
		Slug     string `json:"slug"`
		Token    string `json:"token"`
		ServerFP string `json:"server_fp"`
	}
	if err := json.NewDecoder(res.Body).Decode(&pr); err != nil {
		return fmt.Errorf("parsear respuesta pairing: %w", err)
	}
	if pr.Token == "" {
		return fmt.Errorf("pairing: token vacío en respuesta")
	}

	// Escribir el token real al env file (reemplaza PAIRING_TOKEN por TOKEN).
	if err := writePairedToken(opts.EnvFile, pr.Token, learnedFP); err != nil {
		return fmt.Errorf("escribir token al env file: %w", err)
	}

	log.Info("[netpulse-agent] pairing OK", "slug", pr.Slug, "env_file", opts.EnvFile)
	// Salir limpiamente: quien arranque el agente lo relanza y este lee
	// NETPULSE_TOKEN del env file en modo normal.
	return nil
}

// writePairedToken reescribe el env file: quita NETPULSE_PAIRING_TOKEN y
// añade/actualiza NETPULSE_TOKEN. Conserva el resto de líneas.
//
// FORK: fp, when not empty, is a pin proven during pairing and is written as
// NETPULSE_SERVER_FP, so the agent keeps pinning it after the restart.
func writePairedToken(path, token, fp string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		// Si no existe, crear uno nuevo con el token.
		out := "NETPULSE_TOKEN=" + token + "\n"
		if fp != "" {
			out += "NETPULSE_SERVER_FP=" + fp + "\n"
		}
		return os.WriteFile(path, []byte(out), 0o600)
	}
	var out []string
	haveToken := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			out = append(out, line)
			continue
		}
		k, _, ok := strings.Cut(trimmed, "=")
		if !ok {
			out = append(out, line)
			continue
		}
		key := strings.TrimSpace(k)
		if key == "NETPULSE_PAIRING_TOKEN" {
			continue // eliminar
		}
		if key == "NETPULSE_TOKEN" {
			out = append(out, "NETPULSE_TOKEN="+token)
			haveToken = true
			continue
		}
		if key == "NETPULSE_SERVER_FP" && fp != "" {
			continue // replaced by the proven pin below
		}
		out = append(out, line)
	}
	if !haveToken {
		out = append(out, "NETPULSE_TOKEN="+token)
	}
	if fp != "" {
		out = append(out, "NETPULSE_SERVER_FP="+fp)
	}
	return os.WriteFile(path, []byte(strings.Join(out, "\n")+"\n"), 0o600)
}
