package adapters

import "testing"

// devOverride crea un Device mínimo para tests de overrides (cable, con puerto).
func devOverride(id, mac, router, port string) Device {
	return Device{ID: id, MAC: mac, RouterID: router, Port: port, Band: "cable"}
}

// TestApplyHypervisorOverridePuertoMezclado: el caso real del usuario — un
// hipervisor (citadel-01) con CTs BC:24:11 en un puerto donde además hay
// dispositivos físicos NO relacionados. El autodiscover no los agrupa (varios
// hosts físicos); el override sí, y SOLO los CTs OUI, no los vecinos.
func TestApplyHypervisorOverridePuertoMezclado(t *testing.T) {
	devices := []Device{
		devOverride("c8-ff-bf-08-6f-ba", "c8:ff:bf:08:6f:ba", "rtr1", "lan4"),
		devOverride("bc-24-11-00-00-01", "bc:24:11:00:00:01", "rtr1", "lan4"),
		devOverride("bc-24-11-00-00-02", "bc:24:11:00:00:02", "rtr1", "lan4"),
		devOverride("marantz", "00:05:cd:00:00:01", "rtr1", "lan4"),
		devOverride("shield", "48:b0:2d:00:00:01", "rtr1", "lan4"),
	}
	overrides := []TopologyOverride{
		{Kind: "hypervisor", MAC: "c8:ff:bf:08:6f:ba", Enabled: true},
	}
	out, _ := applyTopologyOverrides(devices, nil, overrides)
	byID := map[string]Device{}
	for _, d := range out {
		byID[d.ID] = d
	}
	if byID["c8-ff-bf-08-6f-ba"].Infra != "hypervisor" {
		t.Errorf("host sin Infra=hypervisor: %+v", byID["c8-ff-bf-08-6f-ba"])
	}
	for _, ct := range []string{"bc-24-11-00-00-01", "bc-24-11-00-00-02"} {
		if byID[ct].AttachTo != "c8-ff-bf-08-6f-ba" || byID[ct].Infra != "ct" {
			t.Errorf("CT %s no anidado bajo host: %+v", ct, byID[ct])
		}
	}
	// Los físicos del mismo puerto NO se tocan
	if byID["marantz"].AttachTo != "" || byID["shield"].AttachTo != "" {
		t.Errorf("dispositivos físicos del puerto no deben anidarse: %+v %+v", byID["marantz"], byID["shield"])
	}
}

// TestApplyAttachOverride: VM con MAC random (Home Assistant) anclada a un
// hipervisor vía override kind=attach.
func TestApplyAttachOverride(t *testing.T) {
	devices := []Device{
		devOverride("c8-ff-bf-08-6f-ba", "c8:ff:bf:08:6f:ba", "rtr1", "lan4"),
		devOverride("02-78-f4-02-8a-94", "02:78:f4:02:8a:94", "rtr1", "lan4"),
	}
	overrides := []TopologyOverride{
		{Kind: "hypervisor", MAC: "c8:ff:bf:08:6f:ba", Enabled: true},
		{Kind: "attach", MAC: "02:78:f4:02:8a:94", Parent: "c8:ff:bf:08:6f:ba", Enabled: true},
	}
	out, _ := applyTopologyOverrides(devices, nil, overrides)
	byID := map[string]Device{}
	for _, d := range out {
		byID[d.ID] = d
	}
	if byID["02-78-f4-02-8a-94"].AttachTo != "c8-ff-bf-08-6f-ba" || byID["02-78-f4-02-8a-94"].Infra != "ct" {
		t.Errorf("VM attach no anclada al host: %+v", byID["02-78-f4-02-8a-94"])
	}
}

// TestApplySwitchOverride: un switch manual (sin LLDP) se convierte en nodo
// managed; el resto del puerto se anida bajo el distnode y el propio switch
// queda sellado como managed-switch.
func TestApplySwitchOverride(t *testing.T) {
	devices := []Device{
		devOverride("switch-manual", "aa:bb:cc:00:00:01", "rtr1", "lan2"),
		devOverride("d1", "00:00:00:00:00:01", "rtr1", "lan2"),
		devOverride("d2", "00:00:00:00:00:02", "rtr1", "lan2"),
	}
	overrides := []TopologyOverride{
		{Kind: "switch", MAC: "aa:bb:cc:00:00:01", Name: "switch-manual", Enabled: true},
	}
	out, dists := applyTopologyOverrides(devices, nil, overrides)
	byID := map[string]Device{}
	for _, d := range out {
		byID[d.ID] = d
	}
	if byID["switch-manual"].Infra != "managed-switch" {
		t.Errorf("switch sin Infra=managed-switch: %+v", byID["switch-manual"])
	}
	if len(dists) != 1 || dists[0].Kind != "managed" || dists[0].Mac != "aa:bb:cc:00:00:01" || dists[0].Name != "switch-manual" {
		t.Fatalf("distnode managed inesperado: %+v", dists)
	}
	id := dists[0].ID
	if byID["d1"].AttachTo != id || byID["d2"].AttachTo != id {
		t.Errorf("devices del puerto no anidados bajo el switch: %+v %+v", byID["d1"], byID["d2"])
	}
}

// TestApplyOverrideDeshabilitado: un override enabled=false no aplica.
func TestApplyOverrideDeshabilitado(t *testing.T) {
	devices := []Device{
		devOverride("c8-ff-bf-08-6f-ba", "c8:ff:bf:08:6f:ba", "rtr1", "lan4"),
		devOverride("bc-24-11-00-00-01", "bc:24:11:00:00:01", "rtr1", "lan4"),
	}
	overrides := []TopologyOverride{
		{Kind: "hypervisor", MAC: "c8:ff:bf:08:6f:ba", Enabled: false},
	}
	out, _ := applyTopologyOverrides(devices, nil, overrides)
	if out[0].Infra != "" || out[1].AttachTo != "" {
		t.Errorf("override deshabilitado no debe aplicar: %+v %+v", out[0], out[1])
	}
}

// TestApplyOverrideSinOverrides: nil/no-op preserva el resultado.
func TestApplyOverrideSinOverrides(t *testing.T) {
	devices := []Device{devOverride("a", "00:00:00:00:00:01", "rtr1", "lan1")}
	out, dists := applyTopologyOverrides(devices, nil, nil)
	if len(out) != 1 || out[0].ID != "a" || len(dists) != 0 {
		t.Errorf("sin overrides no debe cambiar nada")
	}
}

// TestApplyHypervisorNormalizaMAC: la MAC del override se normaliza antes de
// buscar (mayúsculas o espacios no rompen el match).
func TestApplyHypervisorNormalizaMAC(t *testing.T) {
	devices := []Device{
		devOverride("c8-ff-bf-08-6f-ba", "C8:FF:BF:08:6F:BA", "rtr1", "lan4"),
		devOverride("bc-24-11-00-00-01", "bc:24:11:00:00:01", "rtr1", "lan4"),
	}
	overrides := []TopologyOverride{
		{Kind: "hypervisor", MAC: " C8:FF:BF:08:6F:BA ", Enabled: true},
	}
	out, _ := applyTopologyOverrides(devices, nil, overrides)
	if out[0].Infra != "hypervisor" || out[1].AttachTo != "c8-ff-bf-08-6f-ba" {
		t.Errorf("normalización MAC falló: %+v %+v", out[0], out[1])
	}
}
