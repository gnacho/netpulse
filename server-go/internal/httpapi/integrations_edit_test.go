package httpapi

import (
	"strings"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/rintegration"
)

func TestSSHEnvWriteScriptRewritesFileAndRestarts(t *testing.T) {
	v, ok := rintegration.LookupVendor("asuswrt")
	if !ok {
		t.Fatal("asuswrt vendor missing")
	}
	env := map[string]string{
		"ASUSWRT_HOST":      "127.0.0.1",
		"ASUSWRT_USER":      "admin",
		"ASUSWRT_PASS":      "line1\nline2",
		"NETPULSE_INTERVAL": "45s",
		"NETPULSE_TOKEN":    "token-1",
	}
	script := sshEnvWriteScript(v, env)

	for _, want := range []string{
		`cat > "$ENV_FILE.new" <<'ENVEOF'`,
		"NETPULSE_INTERVAL=45s",
		"ASUSWRT_USER=admin",
		`chmod 600 "$ENV_FILE.new"`,
		`mv -f "$ENV_FILE.new" "$ENV_FILE"`,
		`"$INIT" start`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q\n%s", want, script)
		}
	}
	if !strings.Contains(script, `ASUSWRT_PASS=line1\nline2`) {
		t.Fatal("newlines in values must be escaped, not written raw")
	}
}

func TestSSHEnvReadScriptTargetsEnvFile(t *testing.T) {
	v, _ := rintegration.LookupVendor("asuswrt")
	if got := sshEnvReadScript(v); got != "cat /etc/"+v.Binary+".env" {
		t.Fatalf("read script = %q", got)
	}
}

func TestPusherEnvIntervalMatchesEditFormat(t *testing.T) {
	s := &server{}
	v, _ := rintegration.LookupVendor("routeros")
	env := s.pusherEnv(v, rintegration.Row{Slug: "chr-lab", Host: "10.0.0.1", Username: "u", IntervalS: 60}, "pw", "https://np", "tok")
	if env["NETPULSE_INTERVAL"] != "60s" {
		t.Fatalf("interval = %q, want 60s", env["NETPULSE_INTERVAL"])
	}
}
