package orchestr

import (
	"github.com/gnacho/netpulse/agent/executor"
)

// WrapOwnership añade el modo de ownership UCI a un plan generado por un
// módulo. Inserta al principio:
//  1. ownership_mode enforce=true.
//  2. uci_set_managed para cada sección que el plan va a tocar (deducido de
//     uci_set, uci_delete, uci_add_list, uci_delete_section). Si la sección no
//     existe en el router, uci_set_managed es un no-op; si existe, la marca
//     como gestionada por NetPulse.
//
// Las secciones creadas por uci_add o uci_set_named se marcan automáticamente
// en el executor, por lo que no es necesario incluirlas aquí.
func WrapOwnership(ops []executor.Op) []executor.Op {
	if len(ops) == 0 {
		return ops
	}
	seen := map[string]bool{}
	var managed []executor.Op
	for _, op := range ops {
		var cfg, sec string
		switch op.Kind {
		case "uci_set", "uci_delete", "uci_add_list", "uci_delete_section":
			cfg, sec = op.Args["config"], op.Args["section"]
		default:
			continue
		}
		if cfg == "" || sec == "" {
			continue
		}
		key := cfg + "." + sec
		if seen[key] {
			continue
		}
		seen[key] = true
		managed = append(managed, executor.Op{
			Kind: "uci_set_managed",
			Args: map[string]string{"config": cfg, "section": sec},
			Desc: "Mark " + key + " managed by NetPulse",
		})
	}
	if len(managed) == 0 {
		return ops
	}
	out := make([]executor.Op, 0, 1+len(managed)+len(ops))
	out = append(out, executor.Op{
		Kind: "ownership_mode",
		Args: map[string]string{"enforce": "true"},
		Desc: "Enable UCI ownership enforcement",
	})
	out = append(out, managed...)
	out = append(out, ops...)
	return out
}
