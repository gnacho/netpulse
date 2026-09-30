package adapters

import "testing"

// #951: las líneas de log deben identificar al router por el nombre visible
// en la UI (con el slug entre paréntesis cuando difieren), no solo por el
// slug autogenerado que la web no muestra.
func TestRouterConfigLogLabel(t *testing.T) {
	tests := []struct {
		cfg  RouterConfig
		want string
	}{
		{RouterConfig{ID: "router-lan", Name: "Switch salón", Host: "192.0.2.10"}, "Switch salón (router-lan)"},
		{RouterConfig{ID: "router-lan", Name: "router-lan", Host: "192.0.2.10"}, "router-lan"},
		{RouterConfig{ID: "router-lan", Host: "192.0.2.10"}, "192.0.2.10 (router-lan)"},
		{RouterConfig{ID: "router-lan"}, "router-lan"},
	}
	for _, tt := range tests {
		if got := tt.cfg.LogLabel(); got != tt.want {
			t.Errorf("LogLabel(%+v) = %q; want %q", tt.cfg, got, tt.want)
		}
	}
}
