package probe

import (
	"context"
	"strings"
	"testing"
)

// #2176: en mt_wifi cada `iwinfo <if> info` contra una interfaz caída escribe
// una línea KERN_ERR en el log del router. Las tres pasadas de iwinfo saltan
// la interfaz antes de llamar al binario.
func TestIwinfoPassesSkipDownInterfaces(t *testing.T) {
	for name, cmd := range map[string]string{
		"CmdIwinfoAssoc":      CmdIwinfoAssoc,
		"CmdWirelessCombined": CmdWirelessCombined,
		"CmdRadios":           CmdRadios,
	} {
		if !strings.Contains(cmd, `cat /sys/class/net/$i/operstate`) {
			t.Fatalf("%s no consulta operstate antes de iwinfo: %s", name, cmd)
		}
		if !strings.Contains(cmd, "continue") {
			t.Fatalf("%s no salta la interfaz caída: %s", name, cmd)
		}
		// El guard tiene que ir ANTES del primer iwinfo de la interfaz.
		if strings.Index(cmd, "operstate") > strings.Index(cmd, `iwinfo "$i"`) {
			t.Fatalf("%s llama a iwinfo antes de mirar operstate", name)
		}
	}
}

// #2176: si la pasada combinada devolvió algo (aquí solo el resumen de
// radios, cero clientes), no se repite iwinfo con el par clásico. En una
// unidad sin clientes wifi propios eso duplicaba todos los ioctl por ciclo.
func TestProbeWirelessDoesNotRepeatIwinfoWhenCombinedRan(t *testing.T) {
	run := fakeRunner{outs: map[string]string{
		CmdWirelessCombined: "R|2.4|6|HT20|20|94:83:C4:00:00:09|-95|0\n",
		// Si se ejecutara el par clásico, este cliente aparecería.
		CmdIwinfoAssoc: "EC:71:DB:44:12:8A -55 2.4\n",
	}}
	p := NewProber(run, Options{})
	wd := p.probeWireless(context.Background(), true)
	if wd == nil {
		t.Fatal("sin WirelessData pese a tener radios")
	}
	if len(wd.Clients) != 0 {
		t.Fatalf("el par clásico no debía ejecutarse, clientes: %+v", wd.Clients)
	}
	if len(wd.Radios) != 1 {
		t.Fatalf("radios del combinado: %+v", wd.Radios)
	}

	// Gate negativo: si el combinado no devuelve nada, el fallback clásico
	// sigue funcionando (ese es su motivo de existir).
	run2 := fakeRunner{outs: map[string]string{
		CmdIwinfoAssoc: "EC:71:DB:44:12:8A -55 2.4\n",
	}}
	p2 := NewProber(run2, Options{})
	wd2 := p2.probeWireless(context.Background(), true)
	if wd2 == nil || len(wd2.Clients) != 1 {
		t.Fatalf("el fallback clásico debe seguir operativo: %+v", wd2)
	}
}
