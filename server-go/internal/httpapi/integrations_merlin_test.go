package httpapi

import (
	"strings"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/rintegration"
)

// merlinVendor returns the asuswrt vendor: the only ssh vendor and the only
// one with Init "merlin" (#1681).
func merlinVendor(t *testing.T) rintegration.Vendor {
	t.Helper()
	v, ok := rintegration.LookupVendor("asuswrt")
	if !ok {
		t.Fatal("asuswrt vendor missing")
	}
	return v
}

func merlinTestEnv() map[string]string {
	return map[string]string{
		"ASUSWRT_HOST":      "127.0.0.1",
		"ASUSWRT_USER":      "admin",
		"NETPULSE_SERVER":   "https://np.example",
		"NETPULSE_INTERVAL": "45s",
		"NETPULSE_TOKEN":    "token-1",
	}
}

func TestSSHInstallScriptMerlinLayout(t *testing.T) {
	v := merlinVendor(t)
	script := sshInstallScript(v, merlinTestEnv(), "https://releases.example/a.tar.gz", "abc123")

	for _, want := range []string{
		"/jffs/netpulse/asuswrt-pusher",
		"/jffs/netpulse/asuswrt-pusher.env",
		"/jffs/netpulse/asuswrt-pusher.sh",
		"/jffs/netpulse/asuswrt-pusher.pid",
		"/jffs/netpulse/asuswrt-pusher.log",
		"/jffs/scripts/services-start",
		"# >>> netpulse asuswrt-pusher >>>",
		"# <<< netpulse asuswrt-pusher <<<",
		`mv -f "$BIN.new" "$BIN"`,
		`if [ ! -f "$ENV_FILE" ]; then`,
		`chmod 600 "$ENV_FILE"`,
		"nohup",
		`"$CTRL" start`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("merlin install script missing %q\n%s", want, script)
		}
	}

	for _, banned := range []string{"procd", "/usr/sbin", "/etc/init.d", "/etc/asuswrt-pusher.env", "respawn"} {
		if strings.Contains(script, banned) {
			t.Fatalf("merlin install script must not contain %q\n%s", banned, script)
		}
	}

	// Mismo preflight y misma verificación de checksum que el camino procd.
	for _, want := range []string{"$SERVER/api/health", "exit 10", "exit 21"} {
		if !strings.Contains(script, want) {
			t.Fatalf("merlin install script missing %q\n%s", want, script)
		}
	}
}

func TestSSHInstallScriptIsDeterministic(t *testing.T) {
	v := merlinVendor(t)
	a := sshInstallScript(v, merlinTestEnv(), "https://releases.example/a.tar.gz", "abc123")
	b := sshInstallScript(v, merlinTestEnv(), "https://releases.example/a.tar.gz", "abc123")
	if a != b {
		t.Fatal("install script must be deterministic for the same input")
	}
}

func TestSSHInstallScriptMerlinBootHookIsIdempotent(t *testing.T) {
	v := merlinVendor(t)
	script := sshInstallScript(v, merlinTestEnv(), "https://releases.example/a.tar.gz", "abc123")

	for _, want := range []string{
		"mkdir -p /jffs/scripts",
		`if [ ! -f "$HOOK" ]; then`,
		`printf '#!/bin/sh\n' > "$HOOK"`,
		`grep -qF '# >>> netpulse asuswrt-pusher >>>' "$HOOK"`,
		`cat >> "$HOOK"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("merlin boot hook missing %q\n%s", want, script)
		}
	}
	// El bloque del hook llama al control script por su ruta literal, no por
	// una variable que services-start no tiene.
	if !strings.Contains(script, "/jffs/netpulse/asuswrt-pusher.sh start") {
		t.Fatalf("hook block must call the control script by literal path\n%s", script)
	}
}

func TestSSHServiceScriptMerlin(t *testing.T) {
	v := merlinVendor(t)

	if got := sshServiceScript(v, "start"); !strings.Contains(got, "/jffs/netpulse/asuswrt-pusher.sh start") {
		t.Fatalf("merlin start = %q", got)
	}
	if got := sshServiceScript(v, "stop"); !strings.Contains(got, "/jffs/netpulse/asuswrt-pusher.sh stop") {
		t.Fatalf("merlin stop = %q", got)
	}
	if got := sshServiceScript(v, "restart"); !strings.Contains(got, "/jffs/netpulse/asuswrt-pusher.sh restart") {
		t.Fatalf("merlin restart = %q", got)
	}
	// check no cambia: el handler de salud sigue con pgrep.
	if got := sshServiceScript(v, "check"); got != "pgrep -f asuswrt-pusher >/dev/null 2>&1" {
		t.Fatalf("merlin check = %q, want unchanged pgrep", got)
	}
}

func TestSSHUninstallScriptMerlin(t *testing.T) {
	v := merlinVendor(t)
	script := sshUninstallScript(v)

	for _, want := range []string{
		"/jffs/netpulse/asuswrt-pusher.sh",
		"stop",
		`sed -i '/# >>> netpulse asuswrt-pusher >>>/,/# <<< netpulse asuswrt-pusher <<</d'`,
		"rm -rf /jffs/netpulse",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("merlin uninstall missing %q\n%s", want, script)
		}
	}
	for _, banned := range []string{"/usr/sbin", "/etc/init.d"} {
		if strings.Contains(script, banned) {
			t.Fatalf("merlin uninstall must not contain %q\n%s", banned, script)
		}
	}
}

func TestSSHEnvScriptsMerlin(t *testing.T) {
	v := merlinVendor(t)

	if got := sshEnvReadScript(v); got != "cat /jffs/netpulse/asuswrt-pusher.env" {
		t.Fatalf("merlin read script = %q", got)
	}

	script := sshEnvWriteScript(v, merlinTestEnv())
	for _, want := range []string{
		"/jffs/netpulse/asuswrt-pusher.env",
		`cat > "$ENV_FILE.new" <<'ENVEOF'`,
		`chmod 600 "$ENV_FILE.new"`,
		`mv -f "$ENV_FILE.new" "$ENV_FILE"`,
		`"$CTRL" restart`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("merlin write script missing %q\n%s", want, script)
		}
	}
}

func TestSSHInstallScriptProcdUnchanged(t *testing.T) {
	v := procdVendor()
	script := sshInstallScript(v, merlinTestEnv(), "https://releases.example/a.tar.gz", "abc123")

	for _, want := range []string{
		"/usr/sbin/test-pusher",
		"/etc/init.d/test-pusher",
		"/etc/test-pusher.env",
		"USE_PROCD=1",
		"procd_set_param respawn 3600 5 5",
		`mv -f "$BIN.new" "$BIN"`,
		`"$INIT" enable`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("procd install script missing %q\n%s", want, script)
		}
	}
	if strings.Contains(script, "/jffs") {
		t.Fatalf("procd install script must not touch /jffs\n%s", script)
	}

	if got := sshServiceScript(v, "restart"); !strings.Contains(got, "/etc/init.d/test-pusher restart") {
		t.Fatalf("procd restart = %q", got)
	}
	if got := sshUninstallScript(v); !strings.Contains(got, "rm -f /etc/init.d/test-pusher /usr/sbin/test-pusher /etc/test-pusher.env") {
		t.Fatalf("procd uninstall = %q", got)
	}
}
