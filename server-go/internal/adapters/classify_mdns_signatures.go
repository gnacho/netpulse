package adapters

import "strings"

type mdnsProductSignature struct {
	id           string
	service      string
	modelPrefix  string
	manufacturer string
	deviceType   string
	fixture      string
}

var mdnsProductSignatures = []mdnsProductSignature{
	{
		id:           "marantz-airplay-speaker",
		service:      "_airplay._tcp",
		modelPrefix:  "marantz ",
		manufacturer: "Marantz",
		deviceType:   "altavoz",
		fixture:      "real",
	},
	{
		id:           "lg-airplay-tv",
		service:      "_airplay._tcp",
		modelPrefix:  "oled",
		manufacturer: "LG Electronics",
		deviceType:   "tv",
		fixture:      "real",
	},
	{
		id:           "apple-tv-airplay",
		service:      "_airplay._tcp",
		modelPrefix:  "appletv",
		manufacturer: "Apple",
		deviceType:   "tv",
		fixture:      "simulated",
	},
	{
		id:           "apple-homepod-airplay",
		service:      "_airplay._tcp",
		modelPrefix:  "audioaccessory",
		manufacturer: "Apple",
		deviceType:   "altavoz",
		fixture:      "simulated",
	},
}

// guessFromMdnsSignatures applies only product signatures backed by a real
// fixture. Simulated entries document candidates and never classify.
func guessFromMdnsSignatures(services []string, model, manufacturer string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	manufacturer = strings.TrimSpace(manufacturer)
	if model == "" {
		return ""
	}
	for _, signature := range mdnsProductSignatures {
		if signature.fixture != "real" || !ValidDeviceTypes[signature.deviceType] {
			continue
		}
		if !strings.EqualFold(manufacturer, signature.manufacturer) || !strings.HasPrefix(model, strings.ToLower(signature.modelPrefix)) {
			continue
		}
		for _, service := range services {
			if strings.EqualFold(service, signature.service) {
				return signature.deviceType
			}
		}
	}
	return ""
}
