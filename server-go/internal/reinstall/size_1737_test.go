package reinstall

import (
	"strings"
	"testing"
)

// dropbearArgvLimit es MAX_CMD_LEN de dropbear (src/sysoptions.h): un string
// del paquete SSH mayor que esto hace que el servidor aborte la conexión con
// "String too long" y el cliente solo vea EOF, sin exit status ni stderr
// (#1737, medido en un router 25.12.6: 8900 bytes pasan, 9100 no).
const dropbearArgvLimit = 9000

// El script completo de reinstalación viaja por stdin (SSHPool.RunScript), así
// que puede pasar del límite. Este test fija ese hecho: si algún día bajara de
// 9000 bytes, el camino por stdin seguiría siendo válido pero conviene saberlo,
// y si los pequeños lo superaran habría que moverlos también.
func TestScriptSizesAgainstDropbearArgvLimit(t *testing.T) {
	const (
		serverURL = "https://192.168.1.226:8443"
		fp        = "aa:bb:cc:dd:ee:ff:00:11:22:33:44:55:66:77:88:99"
		slug      = "router-ax"
	)
	token := "np_" + strings.Repeat("0123456789abcdef", 3)
	// Un root PEM real pesa ~930 bytes y es lo que empuja el script grande.
	ca := []byte(strings.Repeat("A", 932))
	trust := Trust{ServerURL: serverURL, ServerFP: fp, CAPEM: ca}

	full := ScriptWithTargets(slug, token, serverURL, fp, Digests(), WanTargets{}, trust)
	if len(full) <= dropbearArgvLimit {
		t.Fatalf("the reinstall script is %d bytes, no longer over the %d byte argv limit: is it still sent by stdin?", len(full), dropbearArgvLimit)
	}

	// Los que siguen yendo por argv deben caber con holgura.
	for _, tc := range []struct {
		name   string
		script string
	}{
		{"EnvScript", EnvScript(slug, token, serverURL, fp, WanTargets{}, trust)},
		{"TokenPushScript", TokenPushScript(slug, token)},
	} {
		if n := len(tc.script); n >= dropbearArgvLimit {
			t.Errorf("%s is %d bytes, over the %d byte argv limit: it has to go through RunScript too", tc.name, n, dropbearArgvLimit)
		}
	}
}
