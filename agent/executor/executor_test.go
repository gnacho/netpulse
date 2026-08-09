package executor

import (
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeRunner registra los comandos ejecutados y devuelve respuestas canned.
type fakeRunner struct {
	calls    []string
	responses map[string]int // prefix → exitCode (0 si no está)
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{responses: map[string]int{}}
}

func (f *fakeRunner) Run(name string, args ...string) (string, int) {
	cmd := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, cmd)
	// Buscar respuesta por prefix.
	for prefix, code := range f.responses {
		if strings.HasPrefix(cmd, prefix) {
			if code == 0 {
				return "ok", 0
			}
			return "", code
		}
	}
	return "ok", 0 // default success
}

func TestValidateRejectsUnknownKind(t *testing.T) {
	err := Validate(Op{Kind: "rm_rf", Args: map[string]string{}})
	if err == nil {
		t.Fatal("unknown kind should be rejected")
	}
}

func TestValidateRejectsShellMetachars(t *testing.T) {
	err := Validate(Op{
		Kind: "uci_set",
		Args: map[string]string{
			"config":  "dhcp",
			"section": "@dnsmasq[0]",
			"option":  "server",
			"value":   "1.1.1.1; rm -rf /", // shell injection
		},
	})
	if err == nil {
		t.Fatal("shell metachars in value should be rejected")
	}
}

func TestValidateAcceptsValidOp(t *testing.T) {
	err := Validate(Op{
		Kind: "uci_set",
		Args: map[string]string{
			"config":  "dhcp",
			"section": "@dnsmasq[0]",
			"option":  "server",
			"value":   "1.1.1.1",
		},
	})
	if err != nil {
		t.Fatalf("valid op rejected: %v", err)
	}
}

func TestValidateRejectsMissingArg(t *testing.T) {
	err := Validate(Op{
		Kind: "uci_set",
		Args: map[string]string{"config": "dhcp", "section": "foo", "option": "bar"},
		// missing "value"
	})
	if err == nil {
		t.Fatal("missing arg should be rejected")
	}
}

func TestValidateServiceAction(t *testing.T) {
	// Acciones válidas (start/stop añadidos en Fase 17.1 para módulos que
	// escuchan en puerto, p. ej. parar AdGuard antes de reconfigurarlo).
	for _, action := range []string{"restart", "reload", "enable", "disable", "start", "stop"} {
		if err := Validate(Op{Kind: "service", Args: map[string]string{"name": "dnsmasq", "action": action}}); err != nil {
			t.Fatalf("action %q should be accepted: %v", action, err)
		}
	}
	// Acción inválida
	if err := Validate(Op{Kind: "service", Args: map[string]string{"name": "dnsmasq", "action": "restart;;rm"}}); err == nil {
		t.Fatal("action with shell metachars should be rejected")
	}
}

// --- Fase 17.2: uci_add + secciones @[-1] ---

func TestValidateUciAdd(t *testing.T) {
	valid := Op{Kind: "uci_add", Args: map[string]string{"config": "wireless", "type": "wifi-iface"}}
	if err := Validate(valid); err != nil {
		t.Fatalf("uci_add válido rechazado: %v", err)
	}
	// type con shell metachars → rechazado
	bad := Op{Kind: "uci_add", Args: map[string]string{"config": "wireless", "type": "wifi-iface; rm -rf /"}}
	if err := Validate(bad); err == nil {
		t.Fatal("uci_add type con metachars debería rechazarse")
	}
	// config inválido
	bad2 := Op{Kind: "uci_add", Args: map[string]string{"config": "wireless;", "type": "wifi-iface"}}
	if err := Validate(bad2); err == nil {
		t.Fatal("uci_add config inválido debería rechazarse")
	}
	// uci_delete_section válido (sección nombrada y @referencia)
	del := Op{Kind: "uci_delete_section", Args: map[string]string{"config": "network", "section": "guest"}}
	if err := Validate(del); err != nil {
		t.Fatalf("uci_delete_section válido rechazado: %v", err)
	}
	del2 := Op{Kind: "uci_delete_section", Args: map[string]string{"config": "firewall", "section": "@zone[2]"}}
	if err := Validate(del2); err != nil {
		t.Fatalf("uci_delete_section @referencia rechazado: %v", err)
	}
}

func TestValidateSectionLastIndex(t *testing.T) {
	// @wifi-iface[-1] (última sección) válido para uci_set tras uci_add
	op := Op{Kind: "uci_set", Args: map[string]string{"config": "wireless", "section": "@wifi-iface[-1]", "option": "ssid", "value": "NetPulse-Guest"}}
	if err := Validate(op); err != nil {
		t.Fatalf("@wifi-iface[-1] debería aceptarse: %v", err)
	}
	// índice numérico sigue válido
	op2 := Op{Kind: "uci_set", Args: map[string]string{"config": "wireless", "section": "@wifi-iface[0]", "option": "ssid", "value": "Main"}}
	if err := Validate(op2); err != nil {
		t.Fatalf("@wifi-iface[0] debería aceptarse: %v", err)
	}
	// negativo distinto de -1 → rechazado
	op3 := Op{Kind: "uci_set", Args: map[string]string{"config": "wireless", "section": "@wifi-iface[-2]", "option": "ssid", "value": "X"}}
	if err := Validate(op3); err == nil {
		t.Fatal("@wifi-iface[-2] debería rechazarse (solo -1)")
	}
}

// --- Fase 17.1: validación de los nuevos Kinds ---

func TestValidateDownloadURLAllowlist(t *testing.T) {
	// URL oficial válida
	valid := Op{Kind: "download", Args: map[string]string{
		"url":  "https://github.com/AdguardTeam/AdGuardHome/releases/download/v0.107.52/AdGuardHome_linux_arm64.tar.gz",
		"dest": "/tmp/AdGuardHome.tar.gz",
	}}
	if err := Validate(valid); err != nil {
		t.Fatalf("valid download URL rejected: %v", err)
	}
	// URL maliciosa (dominio distinto) → rechazo
	bad := Op{Kind: "download", Args: map[string]string{
		"url":  "https://evil.example.com/AdGuardHome.tar.gz",
		"dest": "/tmp/x.tar.gz",
	}}
	if err := Validate(bad); err == nil {
		t.Fatal("non-allowlisted download URL should be rejected")
	}
	// URL oficial pero path fuera de allowlist (otro repo) → rechazo
	bad2 := Op{Kind: "download", Args: map[string]string{
		"url":  "https://github.com/evil/repo/releases/download/v1.0/payload",
		"dest": "/tmp/x.tar.gz",
	}}
	if err := Validate(bad2); err == nil {
		t.Fatal("non-AdguardTeam github path should be rejected")
	}
}

func TestValidateFilePathAllowlist(t *testing.T) {
	for _, p := range []string{"/etc/AdGuardHome.yaml", "/tmp/agh.tar.gz", "/usr/bin/AdGuardHome", "/usr/lib/AdGuardHome/dns"} {
		if err := Validate(Op{Kind: "write_file", Args: map[string]string{"path": p, "content_b64": "Zm9v"}}); err != nil {
			t.Errorf("path %q should be accepted: %v", p, err)
		}
	}
	for _, p := range []string{"/etc/passwd;rm", "/etc/../etc/shadow", "/root/.ssh/id_rsa", "/var/log/x", "/home/nacho/x"} {
		if err := Validate(Op{Kind: "write_file", Args: map[string]string{"path": p, "content_b64": "Zm9v"}}); err == nil {
			t.Errorf("path %q should be rejected", p)
		}
	}
}

func TestValidateChmodMode(t *testing.T) {
	for _, m := range []string{"755", "644", "600", "0755", "1777"} {
		if err := Validate(Op{Kind: "chmod", Args: map[string]string{"mode": m, "path": "/usr/bin/AdGuardHome"}}); err != nil {
			t.Errorf("mode %q should be accepted: %v", m, err)
		}
	}
	for _, m := range []string{"999", "rwxr-xr-x", "755;rm", "abc"} {
		if err := Validate(Op{Kind: "chmod", Args: map[string]string{"mode": m, "path": "/usr/bin/AdGuardHome"}}); err == nil {
			t.Errorf("mode %q should be rejected", m)
		}
	}
}

func TestValidateApkInstall(t *testing.T) {
	if err := Validate(Op{Kind: "apk_install", Args: map[string]string{"package": "adguard-home"}}); err != nil {
		t.Fatalf("valid apk_install rejected: %v", err)
	}
	if err := Validate(Op{Kind: "apk_install", Args: map[string]string{"package": "adguard-home;rm"}}); err == nil {
		t.Fatal("apk_install with shell metachars should be rejected")
	}
}

func TestValidateExtractTarball(t *testing.T) {
	if err := Validate(Op{Kind: "extract_tarball", Args: map[string]string{"src": "/tmp/agh.tar.gz", "dest": "/tmp/agh"}}); err != nil {
		t.Fatalf("valid extract_tarball rejected: %v", err)
	}
	// dest fuera de allowlist
	if err := Validate(Op{Kind: "extract_tarball", Args: map[string]string{"src": "/tmp/agh.tar.gz", "dest": "/home/nacho"}}); err == nil {
		t.Fatal("extract_tarball dest outside allowlist should be rejected")
	}
}

func TestValidateMv(t *testing.T) {
	if err := Validate(Op{Kind: "mv", Args: map[string]string{"src": "/tmp/agh-x/AdGuardHome", "dest": "/usr/bin/AdGuardHome"}}); err != nil {
		t.Fatalf("valid mv rejected: %v", err)
	}
	// dest fuera de allowlist
	if err := Validate(Op{Kind: "mv", Args: map[string]string{"src": "/tmp/x", "dest": "/etc/passwd;rm"}}); err == nil {
		t.Fatal("mv with malicious dest should be rejected")
	}
	// src con traversal
	if err := Validate(Op{Kind: "mv", Args: map[string]string{"src": "/tmp/../etc/shadow", "dest": "/tmp/x"}}); err == nil {
		t.Fatal("mv src with traversal should be rejected")
	}
}

// TestWriteFileCreatesParentDir: write_file debe crear /etc/AdGuardHome/ si
// no existe (MkdirAll del dir padre).
func TestWriteFileCreatesParentDir(t *testing.T) {
	parent := "/tmp/netpulse-mkdir-test-" + t.Name()
	defer os.RemoveAll(parent)
	path := parent + "/AdGuardHome/AdGuardHome.yaml"
	spec := allowlist["write_file"]
	if code := spec.exec(nil, map[string]string{"path": path, "content_b64": "Zm9v"}); code != 0 {
		t.Fatalf("write_file exec failed: exit %d", code)
	}
	if _, err := os.ReadFile(path); err != nil {
		t.Fatalf("file not written (dir padre no creado?): %v", err)
	}
}

// TestWriteFileExec verifica el exec de write_file escribe el contenido real.
// Usa un tmpdir (no fakeRunner) porque exec usa os.WriteFile directamente.
func TestWriteFileExec(t *testing.T) {
	// Invoco exec directamente con un path /tmp real y limpio después.
	// El path debe casar con reFilePath (^/tmp/...).
	path := "/tmp/netpulse-exec-test-" + t.Name()
	defer os.Remove(path)

	content := "bind_port: 3000\n"
	b64 := "YmluZF9wb3J0OiAzMDAwCg==" // base64 de content
	spec := allowlist["write_file"]
	if spec.exec == nil {
		t.Fatal("write_file has no exec")
	}
	if code := spec.exec(nil, map[string]string{"path": path, "content_b64": b64}); code != 0 {
		t.Fatalf("write_file exec failed: exit %d", code)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not written: %v", err)
	}
	if string(data) != content {
		t.Fatalf("content mismatch: got %q want %q", data, content)
	}
}

// TestWriteFileRejectsTraversal: path con .. debe ser rechazado por exec
// (defense in depth: aunque el regex lo pille, exec lo revalida).
func TestWriteFileRejectsTraversal(t *testing.T) {
	spec := allowlist["write_file"]
	// "/tmp/../../../etc/x" no casa el regex → Validate lo rechaza.
	// Pero comprobemos exec con un path que SÍ casa el regex pero tiene ..:
	// filepath.Clean("/tmp/a/../b") = "/tmp/b" (válido). Para forzar el
	// rechazo en exec, pasamos un path que Clean cambie (no debería casar
	// el regex, pero si alguien lo bypassa, exec lo para).
	// Como no podemos bypassar el regex desde Validate, este test documenta
	// la defensa: si path != Clean(path), exit 1.
	path := "/tmp/a/./b"
	if code := spec.exec(nil, map[string]string{"path": path, "content_b64": "Zm9v"}); code == 0 {
		t.Fatal("exec should reject path where Clean(path) != path")
	}
}

// TestApplyPlanConDownloadWriteChmod: plan que mezcla los nuevos Kinds con
// los existentes, verificando que Apply despacha exec vs build correctamente.
func TestApplyPlanConDownloadWriteChmod(t *testing.T) {
	fr := newFakeRunner()
	// download/extract/chmod via fakeRunner (build path). write_file via exec
	// (escribe de verdad en /tmp). service via fakeRunner.
	tmpFile := "/tmp/netpulse-plan-test-" + t.Name()
	defer os.Remove(tmpFile)

	e := &Executor{run: fr, now: time.Now, gwTarget: "192.168.1.1"}
	ops := []Op{
		{Kind: "download", Args: map[string]string{
			"url":  "https://github.com/AdguardTeam/AdGuardHome/releases/download/v0.107.52/AdGuardHome_linux_arm64.tar.gz",
			"dest": tmpFile,
		}, Desc: "Download AdGuard tarball"},
		{Kind: "write_file", Args: map[string]string{
			"path":       "/tmp/netpulse-plan-test-cfg.yaml",
			"content_b64": "YmluZF9wb3J0OiAzMDAwCg==",
		}, Desc: "Write AdGuard config"},
		{Kind: "chmod", Args: map[string]string{"mode": "755", "path": tmpFile}, Desc: "Make executable"},
		{Kind: "service", Args: map[string]string{"name": "adguardhome", "action": "start"}, Desc: "Start AdGuard"},
	}
	defer os.Remove("/tmp/netpulse-plan-test-cfg.yaml")

	res := e.Apply(ops)
	if res.Status != "applied" {
		t.Fatalf("expected applied, got %s: %s", res.Status, res.Error)
	}
	// Verifica que los comandos build se ejecutaron (download, chmod, service).
	got := strings.Join(fr.calls, "\n")
	for _, want := range []string{"uclient-fetch https:", "chmod 755", "/etc/init.d/adguardhome start"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing call %q in:\n%s", want, got)
		}
	}
	// Y que write_file escribió el fichero config.
	if _, err := os.ReadFile("/tmp/netpulse-plan-test-cfg.yaml"); err != nil {
		t.Errorf("config file not written: %v", err)
	}
}

func TestApplySuccess(t *testing.T) {
	fr := newFakeRunner()
	e := &Executor{run: fr, now: time.Now, gwTarget: "192.168.1.1"}

	ops := []Op{
		{Kind: "uci_set", Args: map[string]string{"config": "dhcp", "section": "@dnsmasq[0]", "option": "server", "value": "1.1.1.1"}, Desc: "Set DNS upstream"},
		{Kind: "uci_commit", Args: map[string]string{"config": "dhcp"}, Desc: "Commit dhcp"},
	}

	res := e.Apply(ops)
	if res.Status != "applied" {
		t.Fatalf("expected applied, got %s (error=%s)", res.Status, res.Error)
	}
	if res.Snapshot != "dhcp" {
		t.Fatalf("snapshot should list dhcp, got %q", res.Snapshot)
	}

	// Verificar que se hizo snapshot (uci export dhcp) antes de ejecutar
	foundSnap := false
	for _, c := range fr.calls {
		if strings.Contains(c, "uci export dhcp") {
			foundSnap = true
			break
		}
	}
	if !foundSnap {
		t.Fatal("should have done uci export for snapshot")
	}

	// Verificar commit
	foundCommit := false
	for _, c := range fr.calls {
		if c == "uci commit dhcp" {
			foundCommit = true
			break
		}
	}
	if !foundCommit {
		t.Fatal("should have committed dhcp")
	}
}

func TestApplyFailingOpReverts(t *testing.T) {
	fr := newFakeRunner()
	// Hacer que el segundo op (commit) falle
	fr.responses["uci commit dhcp"] = 1

	e := &Executor{run: fr, now: time.Now, gwTarget: "192.168.1.1"}

	ops := []Op{
		{Kind: "uci_set", Args: map[string]string{"config": "dhcp", "section": "@dnsmasq[0]", "option": "server", "value": "1.1.1.1"}, Desc: "Set DNS"},
		{Kind: "uci_commit", Args: map[string]string{"config": "dhcp"}, Desc: "Commit"},
	}

	res := e.Apply(ops)
	if res.Status != "failed" {
		t.Fatalf("expected failed, got %s", res.Status)
	}

	// Verificar que se hizo revert de los staged changes
	foundRevert := false
	for _, c := range fr.calls {
		if strings.Contains(c, "uci revert dhcp") {
			foundRevert = true
			break
		}
	}
	if !foundRevert {
		t.Fatal("should have reverted staged changes on failure")
	}
}

func TestApplyHealthcheckFailRollsBack(t *testing.T) {
	fr := newFakeRunner()
	// Hacer que el ping falle (healthcheck)
	fr.responses["ping"] = 1

	e := &Executor{run: fr, now: time.Now, gwTarget: "192.168.1.1"}

	ops := []Op{
		{Kind: "uci_set", Args: map[string]string{"config": "dhcp", "section": "@dnsmasq[0]", "option": "server", "value": "8.8.8.8"}, Desc: "Change DNS"},
		{Kind: "uci_commit", Args: map[string]string{"config": "dhcp"}, Desc: "Commit"},
	}

	res := e.Apply(ops)
	if res.Status != "rolled_back" {
		t.Fatalf("expected rolled_back, got %s (error=%s)", res.Status, res.Error)
	}
	if res.Error != "healthcheck_failed" {
		t.Fatalf("error should be healthcheck_failed, got %s", res.Error)
	}

	// Verificar que se restauró el snapshot (uci import)
	foundImport := false
	for _, c := range fr.calls {
		if strings.Contains(c, "uci import") {
			foundImport = true
			break
		}
	}
	if !foundImport {
		t.Fatal("should have imported snapshot on rollback")
	}
}

func TestAffectedConfigsDedup(t *testing.T) {
	ops := []Op{
		{Kind: "uci_set", Args: map[string]string{"config": "dhcp", "section": "s1", "option": "o1", "value": "v1"}},
		{Kind: "uci_set", Args: map[string]string{"config": "dhcp", "section": "s2", "option": "o2", "value": "v2"}},
		{Kind: "uci_commit", Args: map[string]string{"config": "dhcp"}},
		{Kind: "service", Args: map[string]string{"name": "dnsmasq", "action": "restart"}},
	}
	cfgs := affectedConfigs(ops)
	if len(cfgs) != 1 || cfgs[0] != "dhcp" {
		t.Fatalf("expected [dhcp], got %v", cfgs)
	}
}

func TestMarshalUnmarshalOps(t *testing.T) {
	ops := []Op{
		{Kind: "uci_set", Args: map[string]string{"config": "dhcp", "section": "s", "option": "o", "value": "1.2.3.4"}, Desc: "test"},
	}
	data, err := MarshalOps(ops)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalOps(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != "uci_set" || got[0].Args["value"] != "1.2.3.4" {
		t.Fatalf("roundtrip failed: %+v", got)
	}
}

// TestValidateTcpCheck: args host/port validados por regex.
func TestValidateTcpCheck(t *testing.T) {
	cases := []struct {
		name, host, port string
		ok               bool
	}{
		{"ipv4", "127.0.0.1", "3000", true},
		{"localhost", "localhost", "53", true},
		{"lan-ip", "192.168.1.1", "80", true},
		{"port-zero", "127.0.0.1", "0", false},
		{"port-too-high", "127.0.0.1", "65536", false},
		{"port-leading-zero", "127.0.0.1", "03000", false},
		{"host-space", "127.0.0.1 ", "3000", false},
		{"host-shell-chars", "127.0.0.1; rm -rf /", "3000", false},
		{"missing-port", "127.0.0.1", "", false},
	}
	for _, c := range cases {
		op := Op{Kind: "tcp_check", Args: map[string]string{"host": c.host, "port": c.port}}
		err := Validate(op)
		if (err == nil) != c.ok {
			t.Errorf("%s: esperaba ok=%v, got err=%v", c.name, c.ok, err)
		}
	}
}

// TestTcpCheckExecOpenThenClosed: el exec de tcp_check devuelve 0 si el
// puerto está abierto y 1 si está cerrado (healthcheck real de servicio).
func TestTcpCheckExecOpenThenClosed(t *testing.T) {
	// Acortar el budget para que el caso "cerrado" no tarde 10 s en el test.
	prev := tcpCheckBudget
	tcpCheckBudget = 300 * time.Millisecond
	defer func() { tcpCheckBudget = prev }()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("no pude abrir listener: %v", err)
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)

	spec, ok := allowlist["tcp_check"]
	if !ok {
		t.Fatal("tcp_check no está en allowlist")
	}

	// Puerto abierto → 0
	if rc := spec.exec(nil, map[string]string{"host": "127.0.0.1", "port": port}); rc != 0 {
		t.Errorf("puerto abierto: esperaba 0, got %d", rc)
	}

	// Cerrar el listener → puerto cerrado → 1 (tras exhausting el budget corto)
	ln.Close()
	if rc := spec.exec(nil, map[string]string{"host": "127.0.0.1", "port": port}); rc != 1 {
		t.Errorf("puerto cerrado: esperaba 1, got %d", rc)
	}
}
