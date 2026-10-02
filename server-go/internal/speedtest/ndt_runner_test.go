package speedtest

import (
	"context"
	"testing"

	"github.com/m-lab/ndt7-client-go/spec"
)

func TestNdtServerHost(t *testing.T) {
	cases := map[string]string{
		"":                            "",
		"   ":                         "",
		"ndt-mlab1-mad04.mlab-oti.org": "ndt-mlab1-mad04.mlab-oti.org",
		"wss://ndt.example.com/ndt/v7/download": "ndt.example.com",
		"ndt.example.com:4443":                  "ndt.example.com:4443",
	}
	for in, want := range cases {
		if got := ndtServerHost(in); got != want {
			t.Errorf("ndtServerHost(%q) = %q, want %q", in, got, want)
		}
	}
}

// fakePhase alimenta un phase con medidas AppInfo/TCPInfo prefijadas.
func fakePhase(t *testing.T, ms []spec.Measurement) func(context.Context) (<-chan spec.Measurement, error) {
	return func(context.Context) (<-chan spec.Measurement, error) {
		ch := make(chan spec.Measurement, len(ms))
		for _, m := range ms {
			ch <- m
		}
		close(ch)
		return ch, nil
	}
}

func TestNdtRunPhaseRateAndRTT(t *testing.T) {
	// 100 MB en 10 s = 80 Mbps; MinRTT 12.5 ms (12500 us).
	// El campo MinRTT es promovido del LinuxTCPInfo embebido: en literal no
	// compila con go1.26, se asigna aparte.
	ti := &spec.TCPInfo{}
	ti.MinRTT = 12500
	ms := []spec.Measurement{
		{AppInfo: &spec.AppInfo{NumBytes: 50_000_000, ElapsedTime: 5e6}, TCPInfo: ti},
		{AppInfo: &spec.AppInfo{NumBytes: 100_000_000, ElapsedTime: 10e6}, TCPInfo: ti},
	}
	mbps, rtt, err := ndtRunPhase(context.Background(), fakePhase(t, ms))
	if err != nil {
		t.Fatal(err)
	}
	if mbps < 79.9 || mbps > 80.1 {
		t.Errorf("mbps = %v, want ~80", mbps)
	}
	if rtt != 12500 {
		t.Errorf("rtt = %v, want 12500", rtt)
	}
}

func TestNdtRunPhaseSinMediciones(t *testing.T) {
	_, _, err := ndtRunPhase(context.Background(), fakePhase(t, nil))
	if err == nil {
		t.Fatal("esperaba error con phase vacío")
	}
}
