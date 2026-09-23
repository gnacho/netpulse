// mqttpub_ha.go: state payloads and Home Assistant MQTT Discovery for the
// NetPulse publisher (#825). The builders are pure so they can be tested
// without a broker.
package mqttpub

import (
	"encoding/json"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/gonzalop/mq"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
)

// statusState is the JSON published (retained) on netpulse/<instance>/status.
type statusState struct {
	Instance      string `json:"instance"`
	Version       string `json:"version"`
	HealthScore   int    `json:"health_score"`
	HealthLabel   string `json:"health_label"`
	ClientsOnline int    `json:"clients_online"`
	ClientsTotal  int    `json:"clients_total"`
	RoutersOnline int    `json:"routers_online"`
	RoutersTotal  int    `json:"routers_total"`
	UnreadAlerts  int    `json:"unread_alerts"`
	Plan          string `json:"plan,omitempty"`
	Ts            int64  `json:"ts"`
}

// routerState is the JSON published (retained) per router.
type routerState struct {
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Model   string `json:"model,omitempty"`
	Status  string `json:"status"`
	Health  int    `json:"health"`
	CPU     *int   `json:"cpu"`
	RAM     *int   `json:"ram"`
	Temp    *int   `json:"temp"`
	Clients int    `json:"clients"`
	Uptime  string `json:"uptime,omitempty"`
	Ts      int64  `json:"ts"`
}

// alertEvent is published (not retained) on netpulse/<instance>/event/alert.
type alertEvent struct {
	ID          string `json:"id"`
	Severity    string `json:"severity"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	RouterID    string `json:"router_id,omitempty"`
	Urgent      bool   `json:"urgent"`
	Ts          int64  `json:"ts"`
}

func availabilityTopic(instance string) string {
	return "netpulse/" + instance + "/availability"
}

func statusTopic(instance string) string {
	return "netpulse/" + instance + "/status"
}

func routerStateTopic(instance, slug string) string {
	return "netpulse/" + instance + "/router/" + slug + "/state"
}

func eventTopic(instance, kind string) string {
	return "netpulse/" + instance + "/event/" + kind
}

// routerSlug is the stable per-router identifier used in topics and discovery.
func routerSlug(r adapters.Router) string {
	s := sanitize(r.ID)
	if s == "default" && r.Name != "" {
		s = sanitize(r.Name)
	}
	return s
}

func buildStatus(instance, version string, ov *adapters.Overview) statusState {
	st := statusState{
		Instance:      instance,
		Version:       version,
		HealthScore:   ov.Health.Score,
		HealthLabel:   ov.Health.Label,
		ClientsOnline: ov.DeviceTotals.Online,
		ClientsTotal:  ov.DeviceTotals.Total,
		UnreadAlerts:  ov.UnreadAlerts,
		Ts:            time.Now().Unix(),
	}
	if ov.WAN.Plan != "" {
		st.Plan = ov.WAN.Plan
	}
	for _, r := range ov.Routers {
		st.RoutersTotal++
		if r.Status == "online" || r.Status == "warn" {
			st.RoutersOnline++
		}
	}
	return st
}

func buildRouterState(r adapters.Router) routerState {
	return routerState{
		Slug:    routerSlug(r),
		Name:    r.Name,
		Model:   r.Model,
		Status:  r.Status,
		Health:  r.Health,
		CPU:     r.CPU,
		RAM:     r.RAM,
		Temp:    r.Temp,
		Clients: r.Clients,
		Uptime:  r.Uptime,
		Ts:      time.Now().Unix(),
	}
}

// routerSetKey is a stable key of the current router set, used to detect when
// discovery must be republished. Incluye la marca de autoexposición (#832)
// para que un cambio en ella también republicque el discovery.
func routerSetKey(ov *adapters.Overview) string {
	slugs := make([]string, 0, len(ov.Routers))
	for _, r := range ov.Routers {
		s := routerSlug(r)
		if selfExposes(r) {
			s += "+self"
		}
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	return strings.Join(slugs, ",")
}

func (p *Publisher) publishStatus(client *mq.Client, ov *adapters.Overview) {
	payload, err := json.Marshal(buildStatus(p.cfg.Instance, p.version, ov))
	if err != nil {
		log.Printf("[mqtt] marshal status: %v", err)
		return
	}
	publish(client, statusTopic(p.cfg.Instance), payload, true)
}

func (p *Publisher) publishRouters(client *mq.Client, ov *adapters.Overview) {
	for _, r := range ov.Routers {
		topic := routerStateTopic(p.cfg.Instance, routerSlug(r))
		// #832: el router se expone solo → se limpia su estado retenido.
		if selfExposes(r) {
			publish(client, topic, []byte{}, true)
			continue
		}
		payload, err := json.Marshal(buildRouterState(r))
		if err != nil {
			log.Printf("[mqtt] marshal router %s: %v", r.ID, err)
			continue
		}
		publish(client, topic, payload, true)
	}
}

// haEntity is one Home Assistant discovery entry.
type haEntity struct {
	component string
	nodeID    string
	objectID  string
	config    map[string]any
	// remove: la entidad se publica vacía para que Home Assistant la borre
	// (#832, el router se expone solo).
	remove bool
}

// selfExposes: el router se expone él mismo a Home Assistant por MQTT (#832).
func selfExposes(r adapters.Router) bool {
	return r.SelfExpose != nil && *r.SelfExpose
}

// publishDiscovery publishes the retained discovery configs for the instance
// and every router.
func (p *Publisher) publishDiscovery(client *mq.Client) {
	ov := p.snapshot()
	if ov == nil {
		return
	}
	for _, e := range haEntities(p.cfg.Instance, p.version, ov) {
		var payload []byte
		if e.remove {
			// Vacío retenido: Home Assistant borra la entidad (#832).
			payload = []byte{}
		} else {
			var err error
			payload, err = json.Marshal(e.config)
			if err != nil {
				log.Printf("[mqtt] marshal discovery %s: %v", e.objectID, err)
				continue
			}
		}
		topic := "homeassistant/" + e.component + "/" + e.nodeID + "/" + e.objectID + "/config"
		publish(client, topic, payload, true)
	}
}

// haEntities builds the discovery entries: instance-level sensors plus one
// device per router. Pure, so it is covered by unit tests.
func haEntities(instance, version string, ov *adapters.Overview) []haEntity {
	avail := map[string]any{
		"availability_topic":    availabilityTopic(instance),
		"payload_available":     "online",
		"payload_not_available": "offline",
	}
	base := func(nodeID string, device map[string]any, component, objectID string, extra map[string]any) haEntity {
		cfg := map[string]any{
			"unique_id": nodeID + "_" + objectID,
			"object_id": nodeID + "_" + objectID,
			"device":    device,
		}
		for k, v := range avail {
			cfg[k] = v
		}
		for k, v := range extra {
			cfg[k] = v
		}
		return haEntity{component: component, nodeID: nodeID, objectID: objectID, config: cfg}
	}

	instNode := "netpulse_" + instance
	instDevice := map[string]any{
		"identifiers":  []string{instNode},
		"name":         "NetPulse " + instance,
		"manufacturer": "NetPulse",
		"sw_version":   version,
	}
	status := statusTopic(instance)
	jsonVal := func(field string) string { return "{{ value_json." + field + " }}" }

	entities := []haEntity{
		base(instNode, instDevice, "sensor", "health_score", map[string]any{
			"name": "Health score", "state_topic": status,
			"value_template": jsonVal("health_score"), "state_class": "measurement",
			"icon": "mdi:heart-pulse",
		}),
		base(instNode, instDevice, "sensor", "clients_online", map[string]any{
			"name": "Clients online", "state_topic": status,
			"value_template": jsonVal("clients_online"), "state_class": "measurement",
			"icon": "mdi:account-network",
		}),
		base(instNode, instDevice, "sensor", "routers_online", map[string]any{
			"name": "Routers online", "state_topic": status,
			"value_template": jsonVal("routers_online"), "state_class": "measurement",
			"icon": "mdi:router-network",
		}),
		base(instNode, instDevice, "sensor", "unread_alerts", map[string]any{
			"name": "Unread alerts", "state_topic": status,
			"value_template": jsonVal("unread_alerts"), "state_class": "measurement",
			"icon": "mdi:bell-alert",
		}),
		base(instNode, instDevice, "sensor", "version", map[string]any{
			"name": "NetPulse version", "state_topic": status,
			"value_template": jsonVal("version"),
			"icon":           "mdi:information-outline", "entity_category": "diagnostic",
		}),
	}

	for _, r := range ov.Routers {
		slug := routerSlug(r)
		node := instNode + "_" + slug
		name := r.Name
		if name == "" {
			name = slug
		}
		dev := map[string]any{
			"identifiers":  []string{node},
			"name":         name,
			"manufacturer": "NetPulse",
			"via_device":   instNode,
		}
		if r.Model != "" {
			dev["model"] = r.Model
		}
		state := routerStateTopic(instance, slug)

		start := len(entities)
		entities = append(entities,
			base(node, dev, "binary_sensor", "online", map[string]any{
				"name": "Online", "state_topic": state,
				"value_template": "{{ 'ON' if value_json.status in ['online', 'warn'] else 'OFF' }}",
				"device_class":   "connectivity",
				"payload_on":     "ON",
				"payload_off":    "OFF",
			}),
			base(node, dev, "sensor", "health", map[string]any{
				"name": "Health", "state_topic": state,
				"value_template": jsonVal("health"), "state_class": "measurement",
				"icon": "mdi:heart-pulse",
			}),
			base(node, dev, "sensor", "cpu_usage", map[string]any{
				"name": "CPU usage", "state_topic": state,
				"value_template": jsonVal("cpu"), "unit_of_measurement": "%",
				"state_class": "measurement", "icon": "mdi:speedometer",
				"entity_category": "diagnostic",
			}),
			base(node, dev, "sensor", "memory_usage", map[string]any{
				"name": "Memory usage", "state_topic": state,
				"value_template": jsonVal("ram"), "unit_of_measurement": "%",
				"state_class": "measurement", "icon": "mdi:memory",
				"entity_category": "diagnostic",
			}),
			base(node, dev, "sensor", "temperature", map[string]any{
				"name": "Temperature", "state_topic": state,
				"value_template": jsonVal("temp"), "unit_of_measurement": "°C",
				"device_class": "temperature", "state_class": "measurement",
			}),
			base(node, dev, "sensor", "clients", map[string]any{
				"name": "Clients", "state_topic": state,
				"value_template": jsonVal("clients"), "state_class": "measurement",
				"icon": "mdi:account-network", "entity_category": "diagnostic",
			}),
		)
		// #832: el router se expone solo → esas entidades se publican vacías
		// para que Home Assistant las borre en lugar de duplicarlas.
		if selfExposes(r) {
			for i := start; i < len(entities); i++ {
				entities[i].remove = true
			}
		}
	}
	return entities
}
