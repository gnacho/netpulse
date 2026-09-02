package orchestr

import (
	"testing"

	"github.com/gnacho/netpulse/agent/executor"
)

func TestWrapOwnershipEmpty(t *testing.T) {
	got := WrapOwnership(nil)
	if len(got) != 0 {
		t.Fatalf("esperaba [] vacío, got %d", len(got))
	}
}

func TestWrapOwnershipAddsModeAndManaged(t *testing.T) {
	ops := []executor.Op{
		{Kind: "uci_set", Args: map[string]string{"config": "dhcp", "section": "@dnsmasq[0]", "option": "server", "value": "1.1.1.1"}},
		{Kind: "uci_commit", Args: map[string]string{"config": "dhcp"}},
	}
	got := WrapOwnership(ops)
	if len(got) != 4 {
		t.Fatalf("esperaba 4 ops, got %d", len(got))
	}
	if got[0].Kind != "ownership_mode" || got[0].Args["enforce"] != "true" {
		t.Errorf("op[0] debería ser ownership_mode enforce=true, got %+v", got[0])
	}
	if got[1].Kind != "uci_set_managed" {
		t.Errorf("op[1] debería ser uci_set_managed, got %+v", got[1])
	}
	if got[2].Kind != "uci_set" {
		t.Errorf("op[2] debería ser uci_set original, got %+v", got[2])
	}
}

func TestWrapOwnershipDeduplicatesSections(t *testing.T) {
	ops := []executor.Op{
		{Kind: "uci_set", Args: map[string]string{"config": "wireless", "section": "guest", "option": "ssid", "value": "A"}},
		{Kind: "uci_set", Args: map[string]string{"config": "wireless", "section": "guest", "option": "key", "value": "B"}},
		{Kind: "uci_commit", Args: map[string]string{"config": "wireless"}},
	}
	got := WrapOwnership(ops)
	managed := 0
	for _, op := range got {
		if op.Kind == "uci_set_managed" {
			managed++
		}
	}
	if managed != 1 {
		t.Fatalf("esperaba 1 uci_set_managed, got %d", managed)
	}
}

func TestWrapOwnershipSkipsNonUci(t *testing.T) {
	ops := []executor.Op{
		{Kind: "service", Args: map[string]string{"name": "network", "action": "reload"}},
	}
	got := WrapOwnership(ops)
	if len(got) != 1 || got[0].Kind != "service" {
		t.Fatalf("no debería añadir ownership a ops no-UCI: %+v", got)
	}
}

func TestWrapOwnershipCoversDeleteSection(t *testing.T) {
	ops := []executor.Op{
		{Kind: "uci_delete_section", Args: map[string]string{"config": "wireless", "section": "guest"}},
	}
	got := WrapOwnership(ops)
	if got[0].Kind != "ownership_mode" || got[1].Kind != "uci_set_managed" {
		t.Fatalf("esperaba ownership_mode + uci_set_managed + delete, got %+v", got)
	}
}
