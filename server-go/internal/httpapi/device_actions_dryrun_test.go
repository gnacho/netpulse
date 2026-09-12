// device_actions_dryrun_test.go — #754: los PUT de reserva y bloqueo con
// ?dry_run=1 devuelven el plan de comandos SIN ejecutar nada en el router.
package httpapi_test

import (
	"strings"
	"testing"
)

func TestReservationDryRunPlansWithoutExecuting(t *testing.T) {
	ssh := newSSH(dhcpEmpty, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "PUT", ts.URL, "/api/devices/"+devMAC+"/reservation?dry_run=1", ts.cookie,
		`{"ip":"192.168.1.60","hostname":"tv-salon"}`)
	if res.StatusCode != 200 {
		t.Fatalf("PUT dry_run: %d (body %v)", res.StatusCode, decodeBody(t, res))
	}
	body := decodeBody(t, res)
	res.Body.Close()

	if body["dryRun"] != true {
		t.Fatalf("la respuesta debe ser dryRun: %v", body)
	}
	apply, _ := body["apply"].([]any)
	if len(apply) < 4 {
		t.Fatalf("plan apply incompleto: %v", apply)
	}
	joined := ""
	for _, c := range apply {
		if s, ok := c.(string); ok {
			joined += s + "\n"
		}
	}
	if !strings.Contains(joined, "uci set dhcp.np_host_aabbccddeeff=host") ||
		!strings.Contains(joined, "uci set dhcp.np_host_aabbccddeeff.ip='192.168.1.60'") {
		t.Fatalf("apply no contiene la reserva: %v", apply)
	}
	rb, _ := body["rollback"].([]any)
	if len(rb) == 0 || !strings.Contains(rb[0].(string), "uci delete dhcp.np_host_aabbccddeeff") {
		t.Fatalf("rollback esperado (borrar sección): %v", rb)
	}

	// NADA se ejecutó: solo la lectura uci show del sondeo de conflictos.
	for _, forbidden := range []string{"uci set", "uci commit", "uci delete", "/etc/init.d"} {
		if ssh.saw(forbidden) {
			t.Fatalf("dry-run ejecutó comandos (%q): %v", forbidden, ssh.cmdsSnapshot())
		}
	}
}

func TestBlockDryRunPlansWithoutExecuting(t *testing.T) {
	ssh := newSSH(dhcpEmpty, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "PUT", ts.URL, "/api/devices/"+devMAC+"/block?dry_run=1", ts.cookie,
		`{"router":"gateway"}`)
	if res.StatusCode != 200 {
		t.Fatalf("PUT block dry_run: %d (body %v)", res.StatusCode, decodeBody(t, res))
	}
	body := decodeBody(t, res)
	res.Body.Close()

	if body["dryRun"] != true {
		t.Fatalf("la respuesta debe ser dryRun: %v", body)
	}
	apply, _ := body["apply"].([]any)
	if len(apply) == 0 {
		t.Fatalf("plan apply vacío: %v", apply)
	}
	joined := ""
	for _, c := range apply {
		if s, ok := c.(string); ok {
			joined += s + "\n"
		}
	}
	if !strings.Contains(joined, "uci set firewall.np_block_aabbccddeeff=rule") {
		t.Fatalf("apply no contiene la regla de bloqueo: %v", apply)
	}

	for _, forbidden := range []string{"uci set", "uci commit", "uci delete", "/etc/init.d"} {
		if ssh.saw(forbidden) {
			t.Fatalf("dry-run ejecutó comandos (%q): %v", forbidden, ssh.cmdsSnapshot())
		}
	}
}
