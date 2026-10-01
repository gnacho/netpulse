// routeros_api.go — cliente mínimo del API binario nativo de RouterOS
// (puerto 8728), alternativa a la REST API (routeros_live.go) para routers
// cuyo admin mantiene `www`/`www-ssl` cerrados por seguridad y solo permite
// `api`/`api-ssl` — el mismo protocolo que usa Winbox y la mayoría de
// herramientas de monitorización de RouterOS (LibreNMS, Zabbix...).
//
// Implementación mínima a propósito: login moderno (RouterOS 6.43+, una sola
// sentencia, sin el challenge-response MD5 legacy) + sentencias de solo
// lectura ("/algo/print"). Sin tags ni consultas concurrentes sobre la misma
// conexión — una conexión nueva por poll, igual que el patrón ya usado para
// SNMP (snmp_live.go). ponytail: puerto fijo 8728 sin TLS; api-ssl/8729
// queda para más adelante si algún admin lo necesita (requiere manejar el
// certificado del lado RouterOS, que muchas instalaciones no tienen listo).
//
// Protocolo: cada "palabra" lleva un prefijo de longitud de tamaño variable
// (1-5 bytes según la magnitud); una "sentencia" es una secuencia de
// palabras terminada por una palabra de longitud 0. Ver la wiki de MikroTik,
// "API" — no hay biblioteca oficial en Go, y las de terceros son deps que
// este proyecto prefiere evitar para algo de ~150 líneas.
package adapters

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

type routerOSAPIConn struct {
	conn net.Conn
	r    *bufio.Reader
}

func dialRouterOSAPI(host, user, pass string, timeout time.Duration) (*routerOSAPIConn, error) {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, "8728"), timeout)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	c := &routerOSAPIConn{conn: conn, r: bufio.NewReader(conn)}
	if err := c.writeSentence("/login", "=name="+user, "=password="+pass); err != nil {
		conn.Close()
		return nil, err
	}
	reply, err := c.readSentence()
	if err != nil {
		conn.Close()
		return nil, err
	}
	if len(reply) == 0 || reply[0] != "!done" {
		conn.Close()
		return nil, fmt.Errorf("routeros api: login: %s", strings.Join(reply, " "))
	}
	return c, nil
}

func (c *routerOSAPIConn) Close() { _ = c.conn.Close() }

// query ejecuta un comando de solo lectura (p.ej. "/system/resource/print")
// y devuelve las filas !re como mapas atributo→valor.
func (c *routerOSAPIConn) query(cmd string) ([]map[string]string, error) {
	if err := c.writeSentence(cmd); err != nil {
		return nil, err
	}
	var rows []map[string]string
	for {
		sentence, err := c.readSentence()
		if err != nil {
			return nil, err
		}
		if len(sentence) == 0 {
			continue
		}
		switch sentence[0] {
		case "!done":
			return rows, nil
		case "!trap":
			return nil, fmt.Errorf("routeros api %s: %s", cmd, strings.Join(sentence[1:], " "))
		case "!re":
			row := map[string]string{}
			for _, w := range sentence[1:] {
				w = strings.TrimPrefix(w, "=")
				if k, v, ok := strings.Cut(w, "="); ok {
					row[k] = v
				}
			}
			rows = append(rows, row)
		}
	}
}

func (c *routerOSAPIConn) writeSentence(words ...string) error {
	for _, w := range words {
		if err := writeRouterOSWord(c.conn, w); err != nil {
			return err
		}
	}
	return writeRouterOSWord(c.conn, "")
}

func (c *routerOSAPIConn) readSentence() ([]string, error) {
	var words []string
	for {
		w, err := readRouterOSWord(c.r)
		if err != nil {
			return nil, err
		}
		if w == "" {
			return words, nil
		}
		words = append(words, w)
	}
}

func writeRouterOSWord(w io.Writer, word string) error {
	if err := writeRouterOSLen(w, len(word)); err != nil {
		return err
	}
	_, err := w.Write([]byte(word))
	return err
}

// writeRouterOSLen codifica la longitud con el prefijo de tamaño variable
// del protocolo RouterOS API.
func writeRouterOSLen(w io.Writer, l int) error {
	switch {
	case l < 0x80:
		_, err := w.Write([]byte{byte(l)})
		return err
	case l < 0x4000:
		v := l | 0x8000
		return binary.Write(w, binary.BigEndian, uint16(v))
	case l < 0x200000:
		buf := []byte{byte(l>>16) | 0xC0, byte(l >> 8), byte(l)}
		_, err := w.Write(buf)
		return err
	case l < 0x10000000:
		buf := []byte{byte(l>>24) | 0xE0, byte(l >> 16), byte(l >> 8), byte(l)}
		_, err := w.Write(buf)
		return err
	default:
		if _, err := w.Write([]byte{0xF0}); err != nil {
			return err
		}
		return binary.Write(w, binary.BigEndian, uint32(l))
	}
}

func readRouterOSWord(r *bufio.Reader) (string, error) {
	l, err := readRouterOSLen(r)
	if err != nil {
		return "", err
	}
	if l == 0 {
		return "", nil
	}
	buf := make([]byte, l)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

// readRouterOSLen decodifica el prefijo de longitud de tamaño variable.
func readRouterOSLen(r *bufio.Reader) (int, error) {
	c0, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	switch {
	case c0&0x80 == 0:
		return int(c0), nil
	case c0&0xC0 == 0x80:
		c1, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		return int(c0&^0xC0)<<8 | int(c1), nil
	case c0&0xE0 == 0xC0:
		b := make([]byte, 2)
		if _, err := io.ReadFull(r, b); err != nil {
			return 0, err
		}
		return int(c0&^0xE0)<<16 | int(b[0])<<8 | int(b[1]), nil
	case c0&0xF0 == 0xE0:
		b := make([]byte, 3)
		if _, err := io.ReadFull(r, b); err != nil {
			return 0, err
		}
		return int(c0&^0xF0)<<24 | int(b[0])<<16 | int(b[1])<<8 | int(b[2]), nil
	case c0 == 0xF0:
		b := make([]byte, 4)
		if _, err := io.ReadFull(r, b); err != nil {
			return 0, err
		}
		return int(binary.BigEndian.Uint32(b)), nil
	default:
		return 0, fmt.Errorf("routeros api: invalid length prefix 0x%02x", c0)
	}
}

// pollRouterROSAPI sondea RouterOS vía su API binario nativo — funciona
// incluso si www/www-ssl están cerrados, siempre que `api` esté habilitado
// para la IP del servidor NetPulse (lo más común: es el mismo puerto que usa
// Winbox y la mayoría de herramientas de monitorización de terceros).
func (l *Live) pollRouterROSAPI(cfg RouterConfig) (*routerPolled, error) {
	conn, err := dialRouterOSAPI(cfg.Host, cfg.RouterOSUser, cfg.RouterOSPassword, 10*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	resRows, err := conn.query("/system/resource/print")
	if err != nil {
		return nil, err
	}
	if len(resRows) == 0 {
		return nil, fmt.Errorf("routeros api %s: sin filas de /system/resource/print", cfg.ID)
	}
	idRows, err := conn.query("/system/identity/print")
	if err != nil {
		return nil, err
	}
	if len(idRows) == 0 {
		return nil, fmt.Errorf("routeros api %s: sin filas de /system/identity/print", cfg.ID)
	}

	rr := resRows[0]
	res := routerOSResource{
		CPULoad:      rr["cpu-load"],
		FreeMemory:   rr["free-memory"],
		TotalMemory:  rr["total-memory"],
		Uptime:       rr["uptime"],
		Version:      rr["version"],
		BoardName:    rr["board-name"],
		Architecture: rr["architecture-name"],
	}
	id := routerOSIdentity{Name: idRows[0]["name"]}
	p, err := buildRouterOSPoll(cfg, res, id)
	if err != nil {
		return nil, err
	}
	// Puertos y leases DHCP: best-effort — un router sin DHCP configurado o
	// con permisos más estrechos en el usuario del API sigue reportando
	// vitals aunque estas dos fallen (igual que el resto de sondas opcionales
	// del pipeline OpenWrt: null/vacío en vez de tumbar todo el poll).
	var ifRows []map[string]string
	if rows, err := conn.query("/interface/print"); err == nil {
		ifRows = rows
		p.ports = routerOSPorts(rows)
		p.brMac = routerOSBridgeMAC(rows)
	}
	if rows, err := conn.query("/ip/dhcp-server/lease/print"); err == nil {
		p.leases = routerOSLeases(rows)
	}
	if rows, err := conn.query("/ip/arp/print"); err == nil {
		p.arp, p.arpStale = routerOSArp(rows)
	}
	if rows, err := conn.query("/interface/bridge/host/print"); err == nil {
		p.fdb = routerOSFDB(rows)
	}
	// Tráfico WAN (issue: "network traffic" en Overview daba 0 para
	// RouterOS): identificar el interfaz de la ruta por defecto y calcular
	// su bps por delta de contadores acumulados entre polls — mismo enfoque
	// que GetNetDev de OpenWrt sobre /proc/net/dev, ponytail: solo IPv4
	// (0.0.0.0/0); una ruta por defecto IPv6-only no se detecta.
	if routeRows, err := conn.query("/ip/route/print"); err == nil {
		if wanIface := routerOSWANInterface(routeRows); wanIface != "" {
			p.net = l.routerOSNetRate(cfg.ID, ifRows, wanIface)
		}
	}
	return p, nil
}

// netByteSample es la última muestra de contadores rx/tx de un interfaz
// RouterOS, para calcular bps por delta entre polls.
type netByteSample struct {
	at      time.Time
	rxBytes uint64
	txBytes uint64
}

// routerOSWANInterface busca la ruta por defecto (0.0.0.0/0) en las filas de
// /ip/route/print y devuelve su interfaz de salida, o "" si no hay ninguna.
// Con gateway IP normal (NAT doméstico típico), RouterOS no expone un
// "interface" resuelto en /ip/route/print — solo la IP del siguiente salto,
// que no basta para identificar el interfaz sin resolver ARP/vecinos
// (fuera de alcance por ahora). Con PPPoE/VPN (sin gateway IP tradicional),
// el propio campo "gateway" YA es el nombre del interfaz (p.ej.
// "telekom-pppoe") — ese caso sí se resuelve aquí.
func routerOSWANInterface(routeRows []map[string]string) string {
	for _, row := range routeRows {
		if row["dst-address"] != "0.0.0.0/0" {
			continue
		}
		if iface := row["interface"]; iface != "" {
			return iface
		}
		if gw := row["gateway"]; gw != "" && net.ParseIP(gw) == nil {
			return gw
		}
	}
	return ""
}

// routerOSNetRate calcula bps rx/tx del interfaz WAN por delta de los
// contadores acumulados de /interface/print entre este poll y el anterior.
// nil en la primera muestra (sin delta posible) o si el contador se reinició
// — misma convención que el resto del pipeline (null en vez de un pico falso).
func (l *Live) routerOSNetRate(routerID string, ifRows []map[string]string, wanIface string) *NetDevBps {
	var rxBytes, txBytes uint64
	found := false
	for _, row := range ifRows {
		if row["name"] != wanIface {
			continue
		}
		rxBytes, _ = strconv.ParseUint(row["rx-byte"], 10, 64)
		txBytes, _ = strconv.ParseUint(row["tx-byte"], 10, 64)
		found = true
		break
	}
	if !found {
		return nil
	}
	now := time.Now()
	l.mu.Lock()
	prev, ok := l.roNetPrev[routerID]
	l.roNetPrev[routerID] = netByteSample{at: now, rxBytes: rxBytes, txBytes: txBytes}
	l.mu.Unlock()
	if !ok {
		return nil
	}
	dt := now.Sub(prev.at).Seconds()
	if dt <= 0 || rxBytes < prev.rxBytes || txBytes < prev.txBytes {
		return nil
	}
	rx := float64(rxBytes-prev.rxBytes) * 8 / dt
	tx := float64(txBytes-prev.txBytes) * 8 / dt
	return &NetDevBps{RxBps: &rx, TxBps: &tx}
}

// routerOSPorts mapea /interface/print a EthPort — solo interfaces físicas
// ("ether"): las virtuales (bridge, vlan, wireguard...) no son "puertos" en
// el sentido que espera el panel de puertos del frontend.
func routerOSPorts(rows []map[string]string) []EthPort {
	ports := make([]EthPort, 0, len(rows))
	for _, row := range rows {
		if row["type"] != "ether" {
			continue
		}
		name := row["name"]
		ports = append(ports, EthPort{
			ID:    name,
			Label: name,
			Up:    row["running"] == "true",
			Iface: name,
		})
	}
	return ports
}

// routerOSLeases mapea /ip/dhcp-server/lease/print a DhcpLease (MAC en
// mayúsculas, misma convención que el resto del pipeline DHCP).
func routerOSLeases(rows []map[string]string) []DhcpLease {
	leases := make([]DhcpLease, 0, len(rows))
	for _, row := range rows {
		mac := strings.ToUpper(row["mac-address"])
		if mac == "" {
			continue
		}
		var expAt *int64
		if exp := row["expires-after"]; exp != "" {
			if sec, err := parseRouterOSUptime(exp); err == nil && sec > 0 {
				at := time.Now().Unix() + int64(sec)
				expAt = &at
			}
		}
		leases = append(leases, DhcpLease{
			MAC:            mac,
			IP:             row["address"],
			Hostname:       row["host-name"],
			LeaseExpiresAt: expAt,
		})
	}
	return leases
}

// routerOSBridgeMAC extrae la MAC principal del equipo (bridge o primer interface físico ether).
func routerOSBridgeMAC(rows []map[string]string) string {
	// 1. Interfaz bridge
	for _, row := range rows {
		if row["type"] == "bridge" {
			mac := strings.ToUpper(strings.TrimSpace(row["mac-address"]))
			if mac != "" && mac != "00:00:00:00:00:00" {
				return mac
			}
		}
	}
	// 2. Primera interfaz ethernet física
	for _, row := range rows {
		if row["type"] == "ether" {
			mac := strings.ToUpper(strings.TrimSpace(row["mac-address"]))
			if mac != "" && mac != "00:00:00:00:00:00" {
				return mac
			}
		}
	}
	// 3. Fallback a cualquier interfaz con MAC válida
	for _, row := range rows {
		mac := strings.ToUpper(strings.TrimSpace(row["mac-address"]))
		if mac != "" && mac != "00:00:00:00:00:00" {
			return mac
		}
	}
	return ""
}

// routerOSArp mapea /ip/arp/print a map[MAC]IP y un conjunto de stale MACs.
func routerOSArp(rows []map[string]string) (map[string]string, map[string]bool) {
	arp := make(map[string]string, len(rows))
	stale := make(map[string]bool)
	for _, row := range rows {
		if row["disabled"] == "true" || row["invalid"] == "true" {
			continue
		}
		mac := strings.ToUpper(strings.TrimSpace(row["mac-address"]))
		ip := strings.TrimSpace(row["address"])
		if mac != "" && ip != "" {
			arp[mac] = ip
			if row["status"] == "failed" || row["status"] == "stale" {
				stale[mac] = true
			}
		}
	}
	return arp, stale
}

// routerOSFDB mapea /interface/bridge/host/print a map[MAC]port, excluyendo
// las entradas locales del propio router (local=true).
func routerOSFDB(rows []map[string]string) map[string]string {
	fdb := make(map[string]string, len(rows))
	for _, row := range rows {
		if row["local"] == "true" {
			continue
		}
		mac := strings.ToUpper(strings.TrimSpace(row["mac-address"]))
		port := strings.TrimSpace(row["on-interface"])
		if mac != "" && port != "" {
			fdb[mac] = port
		}
	}
	return fdb
}

