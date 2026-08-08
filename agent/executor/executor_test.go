package executor

import (
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
	// Acción válida
	if err := Validate(Op{Kind: "service", Args: map[string]string{"name": "dnsmasq", "action": "restart"}}); err != nil {
		t.Fatalf("valid service: %v", err)
	}
	// Acción inválida
	if err := Validate(Op{Kind: "service", Args: map[string]string{"name": "dnsmasq", "action": "stop"}}); err == nil {
		t.Fatal("action 'stop' should be rejected (not in allowlist)")
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
