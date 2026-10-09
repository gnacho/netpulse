package adapters

import "testing"

func TestGuessDeviceType(t *testing.T) {
	cases := []struct {
		hostname, manufacturer, vendorClass, clientID, lldpCaps, want string
	}{
		{"iPhone-de-Nacho", "", "", "", "", "movil"},
		{"pixel-8-pro", "", "", "", "", "movil"},
		{"MacBook-Air", "", "", "", "", "portatil"},
		{"thinkpad-x1", "", "", "", "", "portatil"},
		{"kindle-paperwhite", "", "", "", "", "tablet"},
		{"iPad-de-casa", "", "", "", "", "tablet"},
		{"shield", "", "", "", "", "tv"},
		{"lg-webos-tv", "", "", "", "", "tv"},
		{"ps5-salon", "", "", "", "", "consola"},
		{"heos5", "", "", "", "", "altavoz"},
		{"sonos-cocina", "", "", "", "", "altavoz"},
		{"marantz-av", "", "", "", "", "altavoz"},
		{"jellyfin", "", "", "", "", "servidor"},
		{"transmission", "", "", "", "", "servidor"},
		{"homeassistant", "", "", "", "", "servidor"},
		{"raspberry-pi", "", "", "", "", "servidor"}, // aditivo: raspberry sigue en servidor
		{"rpi4", "", "", "", "", "servidor"},
		{"orangepi5", "", "", "", "", "placa"},
		{"nanopi-neo", "", "", "", "", "placa"},
		{"bananapi-m2", "", "", "", "", "placa"},
		{"rockpi-4c", "", "", "", "", "placa"},
		{"pihole-lan", "", "", "", "", "servidor"}, // aditivo: pihole sigue en servidor
		{"sbc-lab", "", "", "", "", "placa"},
		{"sbcglobal-01", "", "", "", "", "desconocido"}, // "sbc" interno no casa
		{"mac-mini-salon", "", "", "", "", "ordenador"}, // "mac-" contiene "ac-"
		{"pc-sobremesa", "", "", "", "", "ordenador"},
		{"imac-estudio", "", "", "", "", "ordenador"},
		{"switch-netgear", "", "", "", "", "switch"},
		{"gs308e", "", "", "", "", "switch"},
		{"roomba960", "", "", "", "", "aspirador"},
		{"roborock-s8-max", "", "", "", "", "aspirador"},
		{"deebot-n8", "", "", "", "", "aspirador"},
		{"switchbot-k10-pro", "", "", "", "", "aspirador"},
		{"zhirui-4c0268", "", "", "", "", "iot"},
		{"cargador-coche", "", "", "", "", "iot"},
		{"slzb-06m", "", "", "", "", "iot"},
		{"camara-porche", "", "", "", "", "camara"},
		{"esp32-cam-taller", "", "", "", "", "camara"},
		{"cctv-garaje", "", "", "", "", "camara"},
		{"doorbell-entrada", "", "", "", "", "videoportero"},
		{"timbre-salon", "", "", "", "", "videoportero"},
		{"ac-salon", "", "", "", "", "iot"}, // aditivo: "ac-" sigue en iot
		{"minisplit-hab", "", "", "", "", "clima"},
		{"aire-acondicionado-dormitorio", "", "", "", "", "clima"},
		{"clima-estudio", "", "", "", "", "clima"},
		{"fujitsu-split-salon", "", "", "", "", "clima"},
		{"aireacondicionado-2", "", "", "", "", "clima"},
		{"acme-router", "", "", "", "", "desconocido"}, // "ac" interno no casa
		{"caldera-gas", "", "", "", "", "iot"},         // aditivo: "caldera" sigue en iot
		{"calefaccion-suelo", "", "", "", "", "caldera"},
		{"calefacción-central", "", "", "", "", "caldera"}, // con tilde
		{"gas-boiler", "", "", "", "", "caldera"},
		{"radiador-bano", "", "", "", "", "caldera"},
		{"water-heater", "", "", "", "", "caldera"},
		{"termo-agua", "", "", "", "", "iot"}, // termo sigue en iot
		{"sonoff-mini", "", "", "", "", "iot"},
		{"A4:CF:12:9A:01:02", "", "", "", "", "desconocido"}, // MAC como nombre
		{"", "", "", "", "", "desconocido"},
		{"", "Espressif Inc.", "", "", "", "iot"},
		{"", "Tuya Smart Inc.", "", "", "", "iot"},
		// vendor class (option 60)
		{"", "", "android-dhcp-13", "", "", "movil"},
		{"", "", "MSFT 5.0", "", "", "ordenador"},
		{"", "", "Windows", "", "", "ordenador"},
		// client-id
		{"", "", "", "raspberrypi", "", "servidor"},
		{"", "", "", "01:raspberry-pi", "", "servidor"},
		// lldp caps
		{"", "", "", "", "bridge", "switch"},
		{"", "", "", "", "bridge, router", "desconocido"},
		{"", "", "", "", "bridge, wlan", "switch"},
		{"", "", "", "", "wlan", "switch"},
		// el hostname manda sobre el resto de señales
		{"macbook-air", "", "MSFT 5.0", "", "", "portatil"},
	}
	for _, c := range cases {
		if got := GuessDeviceType(c.hostname, c.manufacturer, c.vendorClass, c.clientID, c.lldpCaps); got != c.want {
			t.Errorf("GuessDeviceType(%q, %q, %q, %q, %q) = %q, esperaba %q", c.hostname, c.manufacturer, c.vendorClass, c.clientID, c.lldpCaps, got, c.want)
		}
	}
}

// The devices that came back unclassified from a real network, and what
// each one now resolves to.
func TestGuessDeviceTypeOnTheGapsFoundLive(t *testing.T) {
	for _, tc := range []struct{ name, hostname, manufacturer, want string }{
		{"smart bulb", "bulb_1a2b3c", "WiZ", "iot"},
		{"air conditioner", "gree", "Gree Electric Appliances,Inc. of Zhuhai", "iot"},
		{"connected oven", "oven-0000000000000", "BSH Hausgeräte GmbH", "iot"},
		// Xiaomi sells phones AND appliances under the same OUI, so the
		// vendor cannot decide it; the name can.
		{"air purifier", "AirPurifierBedroom", "Beijing Xiaomi Mobile Software", "iot"},
		{"a Xiaomi that is not an appliance", "mi-9t", "Beijing Xiaomi Mobile Software", "desconocido"},
		// A recorder is a camera, not a generic gadget.
		{"recorder by name", "NVR", "", "camara"},
		{"recorder by vendor", "", "Reolink Innovation Limited", "camara"},
	} {
		if got := GuessDeviceType(tc.hostname, tc.manufacturer, "", "", ""); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
