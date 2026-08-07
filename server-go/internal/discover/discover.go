// Package discover — descubrimiento de routers candidatos en la LAN
// (paridad src/discover.js, SPEC §7.7):
//  1. Subred /24 derivada del gateway por defecto (`ip route`).
//  2. Barrido TCP del puerto 22 en los 254 hosts (concurrencia 100; NO ping).
//  3. Firma OpenWrt: POST http://<ip>/ubus (cualquier JSON-RPC, incluso
//     "access denied", delata uhttpd+ubus; GL.iNet redirige a su UI → firma
//     gl-ui por https).
//  4. Probe SSH con la clave propia → {authorized, model}.
//
// Caché de 60 s (force=1 la ignora).
package discover

import (
	"bytes"
	"crypto/tls"
	"database/sql"
	"io"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/routerstore"
	"github.com/gnacho/netpulse/server-go/internal/sshkey"
)

const (
	cacheMS      = 60_000
	tcpTimeout   = 800 * time.Millisecond
	httpTimeout  = 1500 * time.Millisecond
	sshTimeout   = 4 * time.Second
	scanWorkers  = 100
	probeWorkers = 16
)

// Result es un candidato a router.
type Result struct {
	Host       string  `json:"host"`
	IsGateway  bool    `json:"isGateway"`
	Authorized bool    `json:"authorized"`
	Model      *string `json:"model"`
	Configured bool    `json:"configured"`
}

// Response es la respuesta de /api/config/discover.
type Response struct {
	At      int64    `json:"at,omitempty"` // la respuesta no_gateway de Node NO incluye `at`
	Subnet  *string  `json:"subnet"`
	Results []Result `json:"results"`
	Cached  bool     `json:"cached"`
	Error   string   `json:"error,omitempty"`
}

var (
	cacheMu sync.Mutex
	cache   Response
)

// tcpOpen: true si el puerto TCP responde antes del timeout.
func tcpOpen(host string, port int, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, itoa(port)), timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func itoa(n int) string {
	var buf [4]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// pool ejecuta fn sobre items con concurrencia limitada.
func pool[T any](items []T, size int, fn func(T) *T) []*T {
	out := make([]*T, len(items))
	var idx int64
	var mu sync.Mutex
	var wg sync.WaitGroup
	workers := size
	if len(items) < workers {
		workers = len(items)
	}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				if idx >= int64(len(items)) {
					mu.Unlock()
					return
				}
				i := idx
				idx++
				mu.Unlock()
				out[i] = fn(items[i])
			}
		}()
	}
	wg.Wait()
	res := make([]*T, 0, len(out))
	for _, r := range out {
		if r != nil {
			res = append(res, r)
		}
	}
	return res
}

func httpClient() *http.Client {
	return &http.Client{
		Timeout: httpTimeout,
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, // firma, no confianza
			DisableKeepAlives:   true,
			MaxConnsPerHost:     1,
			TLSHandshakeTimeout: httpTimeout,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // necesitamos el status crudo (301/302)
		},
	}
}

type probeResp struct {
	status int
	body   string
}

var ubusProbeBody = `{"jsonrpc":"2.0","id":1,"method":"call","params":["00000000000000000000000000000000","session","login",{"username":"netpulse-probe","password":""}]}`

// postUbus: POST /ubus crudo (http u https) — {status, body} o nil.
func postUbus(host string, useHTTPS bool) *probeResp {
	scheme := "http"
	port := "80"
	if useHTTPS {
		scheme = "https"
		port = "443"
	}
	req, err := http.NewRequest("POST", scheme+"://"+host+":"+port+"/ubus", bytes.NewReader([]byte(ubusProbeBody)))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := httpClient().Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 8192))
	return &probeResp{status: res.StatusCode, body: string(body)}
}

// getRoot: GET / (http/https) — {status, body} o nil.
func getRoot(host string, useHTTPS bool) *probeResp {
	scheme := "http"
	port := "80"
	if useHTTPS {
		scheme = "https"
		port = "443"
	}
	res, err := httpClient().Get(scheme + "://" + host + ":" + port + "/")
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 16384))
	return &probeResp{status: res.StatusCode, body: string(body)}
}

var glUIRe = regexp.MustCompile(`(?i)gl-ui|GL\.iNet|glinet`)

// probeUbus: true si el host huele a OpenWrt/GL.iNet.
func probeUbus(host string) bool {
	isUbus := func(r *probeResp) bool {
		return r != nil && (strings.Contains(r.body, "jsonrpc") || strings.Contains(r.body, "ubus_rpc_session"))
	}
	httpRes := postUbus(host, false)
	if isUbus(httpRes) {
		return true
	}
	if httpRes != nil && (httpRes.status == 301 || httpRes.status == 302 || httpRes.status == 307 || httpRes.status == 308) {
		if isUbus(postUbus(host, true)) {
			return true
		}
		if root := getRoot(host, true); root != nil && glUIRe.MatchString(root.body) {
			return true
		}
	}
	return false
}

// probeSsh prueba SSH con la clave propia; devuelve (authorized, model).
func probeSsh(host, keyPath string) (bool, *string) {
	args := append(sshkey.BaseArgs(keyPath),
		"-o", "ConnectTimeout=2",
		"-o", "ControlMaster=no",
		"root@"+host,
		"ubus call system board | jsonfilter -e @.model",
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
	case <-time.After(sshTimeout):
		_ = cmd.Process.Kill()
		return false, nil
	case res := <-ch:
		if res.err != nil {
			return false, nil
		}
		model := strings.TrimSpace(string(res.out))
		if model == "" {
			return true, nil
		}
		return true, &model
	}
}

// selfIPs: IPs propias del servidor (para excluirlas del barrido).
func selfIPs() map[string]bool {
	out, err := exec.Command("sh", "-c", `ip -o -4 addr show | grep -oP "inet \K[0-9.]+"`).Output()
	ips := map[string]bool{}
	if err != nil {
		return ips
	}
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			ips[l] = true
		}
	}
	return ips
}

// Routers escanea la LAN y devuelve candidatos (paridad discoverRouters).
// db se usa solo para marcar los ya configurados.
func Routers(db *sql.DB, keyPath string, force bool) Response {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	now := time.Now().UnixMilli()
	if !force && now-cache.At < cacheMS {
		c := cache
		c.Cached = true
		return c
	}

	gwIP := routerstore.DetectGatewayIP()
	if gwIP == "" {
		return Response{Subnet: nil, Results: []Result{}, Cached: false, Error: "no_gateway"}
	}
	parts := strings.Split(gwIP, ".")
	prefix := strings.Join(parts[:3], ".")
	subnet := prefix + ".0/24"

	own := selfIPs()
	hosts := make([]string, 0, 254)
	for i := 1; i <= 254; i++ {
		h := prefix + "." + itoa(i)
		if !own[h] {
			hosts = append(hosts, h)
		}
	}
	alive := pool(hosts, scanWorkers, func(h string) *string {
		if tcpOpen(h, 22, tcpTimeout) {
			return &h
		}
		return nil
	})

	configured := map[string]bool{}
	for _, r := range routerstore.ListRouters(db) {
		configured[r.Host] = true
	}
	type candidate struct{ h string }
	cands := make([]candidate, 0, len(alive))
	for _, h := range alive {
		cands = append(cands, candidate{*h})
	}
	found := pool(cands, probeWorkers, func(c candidate) *candidate {
		if !probeUbus(c.h) {
			return nil
		}
		return &c
	})

	results := make([]Result, 0, len(found))
	for _, c := range found {
		authorized, model := probeSsh(c.h, keyPath)
		results = append(results, Result{
			Host:       c.h,
			IsGateway:  c.h == gwIP,
			Authorized: authorized,
			Model:      model,
			Configured: configured[c.h],
		})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].IsGateway != results[j].IsGateway {
			return results[i].IsGateway
		}
		return results[i].Host < results[j].Host
	})

	cache = Response{At: now, Subnet: &subnet, Results: results, Cached: false}
	return cache
}
