// rtlconsole.go — #639: sondeo HTTP de la consola de switches RTLPlayground
// (Keephome KP-9000 con firmware FreeSwitchOS/RTLPlayground) para poblar
// firmware + uptime en routers cuyo agente es external (beacon UDP, #291).
//
// El datagrama del beacon no puede crecer (el banco de código del 8051 está
// lleno), así que el server consulta la consola web del propio switch con
// cadencia lenta. La web sirve UNA petición a la vez (uIP/8051), por eso el
// poll es serial por switch y nunca en paralelo (InFlight).
//
// El resultado se cachea por slug con {firmware, bootUnix}: a partir del
// bootUnix el uptime se deriva del reloj del server entre polls (no hace
// falta re-consultar cada 30 s), y el poll solo re-sincroniza bootUnix con
// la cadencia configurada (NETPULSE_RTL_POLL_S, default 300).
package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
)

// rtlConsoleEntry: resultado del último poll OK de la consola del switch.
type rtlConsoleEntry struct {
	Firmware  string    // sw_ver de /information.json ("v0.1.0-e1fa080-dirty")
	Model     string    // hw_ver ("keepLink KP-9000-9XHML-X V3.1")
	BootUnix  time.Time // now - uptimeSec del poll; el uptime se deriva de aquí
	PolledAt  time.Time // cuándo se hizo el poll (para la cadencia)
	InFlight  bool      // hay un poll en curso (serial por slug)
	FailedAt  time.Time // último fallo (backoff para no martillear un host no-RTL)
	FailCount int
}

// rtlConsoleInfo es la respuesta de /information.json del firmware (solo los
// campos que necesitamos; el JSON tiene más).
type rtlConsoleInfo struct {
	SwVer string `json:"sw_ver"`
	HwVer string `json:"hw_ver"`
}

// rtlConsoleCache: estado del sondeo por slug. Protegido por su mutex.
type rtlConsoleCache struct {
	mu   sync.Mutex
	pass string
	sec  int // cadencia en segundos
	byID map[string]*rtlConsoleEntry
}

func newRtlConsoleCache(pass string, pollSec int) *rtlConsoleCache {
	if pollSec <= 0 {
		pollSec = 300
	}
	if pass == "" {
		pass = "1234"
	}
	return &rtlConsoleCache{pass: pass, sec: pollSec, byID: map[string]*rtlConsoleEntry{}}
}

// snapshot devuelve la entrada cacheada para adjuntar System al payload del
// beacon. Si la entrada es vieja o no existe y no hay poll en curso, lanza el
// poll en background (no bloquea el handler del beacon). Devuelve nil hasta
// que el primer poll tenga éxito (el beacon de turno no lleva System).
func (c *rtlConsoleCache) snapshot(slug, host string) *rtlConsoleEntry {
	now := time.Now()
	c.mu.Lock()
	e := c.byID[slug]
	if e == nil {
		e = &rtlConsoleEntry{}
		c.byID[slug] = e
	}
	needPoll := e.Firmware == "" || now.Sub(e.PolledAt) >= time.Duration(c.sec)*time.Second
	// Backoff tras fallos: reintentar como mucho cada max(sec, 15 min) si el
	// host no habla el protocolo (login fallido), para no martillearlo.
	if needPoll && e.FailCount > 0 && now.Sub(e.FailedAt) < 15*time.Minute {
		needPoll = false
	}
	if needPoll && !e.InFlight {
		e.InFlight = true
		c.mu.Unlock()
		go c.poll(slug, host)
		c.mu.Lock()
	}
	out := e
	c.mu.Unlock()
	if out.Firmware == "" {
		return nil
	}
	return out
}

// invalidate marca la entrada para re-poll inmediato (reboot detectado por el
// seq del beacon: el bootUnix cacheado ya no es válido).
func (c *rtlConsoleCache) invalidate(slug string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.byID[slug]; e != nil {
		e.PolledAt = time.Time{}
	}
}

// poll consulta la consola del switch (login + /information.json + /cmd time)
// y guarda el resultado. Serial por slug (InFlight) y con timeouts cortos:
// la web del firmware atiende una conexión cada vez.
func (c *rtlConsoleCache) poll(slug, host string) {
	entry, err := c.fetch(host)
	c.mu.Lock()
	e := c.byID[slug]
	if e == nil {
		e = &rtlConsoleEntry{}
		c.byID[slug] = e
	}
	e.InFlight = false
	if err != nil {
		e.FailCount++
		e.FailedAt = time.Now()
		c.mu.Unlock()
		log.Printf("[netpulse:rtl] poll %s (%s) falló: %v", slug, host, err)
		return
	}
	e.Firmware = entry.Firmware
	e.Model = entry.Model
	e.BootUnix = entry.BootUnix
	e.PolledAt = time.Now()
	e.FailCount = 0
	c.mu.Unlock()
}

// fetch hace login en la consola y lee /information.json (sw_ver/hw_ver) y el
// uptime por POST /cmd "time". Devuelve bootUnix = now - uptimeSec.
func (c *rtlConsoleCache) fetch(host string) (*rtlConsoleEntry, error) {
	base := "http://" + host
	// No seguir el 302 del login (Location: index.html): necesitamos la
	// cookie del Set-Cookie y la respuesta es el propio redirect.
	client := &http.Client{
		Timeout: 8 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// 1) login: POST /login pwd=<pass> → 302 + Set-Cookie: session=...
	form := url.Values{"pwd": {c.pass}}
	req, err := http.NewRequest(http.MethodPost, base+"/login", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	cookie := ""
	for _, ck := range resp.Cookies() {
		if ck.Name == "session" && ck.Value != "" {
			cookie = ck.Name + "=" + ck.Value
			break
		}
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("login HTTP %d", resp.StatusCode)
	}
	if cookie == "" {
		return nil, fmt.Errorf("login sin cookie de sesión")
	}

	// 2) /information.json → sw_ver + hw_ver
	infoReq, err := http.NewRequest(http.MethodGet, base+"/information.json", nil)
	if err != nil {
		return nil, err
	}
	infoReq.Header.Set("Cookie", cookie)
	infoResp, err := client.Do(infoReq)
	if err != nil {
		return nil, err
	}
	infoBody, _ := io.ReadAll(io.LimitReader(infoResp.Body, 4096))
	infoResp.Body.Close()
	if infoResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("information.json HTTP %d", infoResp.StatusCode)
	}
	var info rtlConsoleInfo
	if err := json.Unmarshal(infoBody, &info); err != nil {
		return nil, fmt.Errorf("information.json inválido: %w", err)
	}

	// 3) POST /cmd "time" → uptime en segundos (hex, p. ej. "0x00028e9d").
	cmdReq, err := http.NewRequest(http.MethodPost, base+"/cmd", strings.NewReader("time"))
	if err != nil {
		return nil, err
	}
	cmdReq.Header.Set("Cookie", cookie)
	cmdResp, err := client.Do(cmdReq)
	if err != nil {
		return nil, err
	}
	cmdBody, _ := io.ReadAll(io.LimitReader(cmdResp.Body, 256))
	cmdResp.Body.Close()
	if cmdResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("/cmd time HTTP %d", cmdResp.StatusCode)
	}
	upHex := strings.TrimSpace(string(cmdBody))
	upHex = strings.TrimPrefix(upHex, "0x")
	upHex = strings.TrimPrefix(upHex, "0X")
	upSec, err := strconv.ParseUint(upHex, 16, 32)
	if err != nil {
		return nil, fmt.Errorf("/cmd time no es un contador hex (%q): %w", strings.TrimSpace(string(cmdBody)), err)
	}

	now := time.Now()
	return &rtlConsoleEntry{
		Firmware: info.SwVer, Model: info.HwVer,
		BootUnix: now.Add(-time.Duration(upSec) * time.Second),
	}, nil
}

// attachSystem adjunta la sección System (board + uptime) al payload de un
// beacon periódico si hay datos de consola cacheados. Es el punto de
// inyección: polledFromAgent mapea System.Board→board y SysInfo.Uptime→
// uptimeSec, y buildRouter pinta Firmware/Uptime sin cambios en el front.
func (s *server) attachRTLConsole(slug, host string, pl *probe.Payload) {
	if s.rtlConsole == nil || pl == nil {
		return
	}
	e := s.rtlConsole.snapshot(slug, host)
	if e == nil {
		return
	}
	// Board.Release es un struct anónimo; se rellena campo a campo igual que
	// boardWithDesc en los tests de buildRouter.
	b := &probe.BoardInfo{}
	b.Model = e.Model
	b.Release.Description = "RTLPlayground " + e.Firmware
	sd := &probe.SystemData{
		Board:   b,
		SysInfo: &probe.SysInfo{Uptime: time.Since(e.BootUnix).Seconds()},
	}
	pl.Data.System = sd
}
