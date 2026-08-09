package orchestr

import "testing"

// Fixture real de rt2 (Redmi AX6, OpenWrt apk) — sqm-scripts NO instalado,
// sección sqm.eth1=queue preexistente con enabled=0 (config del paquete).
const sqmOut = `===PKG_MGR===
apk
===INSTALLED===
no
===SQM_UCI===
sqm.eth1=queue
sqm.eth1.enabled='0'
sqm.eth1.interface='eth1'
sqm.eth1.download='85000'
sqm.eth1.upload='10000'
sqm.eth1.qdisc='cake'
sqm.eth1.script='piece_of_cake.qos'
sqm.eth1.qdisc_advanced='0'
sqm.eth1.ingress_ecn='ECN'
sqm.eth1.egress_ecn='ECN'
sqm.eth1.qdisc_really_really_advanced='0'
sqm.eth1.itarget='auto'
sqm.eth1.etarget='auto'
sqm.eth1.linklayer='none'
===END===`

// TestParseSqm: gestor apk, NO instalado, sección eth1 detectada con sus
// options interface/enabled.
func TestParseSqm(t *testing.T) {
	sc := parseSqm(sqmOut)
	if sc.Manager != "apk" {
		t.Errorf("Manager: got %q want apk", sc.Manager)
	}
	if sc.Installed {
		t.Error("Installed: esperaba false (sqm-scripts no instalado)")
	}
	if len(sc.Sections) != 1 {
		t.Fatalf("Sections: got %d want 1", len(sc.Sections))
	}
	sec := sc.Sections[0]
	if sec.Idx != "eth1" {
		t.Errorf("Sections[0].Idx: got %q want eth1", sec.Idx)
	}
	if sec.Interface != "eth1" {
		t.Errorf("Sections[0].Interface: got %q want eth1", sec.Interface)
	}
	if sec.Enabled {
		t.Error("Sections[0].Enabled: esperaba false (enabled='0')")
	}
}

// TestParseSqmSinConfig: sin sqm en uci → sin secciones.
func TestParseSqmSinConfig(t *testing.T) {
	out := "===PKG_MGR===\napk\n===INSTALLED===\nno\n===SQM_UCI===\n===END==="
	sc := parseSqm(out)
	if sc.Manager != "apk" {
		t.Errorf("Manager: got %q want apk", sc.Manager)
	}
	if len(sc.Sections) != 0 {
		t.Errorf("Sections: got %d want 0", len(sc.Sections))
	}
}

// TestSqmEnableConSeccionExistente: enable reusa la sección eth1, instala
// sqm-scripts (apk) y configura. Sin uci_add (sección ya existe).
func TestSqmEnableConSeccionExistente(t *testing.T) {
	sc := parseSqm(sqmOut)
	desired := SqmDesired{
		Enabled:   true,
		Interface: "eth1",
		Download:  "90000",
		Upload:    "15000",
	}
	ops := SqmOps(desired, sc)
	if len(ops) < 10 {
		t.Fatalf("ops: got %d, esperaba >= 10", len(ops))
	}
	// Primer op: apk_install sqm-scripts.
	if ops[0].Kind != "apk_install" || ops[0].Args["package"] != "sqm-scripts" {
		t.Errorf("ops[0]: got %v, esperaba apk_install sqm-scripts", ops[0])
	}
	// Sin uci_add (la sección eth1 ya existe).
	for _, op := range ops {
		if op.Kind == "uci_add" {
			t.Errorf("no esperaba uci_add: la sección eth1 ya existe")
		}
	}
	// La sección gestionada es eth1 (no @queue[-1]).
	found := false
	for _, op := range ops {
		if op.Kind == "uci_set" && op.Args["section"] == "eth1" && op.Args["option"] == "enabled" {
			found = true
			if op.Args["value"] != "1" {
				t.Errorf("enabled: got %q want 1", op.Args["value"])
			}
		}
	}
	if !found {
		t.Error("esperaba uci_set sqm.eth1.enabled=1")
	}
	if err := validateSqmOps(ops); err != nil {
		t.Fatalf("validateSqmOps: %v", err)
	}
}

// TestSqmEnableCreaSeccion: sin sección previa → uci_add + @queue[-1].
func TestSqmEnableCreaSeccion(t *testing.T) {
	out := "===PKG_MGR===\nopkg\n===INSTALLED===\nno\n===SQM_UCI===\n===END==="
	sc := parseSqm(out)
	desired := SqmDesired{Enabled: true, Interface: "wan", Download: "50000", Upload: "5000"}
	ops := SqmOps(desired, sc)
	hasAdd := false
	hasNewRef := false
	for _, op := range ops {
		if op.Kind == "uci_add" {
			hasAdd = true
		}
		if op.Kind == "uci_set" && op.Args["section"] == "@queue[-1]" && op.Args["option"] == "interface" {
			hasNewRef = true
		}
	}
	if !hasAdd {
		t.Error("esperaba uci_add (no hay sección)")
	}
	if !hasNewRef {
		t.Error("esperaba uci_set en @queue[-1] (sección recién creada)")
	}
	// opkg → install (no apk_install).
	if ops[0].Kind != "install" {
		t.Errorf("ops[0].Kind: got %q want install (opkg)", ops[0].Kind)
	}
	if err := validateSqmOps(ops); err != nil {
		t.Fatalf("validateSqmOps: %v", err)
	}
}

// TestSqmDisable: usa la sección existente, enabled=0 + stop + disable.
func TestSqmDisable(t *testing.T) {
	sc := parseSqm(sqmOut)
	desired := SqmDesired{Enabled: false, Interface: "eth1"}
	ops := SqmOps(desired, sc)
	if len(ops) != 4 {
		t.Fatalf("ops: got %d want 4 (uci_set+commit+stop+disable)", len(ops))
	}
	if ops[0].Kind != "uci_set" || ops[0].Args["option"] != "enabled" || ops[0].Args["value"] != "0" {
		t.Errorf("ops[0]: got %v, esperaba uci_set enabled=0", ops[0])
	}
	if ops[3].Kind != "service" || ops[3].Args["action"] != "disable" {
		t.Errorf("ops[3]: got %v, esperaba service sqm disable", ops[3])
	}
	if err := validateSqmOps(ops); err != nil {
		t.Fatalf("validateSqmOps: %v", err)
	}
}

// TestSqmDisableSinSeccion: sin sección → no-op (0 ops).
func TestSqmDisableSinSeccion(t *testing.T) {
	out := "===PKG_MGR===\napk\n===INSTALLED===\nno\n===SQM_UCI===\n===END==="
	sc := parseSqm(out)
	desired := SqmDesired{Enabled: false}
	ops := SqmOps(desired, sc)
	if len(ops) != 0 {
		t.Errorf("ops: got %d want 0 (no hay sección que desactivar)", len(ops))
	}
}
