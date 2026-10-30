// integrations.go — API de integraciones externas de routers (#1674):
// pushers de routers externos (MikroTik RouterOS, Asuswrt) gestionados desde
// la app con el contrato de agente externo. El slug de la integración es el
// id del router external asociado, así el push del pusher (POST
// /api/ingest/agent con Bearer token) se atribuye a la misma unidad.
package httpapi

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/rintegration"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
)

// integrationItem es la vista HTTP de una integración (nunca la contraseña).
type integrationItem struct {
	Slug        string `json:"slug"`
	Vendor      string `json:"vendor"`
	Mode        string `json:"mode"`
	Host        string `json:"host"`
	Username    string `json:"username"`
	Version     string `json:"version"`
	Arch        string `json:"arch"`
	IntervalS   int    `json:"intervalS"`
	Enabled     bool   `json:"enabled"`
	State       string `json:"state"`
	LastError   string `json:"lastError"`
	LastStartMs int64  `json:"lastStartMs"`
	LastPushMs  int64  `json:"lastPushMs"`
	Fresh       bool   `json:"fresh"`
}

// vendorView es la vista HTTP del manifiesto de un vendor.
type vendorView struct {
	ID          string   `json:"id"`
	Mode        string   `json:"mode"`
	Arches      []string `json:"arches"`
	TagPrefix   string   `json:"tagPrefix"`
	Description string   `json:"description"`
}

// registerIntegrationRoutes registra las rutas /api/integrations/*. Todas
// exigen rol admin salvo el GET de vendors (solo sesión).
func (s *server) registerIntegrationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/integrations/vendors", s.handleIntegrationsVendors)
	mux.Handle("GET /api/integrations", auth.RequireAdmin(http.HandlerFunc(s.handleIntegrationsList)))
	mux.Handle("POST /api/integrations", auth.RequireAdmin(http.HandlerFunc(s.handleIntegrationsCreate)))
	mux.Handle("POST /api/integrations/{slug}/start", auth.RequireAdmin(http.HandlerFunc(s.handleIntegrationStart)))
	mux.Handle("POST /api/integrations/{slug}/stop", auth.RequireAdmin(http.HandlerFunc(s.handleIntegrationStop)))
	mux.Handle("POST /api/integrations/{slug}/check", auth.RequireAdmin(http.HandlerFunc(s.handleIntegrationCheck)))
	mux.Handle("POST /api/integrations/{slug}/update", auth.RequireAdmin(http.HandlerFunc(s.handleIntegrationUpdate)))
	mux.Handle("PUT /api/integrations/{slug}", auth.RequireAdmin(http.HandlerFunc(s.handleIntegrationEdit)))
	mux.Handle("DELETE /api/integrations/{slug}", auth.RequireAdmin(http.HandlerFunc(s.handleIntegrationDelete)))
}

func (s *server) handleIntegrationsVendors(w http.ResponseWriter, _ *http.Request) {
	vs := rintegration.Vendors()
	out := make([]vendorView, 0, len(vs))
	for _, v := range vs {
		out = append(out, vendorView{
			ID: v.ID, Mode: v.Mode, Arches: v.Arches,
			TagPrefix: v.TagPrefix, Description: v.Description,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"vendors": out})
}

func (s *server) integrationsStore() *rintegration.Store {
	return rintegration.NewStore(s.db.DB)
}

func (s *server) integrationFetcher() *rintegration.Fetcher {
	if s.integrations != nil {
		return s.integrations.Fetch()
	}
	return rintegration.NewFetcher()
}

func (s *server) getIntegrationRow(slug string) (rintegration.Row, bool) {
	if s.db == nil {
		return rintegration.Row{}, false
	}
	return s.integrationsStore().Get(slug)
}

// integrationLastPushMs lee agent.last_push_ts.<slug> (segundos unix) y lo
// devuelve en milisegundos, 0 si nunca empujó.
func (s *server) integrationLastPushMs(slug string) int64 {
	if s.db == nil {
		return 0
	}
	sec, err := strconv.ParseInt(kvGet(s.db.DB, "agent.last_push_ts."+slug), 10, 64)
	if err != nil {
		return 0
	}
	return sec * 1000
}

func (s *server) integrationItemOf(row rintegration.Row) integrationItem {
	lastPush := s.integrationLastPushMs(row.Slug)
	fresh := false
	if lastPush > 0 && row.IntervalS > 0 {
		fresh = time.Now().UnixMilli()-lastPush <= int64(row.IntervalS)*3*1000
	}
	// #1696: reconciliar al leer. Una fila en running cuyo último push es más
	// viejo que 3 intervalos no puede ir de sana: la vista la reporta como
	// "stale". Es solo vista (la fila no se toca): en cuanto el pusher vuelva
	// a empujar, el propio cálculo la devuelve a "running".
	state := row.State
	if state == "running" && lastPush > 0 && row.IntervalS > 0 && !fresh {
		state = "stale"
	}
	return integrationItem{
		Slug: row.Slug, Vendor: row.Vendor, Mode: row.Mode, Host: row.Host,
		Username: row.Username, Version: row.Version, Arch: row.Arch,
		IntervalS: row.IntervalS, Enabled: row.Enabled, State: state,
		LastError: row.LastError, LastStartMs: row.LastStartMs,
		LastPushMs: lastPush, Fresh: fresh,
	}
}

func (s *server) integrationList() []integrationItem {
	if s.db == nil {
		return []integrationItem{}
	}
	rows, err := s.integrationsStore().List()
	if err != nil {
		return []integrationItem{}
	}
	out := make([]integrationItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.integrationItemOf(row))
	}
	return out
}

func (s *server) broadcastIntegrations() {
	if s.hub != nil {
		s.hub.Broadcast("integrations", map[string]any{"integrations": s.integrationList()})
	}
}

// watchIntegrations (#1696) registra el callback de salida inesperada de los
// pushers locales (#1674) y reanuda al arrancar las integraciones habilitadas.
// No-op sin supervisor o sin base de datos (demo). Los paros intencionales no
// pasan por el callback: los gestiona el handler de stop.
func (s *server) watchIntegrations() {
	if s.integrations == nil || s.db == nil {
		return
	}
	s.integrations.SetOnExit(func(slug string, exitErr error) {
		// Un pusher es un daemon de larga vida: también la salida limpia
		// (código 0) es inesperada y deja la fila en estado error con su
		// motivo, igual que un arranque fallido.
		reason := "pusher exited unexpectedly (exit code 0)"
		if exitErr != nil {
			reason = exitErr.Error()
		}
		_ = rintegration.NewStore(s.db.DB).SetState(slug, "error", reason)
		s.broadcastIntegrations()
	})
	s.resumeIntegrations()
}

// resumeIntegrations (#1696) reanuda al arrancar cada integración habilitada.
// En modo local arranca el pusher con el binario ya instalado y el fichero de
// entorno 0600 ya existente (el token y la contraseña solo viven ahí); en
// modo ssh el pusher corre dentro del router y no hay nada que arrancar en el
// servidor, pero el estado guardado deja de decir running (no verificado
// hasta el próximo check/start). Un fallo se loga y deja la fila en estado
// error: nunca tira el arranque del servidor.
func (s *server) resumeIntegrations() {
	if s.integrations == nil || s.db == nil {
		return
	}
	store := s.integrationsStore()
	rows, err := store.List()
	if err != nil {
		log.Printf("[netpulse] aviso: integraciones: no se pudo leer la tabla al arrancar (%v)", err)
		return
	}
	for _, row := range rows {
		if !row.Enabled {
			continue
		}
		if row.Mode != "local" {
			// El proceso vive en el router: sin verificar tras el reinicio
			// del servidor; check/start lo reconcilian.
			if row.State == "running" {
				_ = store.SetState(row.Slug, "unverified", "")
			}
			continue
		}
		if s.integrations.Running(row.Slug) {
			continue
		}
		vendor, ok := rintegration.LookupVendor(row.Vendor)
		if !ok {
			_ = store.SetState(row.Slug, "error", "unknown vendor "+row.Vendor)
			log.Printf("[netpulse] aviso: integración %s con vendor desconocido %q al arrancar", row.Slug, row.Vendor)
			continue
		}
		binPath := filepath.Join(s.integrations.Dir(), row.Slug, vendor.Binary)
		if err := s.integrations.Start(row.Slug, nil, binPath); err != nil {
			_ = store.SetState(row.Slug, "error", err.Error())
			log.Printf("[netpulse] aviso: integración %s no se reanudó al arrancar (%v)", row.Slug, err)
			continue
		}
		_ = store.SetState(row.Slug, "running", "")
		_ = store.SetLastStart(row.Slug, time.Now().UnixMilli())
		log.Printf("[netpulse] integración %s reanudada al arrancar", row.Slug)
	}
	s.broadcastIntegrations()
}

func (s *server) handleIntegrationsList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"integrations": s.integrationList()})
}

type integrationCreateBody struct {
	Slug      string `json:"slug"`
	Vendor    string `json:"vendor"`
	Host      string `json:"host"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	IntervalS int    `json:"intervalS"`
	Arch      string `json:"arch"`
}

func (s *server) handleIntegrationsCreate(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	var body integrationCreateBody
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_body", `Se esperaba { "slug", "vendor", "host", "username", "password", "intervalS", "arch"? }`)
		return
	}
	if !agentSlugRe.MatchString(body.Slug) {
		writeError(w, http.StatusBadRequest, "invalid_slug", "slug must match ^[a-z0-9][a-z0-9-]{0,63}$")
		return
	}
	vendor, ok := rintegration.LookupVendor(body.Vendor)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_vendor", "unknown vendor")
		return
	}
	if body.Host == "" || body.Username == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "host, username and password are required")
		return
	}
	if body.IntervalS < 10 || body.IntervalS > 86400 {
		writeError(w, http.StatusBadRequest, "invalid_interval", "intervalS must be between 10 and 86400")
		return
	}
	arch := body.Arch
	if arch == "" {
		arch = vendor.Arches[0]
	} else if !vendorSupportsArch(vendor, arch) {
		writeError(w, http.StatusBadRequest, "invalid_arch", "unsupported arch "+arch)
		return
	}

	store := s.integrationsStore()
	if _, exists := store.Get(body.Slug); exists {
		writeError(w, http.StatusConflict, "already_exists", "integration already exists")
		return
	}

	// El env que se va a escribir tiene que poder arrancar: con la URL en https
	// el pin dejó de ser opcional (#13 en netpulse-integrations), y derivarlo
	// exige que este server alcance su propia URL pública. Mejor un 502 aquí,
	// sin tocar nada, que un alta que muere al segundo siguiente.
	serverURL := s.agentBaseURL(r)
	if err := requirePusherPin(serverURL); err != nil {
		writeError(w, http.StatusBadGateway, "fingerprint_unreachable", err.Error())
		return
	}

	// Resolver y descargar el último release verificado. En modo local se
	// extrae el binario aquí (corre en este host); en modo ssh solo se
	// verifica: el router descarga el asset él mismo.
	fetch := s.integrationFetcher()
	res, err := fetch.ResolveLatest(vendor, arch)
	if err != nil {
		writeError(w, http.StatusBadGateway, "release_fetch_failed", err.Error())
		return
	}
	binPath := ""
	if vendor.Mode == "local" {
		if s.integrations == nil {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "integrations supervisor not configured")
			return
		}
		installDir := filepath.Join(s.integrations.Dir(), body.Slug)
		binPath, err = fetch.Download(res, vendor.Binary, installDir)
		if err != nil {
			writeError(w, http.StatusBadGateway, "download_failed", err.Error())
			return
		}
	} else {
		if s.pool == nil {
			writeError(w, http.StatusServiceUnavailable, "ssh_unavailable", "el servidor no tiene pool SSH")
			return
		}
		if err := fetch.Verify(res); err != nil {
			writeError(w, http.StatusBadGateway, "download_failed", err.Error())
			return
		}
	}

	now := time.Now().UnixMilli()
	row := rintegration.Row{
		Slug: body.Slug, Vendor: vendor.ID, Mode: vendor.Mode, Host: body.Host,
		Username: body.Username, Version: res.Version, Asset: res.AssetName,
		Arch: arch, IntervalS: body.IntervalS, Enabled: true, State: "pending",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.Upsert(row); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	// Router external cuyo id es el slug (push autenticado por token de
	// agente, agent.token.<slug>).
	router, err := routerstore.AddRouter(s.db.DB, routerstore.AddInput{
		Name: body.Slug, Host: body.Host, Type: "external", AgentOnly: true,
	})
	if err != nil || router.ID != body.Slug {
		_ = store.Delete(body.Slug)
		writeError(w, http.StatusInternalServerError, "router_error")
		return
	}
	s.syncRouters()

	rollback := func() {
		if vendor.Mode == "local" && s.integrations != nil {
			_ = s.integrations.Stop(body.Slug)
			_ = s.integrations.Cleanup(body.Slug)
		}
		_ = routerstore.RemoveRouter(s.db.DB, body.Slug)
		s.syncRouters()
		_ = store.Delete(body.Slug)
	}

	token, err := s.rotateAgentTokenAtomic(body.Slug, func(tok string) error {
		env := s.pusherEnv(vendor, row, body.Password, serverURL, tok)
		if vendor.Mode == "local" {
			return s.integrations.Start(body.Slug, env, binPath)
		}
		return s.runSSHInstall(vendor, row, env, res.URL, res.SHA256)
	})
	if err != nil {
		rollback()
		writeError(w, http.StatusBadGateway, "start_failed", err.Error())
		return
	}

	_ = store.SetState(body.Slug, "running", "")
	_ = store.SetLastStart(body.Slug, time.Now().UnixMilli())
	s.broadcastIntegrations()

	item, _ := s.getIntegrationRow(body.Slug)
	writeJSON(w, http.StatusCreated, map[string]any{
		"integration": s.integrationItemOf(item),
		"token":       token,
	})
}

func vendorSupportsArch(v rintegration.Vendor, arch string) bool {
	for _, a := range v.Arches {
		if a == arch {
			return true
		}
	}
	return false
}

// pusherEnv construye el entorno del pusher. En modo ssh el pusher corre en el
// propio router, así que el host del dispositivo es la propia unidad
// (127.0.0.1) en lugar del host SSH de la tabla.
func (s *server) pusherEnv(v rintegration.Vendor, row rintegration.Row, password, serverURL, token string) map[string]string {
	prefix := strings.ToUpper(v.ID) + "_"
	host := row.Host
	if v.Mode == "ssh" {
		host = "127.0.0.1"
	}
	env := map[string]string{
		prefix + "HOST":     host,
		prefix + "USER":     row.Username,
		prefix + "PASS":     password,
		prefix + "INSECURE": "1",
		"NETPULSE_SERVER":   serverURL,
		"NETPULSE_TOKEN":    token,
		"NETPULSE_SLUG":     row.Slug,
		"NETPULSE_INTERVAL": strconv.Itoa(row.IntervalS) + "s",
	}
	for k, v := range pusherPinEnv(serverURL) {
		env[k] = v
	}
	return env
}

// healPusherPin merges the current server pin into a pusher environment,
// healing files written before the pin requirement and refreshing the pin
// after a certificate reissue. It is a no-op when the server URL needs no
// pin (plain HTTP).
func healPusherPin(env map[string]string, serverURL string) map[string]string {
	for k, v := range pusherPinEnv(serverURL) {
		env[k] = v
	}
	return env
}

func (s *server) runSSHInstall(v rintegration.Vendor, row rintegration.Row, env map[string]string, assetURL, sha256 string) error {
	_, err := s.pool.Run(row.Host, sshInstallScript(v, env, assetURL, sha256), 300*time.Second)
	return err
}

func (s *server) handleIntegrationStart(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	row, ok := s.getIntegrationRow(slug)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	vendor, ok := rintegration.LookupVendor(row.Vendor)
	if !ok {
		writeError(w, http.StatusInternalServerError, "invalid_vendor")
		return
	}
	store := s.integrationsStore()
	if vendor.Mode == "local" {
		if s.integrations == nil {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "integrations supervisor not configured")
			return
		}
		if !s.integrations.Running(slug) {
			binPath := filepath.Join(s.integrations.Dir(), slug, vendor.Binary)
			if err := s.integrations.Start(slug, nil, binPath); err != nil && err != rintegration.ErrAlreadyRunning {
				writeError(w, http.StatusBadGateway, "start_failed", err.Error())
				return
			}
		}
	} else {
		if s.pool == nil {
			writeError(w, http.StatusServiceUnavailable, "ssh_unavailable", "el servidor no tiene pool SSH")
			return
		}
		if _, err := s.pool.Run(row.Host, sshServiceScript(vendor, "restart"), 60*time.Second); err != nil {
			writeError(w, http.StatusBadGateway, "ssh_failed", err.Error())
			return
		}
	}
	_ = store.SetState(slug, "running", "")
	_ = store.SetLastStart(slug, time.Now().UnixMilli())
	s.broadcastIntegrations()
	item, _ := s.getIntegrationRow(slug)
	writeJSON(w, http.StatusOK, map[string]any{"integration": s.integrationItemOf(item)})
}

func (s *server) handleIntegrationStop(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	row, ok := s.getIntegrationRow(slug)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	vendor, ok := rintegration.LookupVendor(row.Vendor)
	if !ok {
		writeError(w, http.StatusInternalServerError, "invalid_vendor")
		return
	}
	if vendor.Mode == "local" {
		if s.integrations != nil {
			_ = s.integrations.Stop(slug) // ErrNotRunning es no-op aquí
		}
	} else if s.pool != nil {
		_, _ = s.pool.Run(row.Host, sshServiceScript(vendor, "stop"), 60*time.Second)
	}
	_ = s.integrationsStore().SetState(slug, "stopped", "")
	s.broadcastIntegrations()
	item, _ := s.getIntegrationRow(slug)
	writeJSON(w, http.StatusOK, map[string]any{"integration": s.integrationItemOf(item)})
}

func (s *server) handleIntegrationCheck(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	row, ok := s.getIntegrationRow(slug)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	vendor, ok := rintegration.LookupVendor(row.Vendor)
	if !ok {
		writeError(w, http.StatusInternalServerError, "invalid_vendor")
		return
	}
	running := false
	if vendor.Mode == "local" {
		running = s.integrations != nil && s.integrations.Running(slug)
	} else if s.pool != nil {
		_, err := s.pool.Run(row.Host, sshServiceScript(vendor, "check"), 30*time.Second)
		running = err == nil
	}
	// #1696: un check verificado reconcilia el estado guardado: el unverified
	// que deja un reinicio del servidor (o una fila desfasada) vuelve a
	// running cuando el proceso está demostradamente vivo.
	if running && row.State != "running" {
		_ = s.integrationsStore().SetState(slug, "running", "")
		s.broadcastIntegrations()
		if fresh, ok := s.getIntegrationRow(slug); ok {
			row = fresh
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"integration": s.integrationItemOf(row),
		"running":     running,
	})
}

func (s *server) handleIntegrationUpdate(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	row, ok := s.getIntegrationRow(slug)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	vendor, ok := rintegration.LookupVendor(row.Vendor)
	if !ok {
		writeError(w, http.StatusInternalServerError, "invalid_vendor")
		return
	}
	fetch := s.integrationFetcher()
	res, err := fetch.ResolveLatest(vendor, row.Arch)
	if err != nil {
		writeError(w, http.StatusBadGateway, "release_fetch_failed", err.Error())
		return
	}
	store := s.integrationsStore()
	if vendor.Mode == "local" {
		if s.integrations == nil {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "integrations supervisor not configured")
			return
		}
		_ = s.integrations.Stop(slug)
		installDir := filepath.Join(s.integrations.Dir(), slug)
		if _, err := fetch.Download(res, vendor.Binary, installDir); err != nil {
			_ = store.SetState(slug, "error", err.Error())
			s.broadcastIntegrations()
			writeError(w, http.StatusBadGateway, "download_failed", err.Error())
			return
		}
		// Heal environments created before the pin requirement: the
		// refreshed binary refuses to start without it. Best effort, on
		// error the old file is reused and the start below reports it.
		if env, err := s.integrations.Env(slug); err == nil {
			_ = s.integrations.WriteEnv(slug, healPusherPin(env, s.agentBaseURL(r)))
		}
		if err := s.integrations.Start(slug, nil, filepath.Join(installDir, vendor.Binary)); err != nil {
			_ = store.SetState(slug, "error", err.Error())
			s.broadcastIntegrations()
			writeError(w, http.StatusBadGateway, "start_failed", err.Error())
			return
		}
	} else {
		if s.pool == nil {
			writeError(w, http.StatusServiceUnavailable, "ssh_unavailable", "el servidor no tiene pool SSH")
			return
		}
		if err := fetch.Verify(res); err != nil {
			writeError(w, http.StatusBadGateway, "download_failed", err.Error())
			return
		}
		env := s.pusherEnv(vendor, row, "", s.agentBaseURL(r), "")
		if err := s.runSSHInstall(vendor, row, env, res.URL, res.SHA256); err != nil {
			writeError(w, http.StatusBadGateway, "ssh_failed", err.Error())
			return
		}
	}
	_ = store.SetVersion(slug, res.Version, res.AssetName, row.Arch)
	_ = store.SetState(slug, "running", "")
	_ = store.SetLastStart(slug, time.Now().UnixMilli())
	s.broadcastIntegrations()
	item, _ := s.getIntegrationRow(slug)
	writeJSON(w, http.StatusOK, map[string]any{"integration": s.integrationItemOf(item)})
}

// handleIntegrationEdit actualiza los ajustes mutables de una integración sin
// recrearla: host, usuario, contraseña e intervalo. El fichero de entorno se
// lee, se cambian solo las claves afectadas y se reescribe, de modo que el
// token y la contraseña omitida se conservan (nunca viven en la base de datos).
func (s *server) handleIntegrationEdit(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	row, ok := s.getIntegrationRow(slug)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	vendor, ok := rintegration.LookupVendor(row.Vendor)
	if !ok {
		writeError(w, http.StatusInternalServerError, "invalid_vendor")
		return
	}

	var body struct {
		Host      *string `json:"host"`
		Username  *string `json:"username"`
		Password  *string `json:"password"`
		IntervalS *int    `json:"intervalS"`
	}
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_body", "")
		return
	}

	changed := false
	if body.Host != nil {
		h := strings.TrimSpace(*body.Host)
		if h == "" {
			writeError(w, http.StatusBadRequest, "invalid_input", "host cannot be empty")
			return
		}
		row.Host = h
		changed = true
	}
	if body.Username != nil {
		u := strings.TrimSpace(*body.Username)
		if u == "" {
			writeError(w, http.StatusBadRequest, "invalid_input", "username cannot be empty")
			return
		}
		row.Username = u
		changed = true
	}
	if body.IntervalS != nil {
		if *body.IntervalS < 10 || *body.IntervalS > 86400 {
			writeError(w, http.StatusBadRequest, "invalid_interval")
			return
		}
		row.IntervalS = *body.IntervalS
		changed = true
	}
	password := ""
	if body.Password != nil {
		password = *body.Password
		changed = true
	}
	if !changed {
		writeError(w, http.StatusBadRequest, "invalid_input", "nothing to update")
		return
	}

	prefix := strings.ToUpper(vendor.ID) + "_"
	host := row.Host
	if vendor.Mode == "ssh" {
		host = "127.0.0.1"
	}
	store := s.integrationsStore()

	if vendor.Mode == "local" {
		if s.integrations == nil {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "integrations supervisor not configured")
			return
		}
		env, err := s.integrations.Env(slug)
		if err != nil {
			_ = store.SetState(slug, "error", err.Error())
			s.broadcastIntegrations()
			writeError(w, http.StatusConflict, "env_unavailable", err.Error())
			return
		}
		env[prefix+"HOST"] = host
		env[prefix+"USER"] = row.Username
		if password != "" {
			env[prefix+"PASS"] = password
		}
		env["NETPULSE_INTERVAL"] = strconv.Itoa(row.IntervalS) + "s"
		healPusherPin(env, s.agentBaseURL(r))
		if err := s.integrations.WriteEnv(slug, env); err != nil {
			_ = store.SetState(slug, "error", err.Error())
			s.broadcastIntegrations()
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		_ = s.integrations.Stop(slug)
		binPath := filepath.Join(s.integrations.Dir(), slug, vendor.Binary)
		if err := s.integrations.Start(slug, nil, binPath); err != nil && err != rintegration.ErrAlreadyRunning {
			_ = store.SetState(slug, "error", err.Error())
			s.broadcastIntegrations()
			writeError(w, http.StatusBadGateway, "start_failed", err.Error())
			return
		}
	} else {
		if s.pool == nil {
			writeError(w, http.StatusServiceUnavailable, "ssh_unavailable", "el servidor no tiene pool SSH")
			return
		}
		raw, err := s.pool.Run(row.Host, sshEnvReadScript(vendor), 30*time.Second)
		if err != nil {
			writeError(w, http.StatusBadGateway, "ssh_failed", err.Error())
			return
		}
		env := rintegration.ParseEnv(raw)
		env[prefix+"HOST"] = host
		env[prefix+"USER"] = row.Username
		if password != "" {
			env[prefix+"PASS"] = password
		}
		env["NETPULSE_INTERVAL"] = strconv.Itoa(row.IntervalS) + "s"
		healPusherPin(env, s.agentBaseURL(r))
		if _, err := s.pool.Run(row.Host, sshEnvWriteScript(vendor, env), 60*time.Second); err != nil {
			_ = store.SetState(slug, "error", err.Error())
			s.broadcastIntegrations()
			writeError(w, http.StatusBadGateway, "ssh_failed", err.Error())
			return
		}
	}

	if err := store.Upsert(row); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	_ = store.SetState(slug, "running", "")
	_ = store.SetLastStart(slug, time.Now().UnixMilli())
	s.broadcastIntegrations()
	item, _ := s.getIntegrationRow(slug)
	writeJSON(w, http.StatusOK, map[string]any{"integration": s.integrationItemOf(item)})
}

func (s *server) handleIntegrationDelete(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	row, ok := s.getIntegrationRow(slug)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	vendor, _ := rintegration.LookupVendor(row.Vendor)
	if vendor.Mode == "local" {
		if s.integrations != nil {
			_ = s.integrations.Stop(slug)
			_ = s.integrations.Cleanup(slug)
		}
	} else if s.pool != nil {
		_, _ = s.pool.Run(row.Host, sshUninstallScript(vendor), 60*time.Second)
	}
	_ = s.integrationsStore().Delete(slug)
	_ = routerstore.RemoveRouter(s.db.DB, slug)
	s.syncRouters()
	if s.db != nil {
		_, _ = s.db.Exec("DELETE FROM kv WHERE key = ?", agentTokenKey(slug))
	}
	s.broadcastIntegrations()
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Scripts POSIX de instalación/gestión por SSH (modo ssh, p. ej. asuswrt)
// ---------------------------------------------------------------------------

func sshInstallScript(v rintegration.Vendor, env map[string]string, assetURL, sha256 string) string {
	bin := "/usr/sbin/" + v.Binary
	init := "/etc/init.d/" + v.Binary
	envFile := "/etc/" + v.Binary + ".env"

	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var envBlock strings.Builder
	for _, k := range keys {
		envBlock.WriteString(k)
		envBlock.WriteString("=")
		envBlock.WriteString(rintegration.EscapeEnvValue(env[k]))
		envBlock.WriteString("\n")
	}

	return `#!/bin/sh
set -e
BIN="` + bin + `"
INIT="` + init + `"
ENV_FILE="` + envFile + `"
SERVER="` + env["NETPULSE_SERVER"] + `"
ASSET_URL="` + assetURL + `"
SHA256="` + sha256 + `"

# Preflight: el router debe alcanzar el server.
if command -v curl >/dev/null 2>&1; then
	curl -fsSL -m 10 "$SERVER/api/health" >/dev/null 2>&1 || { echo "preflight: cannot reach $SERVER/api/health"; exit 10; }
else
	wget -q -T 10 -O /dev/null "$SERVER/api/health" || { echo "preflight: cannot reach $SERVER/api/health"; exit 10; }
fi

# Parar el servicio previo (el proceso vivo mantiene el binario abierto).
[ -x "$INIT" ] && "$INIT" stop >/dev/null 2>&1 || true

# Descargar el asset desde la URL ya verificada en el server.
if command -v curl >/dev/null 2>&1; then
	curl -fsSL -m 600 -o /tmp/` + v.Binary + `.tar.gz "$ASSET_URL"
else
	wget -q -T 60 -O /tmp/` + v.Binary + `.tar.gz "$ASSET_URL"
fi

# Verificar sha256 antes de instalar.
GOT=$(sha256sum /tmp/` + v.Binary + `.tar.gz | awk '{print $1}')
[ "$GOT" = "$SHA256" ] || { echo "sha256 mismatch (want $SHA256, got $GOT)"; exit 21; }

# Extraer y mover de forma atómica (.new -> rename).
mkdir -p /tmp/` + v.Binary + `.extract
tar -xzf /tmp/` + v.Binary + `.tar.gz -C /tmp/` + v.Binary + `.extract
SRC=$(find /tmp/` + v.Binary + `.extract -type f -name "` + v.Binary + `" | head -n1)
[ -n "$SRC" ] || { echo "binary not found in archive"; exit 22; }
cp "$SRC" "$BIN.new"
chmod 0755 "$BIN.new"
mv -f "$BIN.new" "$BIN"
rm -rf /tmp/` + v.Binary + `.extract /tmp/` + v.Binary + `.tar.gz

# Env (chmod 600): solo se escribe en el alta; los updates preservan el
# fichero existente (la contraseña y el token solo viven ahí).
if [ ! -f "$ENV_FILE" ]; then
cat > "$ENV_FILE" <<'ENVEOF'
` + envBlock.String() + `ENVEOF
chmod 600 "$ENV_FILE"
fi

# Init procd.
cat > "$INIT" <<'INITEOF'
#!/bin/sh /etc/rc.common
START=99
STOP=10
USE_PROCD=1
BIN="/usr/sbin/` + v.Binary + `"
ENV_FILE="/etc/` + v.Binary + `.env"
start_service() {
	[ -x "$BIN" ] || return 1
	procd_open_instance ` + v.Binary + `
	procd_set_param command "$BIN"
	procd_set_param respawn 3600 5 5
	procd_set_param stdout 1
	procd_set_param stderr 1
	procd_set_param file "$ENV_FILE"
	procd_close_instance
}
INITEOF
chmod 0755 "$INIT"
"$INIT" enable
"$INIT" restart
`
}

// sshEnvReadScript prints the environment file of an integration running
// inside the router, so the server can merge changes without ever storing the
// token or the password.
func sshEnvReadScript(v rintegration.Vendor) string {
	return "cat /etc/" + v.Binary + ".env"
}

// sshEnvWriteScript replaces the environment file inside the router and
// restarts the service. Values come already escaped by EscapeEnvValue and the
// heredoc is quoted, so the shell does not expand anything.
func sshEnvWriteScript(v rintegration.Vendor, env map[string]string) string {
	init := "/etc/init.d/" + v.Binary
	envFile := "/etc/" + v.Binary + ".env"

	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var envBlock strings.Builder
	for _, k := range keys {
		envBlock.WriteString(k)
		envBlock.WriteString("=")
		envBlock.WriteString(rintegration.EscapeEnvValue(env[k]))
		envBlock.WriteString("\n")
	}

	return `#!/bin/sh
set -e
INIT="` + init + `"
ENV_FILE="` + envFile + `"
cat > "$ENV_FILE.new" <<'ENVEOF'
` + envBlock.String() + `ENVEOF
chmod 600 "$ENV_FILE.new"
mv -f "$ENV_FILE.new" "$ENV_FILE"
if [ -x "$INIT" ]; then
	"$INIT" stop >/dev/null 2>&1 || true
	"$INIT" start >/dev/null 2>&1 || true
fi
`
}

func sshServiceScript(v rintegration.Vendor, action string) string {
	init := "/etc/init.d/" + v.Binary
	switch action {
	case "start":
		return "test -x " + init + " && " + init + " enable && " + init + " start || true"
	case "stop":
		return "test -x " + init + " && " + init + " stop || true"
	case "restart":
		return "test -x " + init + " && " + init + " restart || true"
	case "check":
		return "pgrep -f " + v.Binary + " >/dev/null 2>&1"
	}
	return "true"
}

func sshUninstallScript(v rintegration.Vendor) string {
	init := "/etc/init.d/" + v.Binary
	bin := "/usr/sbin/" + v.Binary
	envFile := "/etc/" + v.Binary + ".env"
	return fmt.Sprintf(
		"test -x %s && %s stop || true; test -x %s && %s disable || true; rm -f %s %s %s",
		init, init, init, init, init, bin, envFile,
	)
}
