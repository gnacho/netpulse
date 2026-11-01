package adapters

// partialVitals contiene solo lecturas que la fuente ha confirmado. Los flags
// hasCpu/hasTemp/hasRam distinguen un cero real de un OID ausente (#1528, #1568).
type partialVitals struct {
	cpu       *int
	ram       *int
	temp      *int
	available bool
}

// resolvePartialVitals evita repetir la regla en la tarjeta y en el histórico.
func resolvePartialVitals(p *routerPolled) partialVitals {
	var out partialVitals
	if p == nil {
		return out
	}
	if p.hasCpu {
		cpu := p.cpu
		out.cpu = &cpu
	}
	if p.hasRam {
		ram := p.ram
		out.ram = &ram
	}
	if p.hasTemp {
		temp := p.temp
		out.temp = &temp
	}
	out.available = out.cpu != nil || out.ram != nil || out.temp != nil
	return out
}
