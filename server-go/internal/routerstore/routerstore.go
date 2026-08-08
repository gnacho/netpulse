// Package routerstore — almacén de routers configurados (tabla `routers` de
// SQLite) + bootstrap (paridad src/routerstore.js, SPEC §8.2):
//  1. Si la tabla tiene filas → es la fuente de verdad.
//  2. Si está vacía y hay ROUTERS_JSON → se siembra.
//  3. Si sigue vacía y NO es demo → autodetección del gateway (ip route +
//     sondeo SSH `ubus call system board`).
package routerstore

import (
	"database/sql"
	"encoding/json"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/sshkey"
	"golang.org/x/text/unicode/norm"
)

const probeTimeout = 4 * time.Second

// ListRouters devuelve la tabla routers ordenada is_gateway DESC,
// created_at ASC, con is_gateway como booleano.
func ListRouters(db *sql.DB) []adapters.RouterConfig {
	rows, err := db.Query("SELECT id, name, host, type, is_gateway, agent_only, created_at FROM routers ORDER BY is_gateway DESC, created_at ASC")
	if err != nil {
		return []adapters.RouterConfig{}
	}
	defer rows.Close()
	out := []adapters.RouterConfig{}
	for rows.Next() {
		var r adapters.RouterConfig
		var gw, ao int
		var name sql.NullString
		if err := rows.Scan(&r.ID, &name, &r.Host, &r.Type, &gw, &ao, &r.CreatedAt); err != nil {
			continue
		}
		r.Name = name.String
		r.IsGateway = gw == 1
		r.AgentOnly = ao == 1
		out = append(out, r)
	}
	return out
}

var slugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// slugify replica el slug JS: minúsculas, NFD sin diacríticos,
// [^a-z0-9]+ → '-', trim guiones, máx 32.
func slugify(text string) string {
	s := strings.ToLower(text)
	// NFD + eliminar marcas combinantes (U+0300–U+036F)
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		if r >= 0x0300 && r <= 0x036F {
			continue
		}
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	s = slugNonAlnum.ReplaceAllString(b.String(), "-")
	s = strings.Trim(s, "-")
	if len(s) > 32 {
		s = s[:32]
	}
	return s
}

// uniqueId genera un id único a partir del nombre o del host.
func uniqueId(db *sql.DB, name, host string) string {
	base := slugify(name)
	if base == "" {
		parts := strings.Split(host, ".")
		base = "router-" + parts[len(parts)-1]
	}
	if base == "" {
		base = "router"
	}
	id := base
	n := 2
	for {
		var one int
		err := db.QueryRow("SELECT 1 FROM routers WHERE id = ?", id).Scan(&one)
		if err == sql.ErrNoRows {
			return id
		}
		if err != nil {
			return id
		}
		id = base + "-" + itoa(n)
		n++
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// AddInput son los datos de alta de un router.
type AddInput struct {
	Name      string
	Host      string
	Type      string // default "openwrt"
	IsGateway bool
	AgentOnly bool
}

// AddRouter inserta un router (si IsGateway, el resto pierde el flag —
// transacción, un solo gateway) y devuelve la fila creada.
func AddRouter(db *sql.DB, in AddInput) (adapters.RouterConfig, error) {
	if in.Type == "" {
		in.Type = "openwrt"
	}
	id := uniqueId(db, in.Name, in.Host)
	now := time.Now().UnixMilli()
	name := in.Name
	if name == "" {
		name = in.Host
	}
	tx, err := db.Begin()
	if err != nil {
		return adapters.RouterConfig{}, err
	}
	defer tx.Rollback()
	if in.IsGateway {
		if _, err := tx.Exec("UPDATE routers SET is_gateway = 0"); err != nil {
			return adapters.RouterConfig{}, err
		}
	}
	gw := 0
	if in.IsGateway {
		gw = 1
	}
	ao := 0
	if in.AgentOnly {
		ao = 1
	}
	if _, err := tx.Exec(
		"INSERT INTO routers (id, name, host, type, is_gateway, agent_only, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		id, name, in.Host, in.Type, gw, ao, now,
	); err != nil {
		return adapters.RouterConfig{}, err
	}
	if err := tx.Commit(); err != nil {
		return adapters.RouterConfig{}, err
	}
	for _, r := range ListRouters(db) {
		if r.ID == id {
			return r, nil
		}
	}
	return adapters.RouterConfig{}, sql.ErrNoRows
}

// RemoveRouter borra por id; true si borró (changes > 0).
func RemoveRouter(db *sql.DB, id string) bool {
	res, err := db.Exec("DELETE FROM routers WHERE id = ?", id)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// ---------------------------------------------------------------------------
// Autodetección de la puerta de enlace
// ---------------------------------------------------------------------------

var defaultViaRe = regexp.MustCompile(`default via (\d+\.\d+\.\d+\.\d+)`)

// DetectGatewayIP devuelve la IP de la puerta de enlace por defecto del
// servidor ("" si no se sabe).
func DetectGatewayIP() string {
	out, err := exec.Command("ip", "route", "show", "default").Output()
	if err != nil {
		return ""
	}
	if m := defaultViaRe.FindSubmatch(out); m != nil {
		return string(m[1])
	}
	return ""
}

var glinetModelRe = regexp.MustCompile(`(?i)GL[.-]?iNet|GL-[A-Z]`)
var glinetDistRe = regexp.MustCompile(`(?i)glinet`)

// probeGatewayModel sondea el modelo del gateway por SSH (ubus system board).
func probeGatewayModel(host, keyPath string) (model, typ string) {
	args := append(sshkey.BaseArgs(keyPath),
		"-o", "ConnectTimeout=3",
		"-o", "ControlMaster=no",
		"root@"+host,
		"ubus call system board",
	)
	cmd := exec.Command("ssh", args...)
	type result struct {
		out []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		out, err := cmd.Output()
		ch <- result{out, err}
	}()
	select {
	case <-time.After(probeTimeout + time.Second):
		_ = cmd.Process.Kill()
		return "", "openwrt"
	case res := <-ch:
		if res.err != nil {
			return "", "openwrt"
		}
		var board struct {
			Model   string `json:"model"`
			Release struct {
				Distribution string `json:"distribution"`
			} `json:"release"`
		}
		if err := json.Unmarshal(res.out, &board); err != nil {
			return "", "openwrt"
		}
		typ = "openwrt"
		if glinetModelRe.MatchString(board.Model) || glinetDistRe.MatchString(board.Release.Distribution) {
			typ = "glinet"
		}
		return board.Model, typ
	}
}

// EnsureInitialRouters es el bootstrap de la tabla routers
// (routerstore.js:126-158). Devuelve la lista final configurada.
func EnsureInitialRouters(db *sql.DB, cfg *config.Config) []adapters.RouterConfig {
	existing := ListRouters(db)
	if len(existing) > 0 {
		return existing
	}

	// 1. Semilla desde ROUTERS_JSON (primer arranque)
	if len(cfg.Routers) > 0 {
		for i, r := range cfg.Routers {
			_, err := AddRouter(db, AddInput{
				Name: r.Name, Host: r.Host, Type: r.Type,
				IsGateway: r.Type == "glinet" || i == 0,
			})
			if err != nil {
				log.Printf("[netpulse] error sembrando router %s: %v", r.Host, err)
			}
		}
		log.Printf("[netpulse] routers sembrados desde ROUTERS_JSON (%d)", len(cfg.Routers))
		return ListRouters(db)
	}

	// 2. Autodetección de la puerta de enlace (nunca en demo forzado)
	if cfg.DemoMode {
		return []adapters.RouterConfig{}
	}
	gwIP := DetectGatewayIP()
	if gwIP == "" {
		log.Printf("[netpulse] sin puerta de enlace detectada; añade routers desde Ajustes")
		return []adapters.RouterConfig{}
	}
	model, typ := probeGatewayModel(gwIP, cfg.SSHKeyPath)
	name := model
	if name == "" {
		name = "Gateway"
	}
	created, err := AddRouter(db, AddInput{Name: name, Host: gwIP, Type: typ, IsGateway: true})
	if err != nil {
		log.Printf("[netpulse] error creando gateway autodetectado: %v", err)
		return ListRouters(db)
	}
	if model == "" {
		model = "modelo desconocido"
	}
	log.Printf("[netpulse] gateway autodetectado: %s (%s, tipo %s). El resto se añade desde Ajustes.",
		created.Host, model, created.Type)
	return ListRouters(db)
}
