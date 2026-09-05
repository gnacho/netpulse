// Package pve — cliente read-only de la API de Proxmox VE (issue #561).
//
// Objetivo: obtener el inventario del cluster (VMs/CTs → nodo/host) como
// ground truth para sellar la infraestructura en NetPulse (hypervisor + ct
// con attachTo). La relación CT→host NO es deducible del tráfico L2 (un
// puerto del router mezcla dispositivos); vive en el cluster.
//
// Auth: API token de Proxmox (PVEAPIToken) con privilegios de solo lectura
// (Sys.Audit para cluster/resources; VM.Audit para leer la config de cada
// VM/CT y extraer sus MACs). El token se envía en el header Authorization:
//
//	Authorization: PVEAPIToken=USER@REALM!TOKENID=UUID
//
// TLS: el certificado del cluster es self-signed (pve-api-daemon genera uno
// propio); el cliente acepta cualquier cert (configurable en el futuro con
// CA fija). Solo lectura: GET únicamente.
package pve

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config: datos de conexión al cluster (persistidos en kv por el server).
type Config struct {
	// URL base, p. ej. "https://192.168.1.100:8006" (sin barra final).
	URL string
	// TokenID: "USER@REALM!TOKENID" (p. ej. "root@pam!netpulse").
	TokenID string
	// Secret: la parte UUID del token (se muestra una sola vez al crearlo).
	Secret string
}

// Enabled: true si hay config completa.
func (c Config) Enabled() bool {
	return c.URL != "" && c.TokenID != "" && c.Secret != ""
}

const httpTimeout = 5 * time.Second

// Client consulta la API PVE de un cluster.
type Client struct {
	cfg Config
	hc  *http.Client
}

// NewClient crea el cliente (sin red: solo guarda config). TLS: los clusters
// PVE usan un certificado self-signed (pve-api-daemon genera uno propio al
// instalar); el cliente acepta cualquier cert (InsecureSkipVerify) porque la
// autenticación real es el token API, no el cert. Una CA fija configurable se
// podría añadir en el futuro si hiciera falta.
func NewClient(cfg Config) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #561
	return &Client{
		cfg: cfg,
		hc:  &http.Client{Timeout: httpTimeout, Transport: transport},
	}
}

func (c *Client) authHeader() string {
	return "PVEAPIToken=" + c.cfg.TokenID + "=" + c.cfg.Secret
}

func (c *Client) get(ctx context.Context, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.cfg.URL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.authHeader())
	req.Header.Set("Accept", "application/json")
	res, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("PVE %s: %w", path, err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("PVE %s → HTTP %d: %s", path, res.StatusCode, strings.TrimSpace(string(body)))
	}
	// La API PVE envuelve todo en {"data": ...}.
	var wrapper struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&wrapper); err != nil {
		return err
	}
	return json.Unmarshal(wrapper.Data, dst)
}

// Resource: un elemento de cluster/resources (VM, CT, storage, node…).
// Verificado contra pve-docs: los campos que NetPulse necesita para el
// sellado de infra son vmid/name/type/node/status.
type Resource struct {
	ID     string `json:"id"`     // "qemu/100" | "lxc/200" | "node/citadel-01"…
	VMID   int    `json:"vmid"`   // 0 para nodos/storage
	Name   string `json:"name"`   // hostname del CT/VM, o nombre del nodo
	Type   string `json:"type"`   // "lxc" | "qemu" | "node" | "storage" | …
	Node   string `json:"node"`   // nodo que lo ejecuta ("" para storage)
	Status string `json:"status"` // "running" | "stopped" | …
}

// ClusterResources: GET /api2/json/cluster/resources → VMs/CTs de TODO el
// cluster con su nodo (host). Una sola llamada.
func (c *Client) ClusterResources(ctx context.Context) ([]Resource, error) {
	var out []Resource
	if err := c.get(ctx, "/api2/json/cluster/resources", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// VMConfig: GET /nodes/{node}/lxc|qemu/{vmid}/config. Devuelve el objeto de
// config crudo; NetPulse usa netN → MAC de cada interfaz.
func (c *Client) VMConfig(ctx context.Context, node, typ string, vmid int) (map[string]any, error) {
	path := fmt.Sprintf("/api2/json/nodes/%s/%s/%d/config", url.PathEscape(node), typ, vmid)
	var out map[string]any
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// NodeIP: GET /nodes/{node}/network → IP IPv4 del bridge vmbr0 (o el primer
// iface con address IPv4). Identifica al host físico del nodo en la LAN.
func (c *Client) NodeIP(ctx context.Context, node string) (string, error) {
	path := fmt.Sprintf("/api2/json/nodes/%s/network", url.PathEscape(node))
	var out []struct {
		Iface   string `json:"iface"`
		Type    string `json:"type"`
		Address string `json:"address"`
	}
	if err := c.get(ctx, path, &out); err != nil {
		return "", err
	}
	// Preferencia: vmbr0 (bridge principal); si no, cualquier iface con IPv4.
	var fallback string
	for _, i := range out {
		if !strings.Contains(i.Address, ".") {
			continue
		}
		if i.Iface == "vmbr0" {
			return i.Address, nil
		}
		if fallback == "" {
			fallback = i.Address
		}
	}
	return fallback, nil
}

// MACsOfConfig extrae las MACs de una config de VM/CT. Proxmox expone cada
// NIC como "net0": "…" con el formato:
//
//	LXC:  net0: bridge=vmbr0,hwaddr=BC:24:11:XX:XX:XX,ip=dhcp,name=eth0,type=veth
//	QEMU: net0: virtio=BC:24:11:XX:XX:XX,bridge=vmbr0   (MAC tras el nombre del modelo)
//
// Devuelve las MACs normalizadas a mayúsculas con ':'.
func MACsOfConfig(cfg map[string]any) []string {
	var out []string
	for k, v := range cfg {
		if !strings.HasPrefix(k, "net") {
			continue
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		for _, field := range strings.Split(s, ",") {
			if strings.HasPrefix(field, "hwaddr=") {
				if mac := normMAC(field[len("hwaddr="):]); mac != "" {
					out = append(out, mac)
				}
				continue
			}
			// QEMU: el primer campo es <modelo>=<MAC> (virtio=XX, e1000=XX…)
			if eq := strings.Index(field, "="); eq > 0 {
				val := field[eq+1:]
				if looksLikeMAC(val) {
					if mac := normMAC(val); mac != "" {
						out = append(out, mac)
					}
				}
			}
		}
	}
	return out
}

func looksLikeMAC(s string) bool {
	if len(s) != 17 {
		return false
	}
	for i, r := range s {
		if i%3 == 2 {
			if r != ':' {
				return false
			}
		} else if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func normMAC(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'F') {
			b.WriteRune(r)
		}
	}
	hex := b.String()
	if len(hex) != 12 {
		return ""
	}
	var out strings.Builder
	for i := 0; i < 12; i += 2 {
		if i > 0 {
			out.WriteByte(':')
		}
		out.WriteString(hex[i : i+2])
	}
	return out.String()
}
