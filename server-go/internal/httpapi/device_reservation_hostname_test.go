// device_reservation_hostname_test.go — issue #800: aplicar el nombre visible
// del dispositivo como hostname de su reserva DHCP. Con el fake SSHRunner
// (scripteable); NINGÚN router real se toca. Cubre crear/actualizar/no-op,
// dry-run, conflicto de IP, validación y rollback ante fallo de reload.
package httpapi_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// dhcpDevHostNoName: reserva existente SIN opción name (rollback = delete).
const dhcpDevHostNoName = `dhcp.np_host_aabbccddeeff=host
dhcp.np_host_aabbccddeeff.mac='aa:bb:cc:dd:ee:ff'
dhcp.np_host_aabbccddeeff.ip='192.168.1.60'
`

func hostnameBody(ip, hostname string) string {
	return fmt.Sprintf(`{"ip":%q,"hostname":%q}`, ip, hostname)
}

func TestReservationHostnameCreate(t *testing.T) {
	ssh := newSSH(dhcpEmpty, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)

	// Apply real: crea la sección host con name/mac/ip.
	res := deviceReq(t, "PUT", ts.URL, "/api/devices/"+devMAC+"/reservation-hostname", ts.cookie, hostnameBody("192.168.1.60", "tv-salon"))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", res.StatusCode)
	}
	body := decodeBody(t, res)
	if body["created"] != true {
		t.Fatalf("created: %v", body["created"])
	}
	if !ssh.saw("uci set dhcp.np_host_aabbccddeeff=host") {
		t.Fatalf("no creó la sección: %v", ssh.cmdsSnapshot())
	}
	if !ssh.saw(".name='tv-salon'") || !ssh.saw(".ip='192.168.1.60'") {
		t.Fatalf("faltan set name/ip: %v", ssh.cmdsSnapshot())
	}
	if !ssh.saw("uci commit dhcp") || !ssh.saw("/etc/init.d/dnsmasq restart") {
		t.Fatalf("falta commit/reload: %v", ssh.cmdsSnapshot())
	}
}

func TestReservationHostnameCreateDryRun(t *testing.T) {
	ssh := newSSH(dhcpEmpty, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "PUT", ts.URL, "/api/devices/"+devMAC+"/reservation-hostname?dry_run=1", ts.cookie, hostnameBody("192.168.1.60", "tv-salon"))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", res.StatusCode)
	}
	body := decodeBody(t, res)
	if body["dryRun"] != true {
		t.Fatalf("dryRun: %v", body["dryRun"])
	}
	apply, _ := body["apply"].([]any)
	if len(apply) != 4 {
		t.Fatalf("apply: %v", apply)
	}
	if ssh.saw("uci commit dhcp") {
		t.Fatalf("dry-run ejecutó comandos: %v", ssh.cmdsSnapshot())
	}
}

func TestReservationHostnameUpdate(t *testing.T) {
	ssh := newSSH(dhcpDevHost, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)

	// Dry-run: apply solo toca el name y el rollback restaura el name anterior.
	res := deviceReq(t, "PUT", ts.URL, "/api/devices/"+devMAC+"/reservation-hostname?dry_run=1", ts.cookie, hostnameBody("192.168.1.60", "movil-nacho"))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("dry-run status: %d", res.StatusCode)
	}
	plan := decodeBody(t, res)
	apply, _ := plan["apply"].([]any)
	rollback, _ := plan["rollback"].([]any)
	if len(apply) != 1 || !strings.Contains(fmt.Sprint(apply[0]), "name='movil-nacho'") {
		t.Fatalf("apply: %v", apply)
	}
	if len(rollback) != 1 || !strings.Contains(fmt.Sprint(rollback[0]), "name='tv-salon'") {
		t.Fatalf("rollback: %v", rollback)
	}

	// Apply real: actualiza el name; no recrea la sección.
	res = deviceReq(t, "PUT", ts.URL, "/api/devices/"+devMAC+"/reservation-hostname", ts.cookie, hostnameBody("192.168.1.60", "movil-nacho"))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", res.StatusCode)
	}
	body := decodeBody(t, res)
	if body["created"] != false {
		t.Fatalf("created: %v", body["created"])
	}
	if !ssh.saw("uci set dhcp.np_host_aabbccddeeff.name='movil-nacho'") {
		t.Fatalf("no actualizó el name: %v", ssh.cmdsSnapshot())
	}
	if ssh.saw("np_host_aabbccddeeff=host") {
		t.Fatalf("recreate innecesario de la sección: %v", ssh.cmdsSnapshot())
	}
}

func TestReservationHostnameUpdateNoNameRollback(t *testing.T) {
	ssh := newSSH(dhcpEmpty+dhcpDevHostNoName, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)

	// La sección no tenía name: el dry-run planea BORRAR la opción en el rollback.
	res := deviceReq(t, "PUT", ts.URL, "/api/devices/"+devMAC+"/reservation-hostname?dry_run=1", ts.cookie, hostnameBody("192.168.1.60", "tv-salon"))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", res.StatusCode)
	}
	plan := decodeBody(t, res)
	rollback, _ := plan["rollback"].([]any)
	if len(rollback) != 1 || !strings.Contains(fmt.Sprint(rollback[0]), "uci delete dhcp.np_host_aabbccddeeff.name") {
		t.Fatalf("rollback: %v", rollback)
	}
}

func TestReservationHostnameNoop(t *testing.T) {
	ssh := newSSH(dhcpDevHost, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "PUT", ts.URL, "/api/devices/"+devMAC+"/reservation-hostname", ts.cookie, hostnameBody("192.168.1.60", "tv-salon"))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", res.StatusCode)
	}
	body := decodeBody(t, res)
	if body["noop"] != true {
		t.Fatalf("noop: %v", body["noop"])
	}
	if ssh.saw("uci commit dhcp") {
		t.Fatalf("no-op ejecutó escritura: %v", ssh.cmdsSnapshot())
	}
}

func TestReservationHostnameConflict(t *testing.T) {
	ssh := newSSH(dhcpOtherHost, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "PUT", ts.URL, "/api/devices/"+devMAC+"/reservation-hostname", ts.cookie, hostnameBody("192.168.1.50", "tv-salon"))
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("status: %d", res.StatusCode)
	}
	if ssh.saw("uci commit dhcp") {
		t.Fatalf("conflicto ejecutó escritura: %v", ssh.cmdsSnapshot())
	}
}

func TestReservationHostnameApplyValidation(t *testing.T) {
	ssh := newSSH(dhcpEmpty, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)

	for _, body := range []string{
		hostnameBody("192.168.1.60", "tv salón"), // espacio: no es hostname DNS
		hostnameBody("192.168.1.60", ""),         // hostname vacío
		`{"ip":"nop","hostname":"tv-salon"}`,     // IP inválida
	} {
		res := deviceReq(t, "PUT", ts.URL, "/api/devices/"+devMAC+"/reservation-hostname", ts.cookie, body)
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("body %s: status %d", body, res.StatusCode)
		}
		res.Body.Close()
	}
	if ssh.saw("uci commit dhcp") {
		t.Fatalf("validación ejecutó escritura: %v", ssh.cmdsSnapshot())
	}
}

func TestReservationHostnameReloadFailureRollsBack(t *testing.T) {
	ssh := newSSH(dhcpEmpty, fwEmpty, sshRule{
		contains: "/etc/init.d/dnsmasq restart",
		err:      fmt.Errorf("boom"),
	})
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "PUT", ts.URL, "/api/devices/"+devMAC+"/reservation-hostname", ts.cookie, hostnameBody("192.168.1.60", "tv-salon"))
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status: %d", res.StatusCode)
	}
	res.Body.Close()
	// El rollback borra la sección creada.
	if !ssh.saw("uci delete dhcp.np_host_aabbccddeeff") {
		t.Fatalf("falta rollback: %v", ssh.cmdsSnapshot())
	}
	if !strings.Contains(strings.Join(ssh.cmdsSnapshot(), "\n"), "uci commit dhcp") {
		t.Fatalf("falta commit del rollback: %v", ssh.cmdsSnapshot())
	}
}
