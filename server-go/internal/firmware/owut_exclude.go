// owut_exclude.go — escritura de la lista de exclusión de ASU en el router
// (#1769). El check ya lee attendedsysupgrade.owut.remove, así que el admin ve
// lo que el router conoce. Este paso permite añadir ahí los paquetes que ASU
// reporta como missing, para que `owut download`, `owut install` y `owut
// upgrade` se comporten igual que lanzados a mano en la unidad.
//
// Solo se escribe cuando el admin lo pide: el handler es una acción explícita
// de la UI con confirmación, nunca un efecto del check ni del upgrade.
package firmware

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// owutExcludeTimeout: dos comandos uci y el commit sobre la config local del
// router. Si esto tarda, algo va mal.
const owutExcludeTimeout = 30 * time.Second

// owutPkgNameRe valida un nombre de paquete de OpenWrt antes de meterlo en una
// línea de shell: la lista llega del navegador y termina en `uci add_list`.
var owutPkgNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._+-]*$`)

// ErrOwutInvalidPkg: nombre de paquete rechazado por la validación.
var ErrOwutInvalidPkg = errors.New("firmware: nombre de paquete inválido")

// AddOwutRemovePkgs añade paquetes a attendedsysupgrade.owut.remove y hace
// commit. Devuelve la lista resultante leída del router. Los paquetes que ya
// estaban no se duplican, y sin paquetes nuevos no se toca la configuración.
func AddOwutRemovePkgs(runner Runner, host string, pkgs []string) ([]string, error) {
	if runner == nil || host == "" {
		return nil, fmt.Errorf("firmware: runner o host vacío")
	}
	clean := make([]string, 0, len(pkgs))
	seen := map[string]bool{}
	for _, p := range pkgs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !owutPkgNameRe.MatchString(p) {
			return nil, ErrOwutInvalidPkg
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		clean = append(clean, p)
	}

	current := readOwutRemoveList(runner, host)
	if len(clean) == 0 {
		return current, nil
	}
	have := make(map[string]bool, len(current))
	for _, p := range current {
		have[p] = true
	}
	var add []string
	for _, p := range clean {
		if !have[p] {
			add = append(add, p)
		}
	}
	if len(add) == 0 {
		return current, nil
	}

	var cmd strings.Builder
	for i, p := range add {
		if i > 0 {
			cmd.WriteString(" && ")
		}
		cmd.WriteString("uci -q add_list attendedsysupgrade.owut.remove=")
		cmd.WriteString(p)
	}
	cmd.WriteString(" && uci -q commit attendedsysupgrade")
	if _, err := runner.Run(host, cmd.String(), owutExcludeTimeout); err != nil {
		return current, err
	}
	return readOwutRemoveList(runner, host), nil
}
