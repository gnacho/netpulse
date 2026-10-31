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
	"net"
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
	rows, err := db.Query("SELECT id, name, host, type, is_gateway, agent_only, firmware_target, created_at, snmp_enabled, snmp_community, snmp_port, snmp_poll_interval, ssh_port, temp_threshold, console_polling, disabled FROM routers ORDER BY is_gateway DESC, created_at ASC")
	if err != nil {
		return []adapters.RouterConfig{}
	}
	defer rows.Close()
	out := []adapters.RouterConfig{}
	for rows.Next() {
		var r adapters.RouterConfig
		var gw, ao, snmpEn, snmpPort, snmpInterval, sshPort, consolePoll, disabled int
		var name, ft, snmpComm sql.NullString
		var tt sql.NullInt64
		if err := rows.Scan(&r.ID, &name, &r.Host, &r.Type, &gw, &ao, &ft, &r.CreatedAt, &snmpEn, &snmpComm, &snmpPort, &snmpInterval, &sshPort, &tt, &consolePoll, &disabled); err != nil {
			continue
		}
		// DEFAULT 1 de la migración, pero una fila insertada antes de la
		// columna con valor 0 explícito cuenta como desactivada; el NULL no
		// aplica (NOT NULL DEFAULT 1).
		r.ConsolePolling = consolePoll == 1
		r.Disabled = disabled == 1
		r.Name = name.String
		r.FirmwareTarget = ft.String
		r.IsGateway = gw == 1
		r.AgentOnly = ao == 1
		r.SnmpEnabled = snmpEn == 1
		r.SnmpCommunity = snmpComm.String
		r.SnmpPort = snmpPort
		r.SnmpPollInterval = snmpInterval
		r.SSHPort = sshPort
		if tt.Valid {
			v := int(tt.Int64)
			r.TempThreshold = &v
		}
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
	// FirmwareTarget: versión objetivo (issue #241). "" = sin comprobar.
	FirmwareTarget string
	// SNMP (issue #309): credenciales para sondeo SNMP.
	SnmpEnabled      bool
	SnmpCommunity    string
	SnmpPort         int
	SnmpPollInterval int // issue #414; 0 = default 60
	// SSHPort (issue #605): puerto SSH del router (dropbear en puerto no
	// estándar). 0/ausente → 22.
	SSHPort int
	// ConsolePolling (issue #863): sondeo HTTP de la consola del switch.
	// nil = default ON (el sondeo no se desactiva por omisión).
	ConsolePolling *bool
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
	snmpEn := 0
	if in.SnmpEnabled {
		snmpEn = 1
	}
	snmpInterval := in.SnmpPollInterval
	if snmpInterval <= 0 {
		snmpInterval = 60
	}
	snmpPort := in.SnmpPort
	if snmpPort <= 0 {
		snmpPort = 161
	}
	sshPort := in.SSHPort
	if sshPort <= 0 {
		sshPort = 22
	}
	consolePoll := 1
	if in.ConsolePolling != nil && !*in.ConsolePolling {
		consolePoll = 0
	}
	if _, err := tx.Exec(
		"INSERT INTO routers (id, name, host, type, is_gateway, agent_only, firmware_target, created_at, snmp_enabled, snmp_community, snmp_port, snmp_poll_interval, ssh_port, console_polling) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		id, name, in.Host, in.Type, gw, ao, in.FirmwareTarget, now, snmpEn, in.SnmpCommunity, snmpPort, snmpInterval, sshPort, consolePoll,
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

// UpdateInput son los campos editables de un router. Todos opcionales:
// nil = no tocar el campo. Se usa desde PUT /api/config/routers/{id}.
type UpdateInput struct {
	Name      *string // "" = limpiar (usar host)
	Host      *string
	Type      *string
	IsGateway *bool
	AgentOnly *bool
	// Disabled (issue #1085): nil = no tocar; true = pausar la unidad
	// (sin sondeo ni alertas, insignia y fuera de desplegables).
	Disabled *bool
	// FirmwareTarget: versión objetivo (issue #241). nil = no tocar; "" = limpiar.
	FirmwareTarget *string
	// SNMP (issue #309): nil = no tocar; puntero a bool/string/int = aplicar.
	SnmpEnabled      *bool
	SnmpCommunity    *string
	SnmpPort         *int
	SnmpPollInterval *int // issue #414
	// SSHPort (issue #605): puerto SSH; nil = no tocar.
	SSHPort *int
	// TempThreshold (issue #716): umbral de alerta por temperatura alta (°C).
	// nil = no tocar; <= 0 = reset a NULL (default 65); > 0 = fijar.
	TempThreshold *int
	// ConsolePolling (issue #863): sondeo HTTP de la consola del switch.
	// nil = no tocar. DEFAULT ON (la columna migra a 1).
	ConsolePolling *bool
}

// UpdateRouter actualiza un router existente por id. Si IsGateway pasa a true,
// el flag se quita del resto (un solo gateway, transacción). Si el router no
// existe devuelve false (caller responde 404).
func UpdateRouter(db *sql.DB, id string, in UpdateInput) (adapters.RouterConfig, bool) {
	tx, err := db.Begin()
	if err != nil {
		return adapters.RouterConfig{}, false
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRow("SELECT 1 FROM routers WHERE id = ?", id).Scan(&exists); err != nil {
		return adapters.RouterConfig{}, false
	}

	if in.IsGateway != nil && *in.IsGateway {
		if _, err := tx.Exec("UPDATE routers SET is_gateway = 0 WHERE id != ?", id); err != nil {
			return adapters.RouterConfig{}, false
		}
	}

	sets := []string{}
	args := []any{}
	if in.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *in.Name)
	}
	if in.Host != nil {
		sets = append(sets, "host = ?")
		args = append(args, *in.Host)
	}
	if in.Type != nil {
		sets = append(sets, "type = ?")
		args = append(args, *in.Type)
	}
	if in.IsGateway != nil {
		gw := 0
		if *in.IsGateway {
			gw = 1
		}
		sets = append(sets, "is_gateway = ?")
		args = append(args, gw)
	}
	if in.AgentOnly != nil {
		ao := 0
		if *in.AgentOnly {
			ao = 1
		}
		sets = append(sets, "agent_only = ?")
		args = append(args, ao)
	}
	if in.Disabled != nil {
		v := 0
		if *in.Disabled {
			v = 1
		}
		sets = append(sets, "disabled = ?")
		args = append(args, v)
	}
	if in.FirmwareTarget != nil {
		sets = append(sets, "firmware_target = ?")
		args = append(args, *in.FirmwareTarget)
	}
	if in.SnmpEnabled != nil {
		v := 0
		if *in.SnmpEnabled {
			v = 1
		}
		sets = append(sets, "snmp_enabled = ?")
		args = append(args, v)
	}
	if in.SnmpCommunity != nil {
		sets = append(sets, "snmp_community = ?")
		args = append(args, *in.SnmpCommunity)
	}
	if in.SnmpPort != nil {
		sets = append(sets, "snmp_port = ?")
		args = append(args, *in.SnmpPort)
	}
	if in.SnmpPollInterval != nil {
		v := *in.SnmpPollInterval
		if v <= 0 {
			v = 60
		}
		sets = append(sets, "snmp_poll_interval = ?")
		args = append(args, v)
	}
	if in.SSHPort != nil {
		v := *in.SSHPort
		if v <= 0 {
			v = 22
		}
		sets = append(sets, "ssh_port = ?")
		args = append(args, v)
	}
	if in.ConsolePolling != nil {
		v := 0
		if *in.ConsolePolling {
			v = 1
		}
		sets = append(sets, "console_polling = ?")
		args = append(args, v)
	}
	if in.TempThreshold != nil {
		if *in.TempThreshold > 0 {
			sets = append(sets, "temp_threshold = ?")
			args = append(args, *in.TempThreshold)
		} else {
			// <= 0 = reset a default (NULL), misma convención que snmp_poll_interval.
			sets = append(sets, "temp_threshold = NULL")
		}
	}
	if len(sets) > 0 {
		args = append(args, id)
		if _, err := tx.Exec("UPDATE routers SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...); err != nil {
			return adapters.RouterConfig{}, false
		}
	}

	if err := tx.Commit(); err != nil {
		return adapters.RouterConfig{}, false
	}
	for _, r := range ListRouters(db) {
		if r.ID == id {
			return r, true
		}
	}
	return adapters.RouterConfig{}, false
}

// ---------------------------------------------------------------------------
// Autodetección de la puerta de enlace
// ---------------------------------------------------------------------------

var (
	routeViaRe    = regexp.MustCompile(`via (\d+\.\d+\.\d+\.\d+)`)
	routeSubnetRe = regexp.MustCompile(`^(\d+\.\d+\.\d+\.\d+)/\d+`)
	routeSrcRe    = regexp.MustCompile(`src (\d+\.\d+\.\d+\.\d+)`)
)

// DetectGatewayIP devuelve la IP de la puerta de enlace LAN por defecto del
// servidor ("" si no se sabe).
//
// Dos escenarios (#1701):
//   - Servidor dedicado en la LAN: la ruta por defecto sale por una via
//     privada (RFC1918) y esa es la gateway.
//   - Servidor on-box (el propio router): la via por defecto es pública o
//     CGNAT (la del ISP), que NO es la gateway LAN; la gateway es el propio
//     router y se toma la IP fuente de la primera ruta de subred privada.
//     Con via pública/CGNAT y sin subred privada, devuelve "" (antes
//     devolvía la dirección del ISP y generaba "Gateway offline" falso).
func DetectGatewayIP() string {
	out, err := exec.Command("ip", "-4", "route", "show").Output()
	if err != nil {
		return ""
	}
	return gatewayIPFromRoutes(out)
}

// gatewayIPFromRoutes elige la gateway a partir de la salida de
// `ip -4 route show`. Es pura para poder probarla sin `ip`.
func gatewayIPFromRoutes(out []byte) string {
	var viaFallback string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "default" {
			if m := routeViaRe.FindStringSubmatch(line); m != nil {
				ip := net.ParseIP(m[1])
				if ip != nil && ip.IsPrivate() {
					return m[1]
				}
				if viaFallback == "" {
					viaFallback = m[1]
				}
			}
			continue
		}
	}
	// Ninguna via privada: on-box o sin via (pppoe). La gateway LAN es el
	// propio router: src de la primera ruta de subred privada.
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] == "default" {
			continue
		}
		if !routeSubnetRe.MatchString(fields[0]) {
			continue
		}
		m := routeSrcRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		ip := net.ParseIP(m[1])
		if ip != nil && ip.IsPrivate() {
			return m[1]
		}
	}
	return viaFallback
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
