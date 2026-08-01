package adapters

import "testing"

func TestGuessDeviceType(t *testing.T) {
	cases := []struct {
		hostname, manufacturer, want string
	}{
		{"iPhone-de-Nacho", "", "movil"},
		{"pixel-8-pro", "", "movil"},
		{"MacBook-Air", "", "portatil"},
		{"thinkpad-x1", "", "portatil"},
		{"kindle-paperwhite", "", "tablet"},
		{"iPad-de-casa", "", "tablet"},
		{"shield", "", "tv"},
		{"lg-webos-tv", "", "tv"},
		{"ps5-salon", "", "consola"},
		{"heos5", "", "altavoz"},
		{"sonos-cocina", "", "altavoz"},
		{"marantz-av", "", "altavoz"},
		{"jellyfin", "", "servidor"},
		{"transmission", "", "servidor"},
		{"homeassistant", "", "servidor"},
		{"raspberry-pi", "", "servidor"},
		{"pc-sobremesa", "", "ordenador"},
		{"imac-estudio", "", "ordenador"},
		{"switch-netgear", "", "switch"},
		{"gs308e", "", "switch"},
		{"roomba960", "", "iot"},
		{"zhirui-4c0268", "", "iot"},
		{"cargador-coche", "", "iot"},
		{"slzb-06m", "", "iot"},
		{"camara-porche", "", "camara"},
		{"esp32-cam-taller", "", "camara"},
		{"sonoff-mini", "", "iot"},
		{"A4:CF:12:9A:01:02", "", "desconocido"}, // MAC como nombre
		{"", "", "desconocido"},
		{"", "Espressif Inc.", "iot"},
		{"", "Tuya Smart Inc.", "iot"},
	}
	for _, c := range cases {
		if got := GuessDeviceType(c.hostname, c.manufacturer); got != c.want {
			t.Errorf("GuessDeviceType(%q, %q) = %q, esperaba %q", c.hostname, c.manufacturer, got, c.want)
		}
	}
}
