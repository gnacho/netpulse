package probe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxMdnsTXTRecords     = 64
	maxMdnsTXTValueLength = 4096
	maxMdnsMetadataLength = 128
)

type mdnsWireRecord struct {
	IPv4 string
	Host string
	TXT  []string
}

// UnmarshalJSON preserves repeated TXT keys emitted by umdns. Decoding into a
// Go map would silently keep only the last one.
func (r *mdnsWireRecord) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	opening, err := dec.Token()
	if err != nil {
		return err
	}
	if opening != json.Delim('{') {
		return fmt.Errorf("mDNS record is not an object")
	}
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok {
			return fmt.Errorf("mDNS record key is not a string")
		}
		if key == "txt" {
			var value any
			if err := dec.Decode(&value); err != nil {
				return err
			}
			r.TXT = appendMdnsTXT(r.TXT, value)
			continue
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		switch key {
		case "ipv4":
			if err := json.Unmarshal(raw, &r.IPv4); err != nil {
				return err
			}
		case "host":
			if err := json.Unmarshal(raw, &r.Host); err != nil {
				return err
			}
		}
	}
	_, err = dec.Token()
	return err
}

func appendMdnsTXT(dst []string, value any) []string {
	if len(dst) >= maxMdnsTXTRecords {
		return dst
	}
	switch v := value.(type) {
	case string:
		if len(v) <= maxMdnsTXTValueLength {
			return append(dst, v)
		}
	case []any:
		for _, item := range v {
			text, ok := item.(string)
			if ok && len(text) <= maxMdnsTXTValueLength && len(dst) < maxMdnsTXTRecords {
				dst = append(dst, text)
			}
		}
	}
	return dst
}

func parseMdnsBrowseRecords(raw []byte) *DiscoveryData {
	if len(raw) == 0 {
		return nil
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil || len(top) == 0 {
		return nil
	}
	dd := &DiscoveryData{
		Services:      map[string][]string{},
		HostByIP:      map[string]string{},
		Models:        map[string]string{},
		Manufacturers: map[string]string{},
	}
	for key, rawRecord := range top {
		var direct mdnsWireRecord
		if json.Unmarshal(rawRecord, &direct) == nil && (direct.IPv4 != "" || direct.Host != "" || len(direct.TXT) > 0) {
			host := direct.Host
			if host == "" {
				host = mdnsShortName(key)
			}
			dd.addMdnsRecord(host, mdnsServiceFromFQDN(key), direct)
			continue
		}

		var instances map[string]json.RawMessage
		if json.Unmarshal(rawRecord, &instances) != nil {
			continue
		}
		for instance, rawInstance := range instances {
			var record mdnsWireRecord
			if json.Unmarshal(rawInstance, &record) != nil {
				continue
			}
			host := record.Host
			if host == "" {
				host = instance
			}
			dd.addMdnsRecord(host, key, record)
		}
	}
	if len(dd.Services) == 0 && len(dd.HostByIP) == 0 && len(dd.Models) == 0 && len(dd.Manufacturers) == 0 {
		return nil
	}
	return dd
}

func mdnsShortName(value string) string {
	name := strings.TrimSpace(strings.TrimSuffix(value, "."))
	if idx := strings.Index(name, "."); idx > 0 {
		name = name[:idx]
	}
	return name
}

func mdnsServiceFromFQDN(fqdn string) string {
	parts := strings.SplitN(fqdn, ".", 3)
	if len(parts) < 2 || !strings.HasPrefix(parts[1], "_") {
		return ""
	}
	service := parts[1]
	if len(parts) >= 3 {
		service += "." + strings.SplitN(parts[2], ".", 2)[0]
	}
	return service
}

func (dd *DiscoveryData) addMdnsRecord(host, service string, record mdnsWireRecord) {
	host = mdnsShortName(host)
	if host == "" {
		return
	}
	if service != "" {
		dd.Services[host] = appendUnique(dd.Services[host], service)
	}
	if record.IPv4 != "" {
		dd.HostByIP[record.IPv4] = host
	}
	for _, entry := range record.TXT {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		value = cleanMdnsMetadata(value)
		if value == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "model":
			if dd.Models[host] == "" {
				dd.Models[host] = value
			}
		case "manufacturer":
			if dd.Manufacturers[host] == "" {
				dd.Manufacturers[host] = value
			}
		}
	}
}

func cleanMdnsMetadata(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxMdnsMetadataLength || !utf8.ValidString(value) {
		return ""
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return value
}

// MergeMdnsHosts joins host records with service metadata. A host with no SRV
// target can only inherit metadata when exactly one model candidate matches
// its name prefix. Ambiguous candidates stay unattributed.
func (dd *DiscoveryData) MergeMdnsHosts(hosts map[string]string) {
	if dd == nil {
		return
	}
	if dd.HostByIP == nil {
		dd.HostByIP = map[string]string{}
	}
	if dd.Services == nil {
		dd.Services = map[string][]string{}
	}
	if dd.Models == nil {
		dd.Models = map[string]string{}
	}
	if dd.Manufacturers == nil {
		dd.Manufacturers = map[string]string{}
	}
	for ip, rawHost := range hosts {
		host := mdnsShortName(rawHost)
		if host == "" {
			continue
		}
		dd.HostByIP[ip] = host
		if dd.Models[host] != "" || dd.Manufacturers[host] != "" {
			continue
		}
		candidate := dd.uniqueMetadataHost(host)
		if candidate != "" {
			dd.moveMdnsHost(candidate, host)
		}
	}
}

func (dd *DiscoveryData) uniqueMetadataHost(host string) string {
	normalizedHost := normalizeMdnsHost(host)
	if len(normalizedHost) < 6 {
		return ""
	}
	candidate := ""
	for key := range dd.Models {
		if key == host || !strings.HasPrefix(normalizeMdnsHost(key), normalizedHost) {
			continue
		}
		if candidate != "" {
			return ""
		}
		candidate = key
	}
	return candidate
}

func normalizeMdnsHost(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (dd *DiscoveryData) moveMdnsHost(from, to string) {
	if from == "" || from == to {
		return
	}
	for _, service := range dd.Services[from] {
		dd.Services[to] = appendUnique(dd.Services[to], service)
	}
	delete(dd.Services, from)
	if model := dd.Models[from]; model != "" {
		if dd.Models[to] == "" {
			dd.Models[to] = model
		}
		delete(dd.Models, from)
	}
	if manufacturer := dd.Manufacturers[from]; manufacturer != "" {
		if dd.Manufacturers[to] == "" {
			dd.Manufacturers[to] = manufacturer
		}
		delete(dd.Manufacturers, from)
	}
	for ip, host := range dd.HostByIP {
		if host == from {
			dd.HostByIP[ip] = to
		}
	}
}
