package probe

import "testing"

// #1757: `iw dev` declara el SSID de la interfaz AP y el cliente del station
// dump lo hereda, para poder ubicarlo despues en su VLAN. La banda y el
// signal siguen saliendo como siempre.
func TestParseIwWirelessCarriesIfaceAndSSID(t *testing.T) {
	out := "==DEV==\n" +
		"phy#0\n" +
		"\tInterface wlan0\n" +
		"\t\taddr 24:9E:7D:0A:C3:1A\n" +
		"\t\tssid IoT\n" +
		"\t\ttype AP\n" +
		"\t\tchannel 6 (2437 MHz), width: 20 MHz\n" +
		"==IF==wlan0\n" +
		"Station aa:bb:cc:dd:ee:01 (on wlan0)\n" +
		"\tsignal:  -55 dBm\n" +
		"\trx bytes:\t1234\n" +
		"\ttx bytes:\t5678\n"
	clients, _ := ParseIwWireless(out)
	c, ok := clients["AA:BB:CC:DD:EE:01"]
	if !ok {
		t.Fatalf("falta el cliente: %+v", clients)
	}
	if c.Iface != "wlan0" || c.SSID != "IoT" {
		t.Errorf("iface=%q ssid=%q, want wlan0 / IoT", c.Iface, c.SSID)
	}
	if c.Band != "2.4 GHz" || c.SignalDbm != -55 {
		t.Errorf("band=%q signal=%d, want 2.4 GHz / -55", c.Band, c.SignalDbm)
	}
}

// Una interfaz sin linea `ssid` deja el campo vacio: no se hereda el SSID de
// otra interfaz ni se inventa uno.
func TestParseIwWirelessWithoutSSIDLeavesItEmpty(t *testing.T) {
	out := "==DEV==\n" +
		"phy#0\n" +
		"\tInterface wlan1\n" +
		"\t\ttype AP\n" +
		"==IF==wlan1\n" +
		"Station aa:bb:cc:dd:ee:02 (on wlan1)\n" +
		"\tsignal:  -70 dBm\n"
	clients, _ := ParseIwWireless(out)
	c, ok := clients["AA:BB:CC:DD:EE:02"]
	if !ok {
		t.Fatalf("falta el cliente: %+v", clients)
	}
	if c.Iface != "wlan1" || c.SSID != "" {
		t.Errorf("iface=%q ssid=%q, want wlan1 / vacio", c.Iface, c.SSID)
	}
}
