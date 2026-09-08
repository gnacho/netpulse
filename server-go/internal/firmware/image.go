// image.go — resolver que determina la imagen de firmware (URL + sha256) de un
// router OpenWrt a partir de lo que reporta el propio firmware: board_name +
// release.target + release.version. Usa el índice oficial de descargas
// (downloads.openwrt.org/profiles.json), la misma fuente que LuCI attended
// sysupgrade / owut, sin depender del servicio ASU (asu.openwrt.org ya no
// resuelve; el índice es determinista y rápido).
package firmware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// DefaultImageBase es la raíz del índice de descargas de OpenWrt.
const DefaultImageBase = "https://downloads.openwrt.org"

// ResolvedImage es la imagen resuelta para un router.
type ResolvedImage struct {
	Version   string `json:"version"`
	Target    string `json:"target"`
	Board     string `json:"board"`
	ImageName string `json:"imageName"`
	URL       string `json:"url"`
	Checksum  string `json:"checksum"`
}

// ImageResolver resuelve la imagen a partir del índice de downloads.
type ImageResolver struct {
	BaseURL string
	Client  *http.Client
}

// NewImageResolver construye el resolutor con la base por defecto y un HTTP
// client con timeout.
func NewImageResolver() *ImageResolver {
	return &ImageResolver{
		BaseURL: DefaultImageBase,
		Client:  &http.Client{Timeout: 20 * time.Second},
	}
}

// ErrNoProfile se devuelve cuando el board_name no casa con ningún perfil.
var ErrNoProfile = fmt.Errorf("firmware: perfil no encontrado para el board_name")

// RestrictToTarget helper no usado por ahora; se añade si hay que filtrar.
type profilesJSON struct {
	Profiles map[string]struct {
		ImagePrefix      string `json:"image_prefix"`
		SupportedDevices []string `json:"supported_devices"`
		Images           []struct {
			Filesystem string `json:"filesystem"`
			Name       string `json:"name"`
			SHA256     string `json:"sha256"`
			Type       string `json:"type"`
		} `json:"images"`
	} `json:"profiles"`
}

// Resolve devuelve la imagen (sysupgrade preferentemente) para
// version+target+boardName consultando profiles.json del release.
func (r *ImageResolver) Resolve(ctx context.Context, version, target, boardName string) (*ResolvedImage, error) {
	if r == nil || r.Client == nil {
		return nil, fmt.Errorf("firmware: resolutor no inicializado")
	}
	if version == "" || target == "" || boardName == "" {
		return nil, fmt.Errorf("firmware: version/target/board vacíos")
	}
	// El target es una subruta ("qualcommax/ipq807x") con su slash real, no
	// un segmento escapado; version e imageName sí se escapan.
	u := fmt.Sprintf("%s/releases/%s/targets/%s/profiles.json", r.BaseURL, url.PathEscape(version), target)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("firmware: HTTP %d al obtener %s", resp.StatusCode, u)
	}
	var doc profilesJSON
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, err
	}
	// El board_name (p.ej. "redmi,ax6") casa con supported_devices de un perfil.
	var best *struct {
		ImagePrefix      string `json:"image_prefix"`
		SupportedDevices []string `json:"supported_devices"`
		Images           []struct {
			Filesystem string `json:"filesystem"`
			Name       string `json:"name"`
			SHA256     string `json:"sha256"`
			Type       string `json:"type"`
		} `json:"images"`
	}
	for _, p := range doc.Profiles {
		for _, sd := range p.SupportedDevices {
			if sd == boardName {
				b := p
				best = &b
				break
			}
		}
		if best != nil {
			break
		}
	}
	if best == nil {
		return nil, ErrNoProfile
	}
	// Preferir la imagen sysupgrade; si no hay, la primera.
	var chosenName, chosenSHA, chosenType string
	for _, im := range best.Images {
		if im.Type == "sysupgrade" {
			chosenName, chosenSHA, chosenType = im.Name, im.SHA256, im.Type
			break
		}
		if chosenName == "" {
			chosenName, chosenSHA, chosenType = im.Name, im.SHA256, im.Type
		}
	}
	if chosenName == "" {
		return nil, fmt.Errorf("firmware: perfil sin imágenes")
	}
	_ = chosenType
	imgURL := fmt.Sprintf("%s/releases/%s/targets/%s/%s", r.BaseURL, url.PathEscape(version), target, url.PathEscape(chosenName))
	return &ResolvedImage{
		Version:   version,
		Target:    target,
		Board:     boardName,
		ImageName: chosenName,
		URL:       imgURL,
		Checksum:  chosenSHA,
	}, nil
}
