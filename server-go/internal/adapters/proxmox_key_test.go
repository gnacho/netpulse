// proxmox_key_test.go — #1879: la clave de caché de los clientes PVE tiene que
// incluir los ajustes TLS. Con solo url/token/secret, editar caPem,
// fingerprintSha256 o insecure no recreaba el cliente y el cambio no surtía
// efecto hasta reiniciar el servicio.
package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/pve"
)

func TestPveClientsKeyIncludesTLS(t *testing.T) {
	base := pve.Instance{ID: "ofi", Name: "PVE", Config: pve.Config{
		URL: "https://192.168.10.200:8006", TokenID: "netpulse@pve!netpulse", Secret: "s3cret",
	}}
	withCA := base
	withCA.CAPEM = "-----BEGIN CERTIFICATE-----\nAAA\n-----END CERTIFICATE-----"
	withFP := base
	withFP.FingerprintSHA256 = "EE:2A:D0:F1"
	withInsecure := base
	withInsecure.Insecure = true

	key := pveClientsKey([]pve.Instance{base})
	for name, inst := range map[string]pve.Instance{
		"caPem":       withCA,
		"fingerprint": withFP,
		"insecure":    withInsecure,
	} {
		if got := pveClientsKey([]pve.Instance{inst}); got == key {
			t.Errorf("%s no cambia la clave de caché: %q", name, got)
		}
	}
	// Misma config → misma clave (no se recrean clientes en cada sondeo).
	if got := pveClientsKey([]pve.Instance{base}); got != key {
		t.Errorf("clave inestable: %q != %q", got, key)
	}
}
