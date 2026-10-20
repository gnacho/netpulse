package probe

import (
	"context"
	"encoding/json"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// uciWirelessFixture es un `uci show wireless` reducido: un wifi-device y un
// wifi-iface con 802.11r, el mínimo que el server necesita para pintar la
// vista (#1450).
const uciWirelessFixture = `wireless.radio0=wifi-device
wireless.radio0.band='2g'
wireless.radio0.channel='6'
wireless.wifi2g=wifi-iface
wireless.wifi2g.device='radio0'
wireless.wifi2g.ssid='temiscira'
wireless.wifi2g.ieee80211r='1'
wireless.wifi2g.mobility_domain='2025'
wireless.wifi2g.ft_over_ds='1'
`

// hostapdUnoOut: un AP con un cliente, suficiente para que probeWireless
// marque la sección como presente (any=true) sin depender de iwinfo.
const hostapdUnoOut = `==AP==hostapd.phy0-ap0
{
	"freq": 2437,
	"clients": {
		"AA:BB:CC:DD:EE:FF": { "auth": true, "assoc": true, "authorized": true, "signal": -60 }
	}
}`

// TestProberWirelessUciWirelessRaw: el sondeo completo rellena el campo con la
// salida cruda de `uci show wireless` y el path de eventos la reutiliza desde
// caché (sin un segundo uci), para que ningún push deje al server sin la
// sección.
func TestProberWirelessUciWirelessRaw(t *testing.T) {
	run := &countingRunner{
		fakeRunner: fakeRunner{outs: map[string]string{
			CmdHostapdClients: hostapdUnoOut,
			CmdUciWireless:    uciWirelessFixture,
		}},
		calls: map[string]int{},
	}
	p := NewProber(run, Options{})

	wd := p.probeWireless(context.Background(), true)
	if wd == nil {
		t.Fatal("wireless nil en el sondeo completo")
	}
	if wd.UciWirelessRaw != uciWirelessFixture {
		t.Fatalf("uciWirelessRaw inesperado: %q", wd.UciWirelessRaw)
	}
	if n := run.calls[CmdUciWireless]; n != 1 {
		t.Fatalf("uci show wireless ejecutado %d veces en el sondeo completo, want 1", n)
	}

	// Path de eventos: misma sección, sin volver a lanzar uci.
	wd2 := p.probeWireless(context.Background(), false)
	if wd2 == nil {
		t.Fatal("wireless nil en el path de eventos")
	}
	if wd2.UciWirelessRaw != uciWirelessFixture {
		t.Fatalf("el path de eventos perdió uciWirelessRaw: %q", wd2.UciWirelessRaw)
	}
	if n := run.calls[CmdUciWireless]; n != 1 {
		t.Fatalf("uci show wireless se repitió en el path de eventos: %d llamadas", n)
	}
}

// TestProberWirelessUciWirelessFallo: sin uci (o si el comando falla) el
// campo queda vacío y la sección wireless sigue existiendo. Es la degradación
// que el server interpreta como "802.11r no disponible", sin error.
func TestProberWirelessUciWirelessFallo(t *testing.T) {
	run := fakeRunner{outs: map[string]string{
		CmdHostapdClients: hostapdUnoOut,
	}}
	p := NewProber(run, Options{})

	wd := p.probeWireless(context.Background(), true)
	if wd == nil {
		t.Fatal("wireless nil: un fallo de uci no debe vaciar la sección")
	}
	if wd.UciWirelessRaw != "" {
		t.Fatalf("uciWirelessRaw = %q, want vacío", wd.UciWirelessRaw)
	}
}

// TestUciWirelessSecretsFilter: el volcado que sale del router lleva la config
// pero NO los secretos (PSK/SAE/RADIUS): viaja en cada push y queda guardado
// en kv agent.state.<slug>. El parser 802.11r no los necesita.
func TestUciWirelessSecretsFilter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("necesita /bin/sh")
	}
	in := strings.Join([]string{
		`wireless.wifi2g=wifi-iface`,
		`wireless.wifi2g.ssid='temiscira'`,
		`wireless.wifi2g.key='super-secreta'`,
		`wireless.wifi2g.sae_key='sae-secreta'`,
		`wireless.wifi2g.radius_secret='radius-secreto'`,
		`wireless.wifi2g.ieee80211r='1'`,
		`wireless.wifi2g.ft_psk_generate_local='1'`,
		"",
	}, "\n")
	cmd := exec.Command("/bin/sh", "-c", uciWirelessSecretsFilter)
	cmd.Stdin = strings.NewReader(in)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("filtro: %v", err)
	}
	got := string(out)
	for _, leaked := range []string{"super-secreta", "sae-secreta", "radius-secreto", "sae_key", "radius_secret"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("secreto %q en el volcado: %s", leaked, got)
		}
	}
	for _, kept := range []string{"wireless.wifi2g=wifi-iface", `ssid='temiscira'`, `ieee80211r='1'`, `ft_psk_generate_local='1'`} {
		if !strings.Contains(got, kept) {
			t.Fatalf("el filtro se llevó %q: %s", kept, got)
		}
	}
	if !strings.Contains(CmdUciWireless, uciWirelessSecretsFilter) {
		t.Fatalf("CmdUciWireless no aplica el filtro de secretos: %q", CmdUciWireless)
	}
}

// TestUciWirelessRawJSON fija el contrato con el server: la clave
// `uciWirelessRaw` viaja en el payload y se omite cuando está vacía (agentes
// antiguos y equipos sin uci no mandan la clave).
func TestUciWirelessRawJSON(t *testing.T) {
	wd := WirelessData{Clients: map[string]WirelessClient{}, UciWirelessRaw: uciWirelessFixture}
	raw, err := json.Marshal(wd)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"uciWirelessRaw":"wireless.radio0=wifi-device\n`) {
		t.Fatalf("uciWirelessRaw no serializado: %s", raw)
	}

	vacio := WirelessData{Clients: map[string]WirelessClient{}}
	raw, err = json.Marshal(vacio)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "uciWirelessRaw") {
		t.Fatalf("campo vacío debe omitirse: %s", raw)
	}
}
