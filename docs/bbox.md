# Bouygues Telecom Bbox

NetPulse has a native, read-only Bbox adapter targeting the Bbox Pure Fibre
first. It uses the local API at `https://<bbox-address>/api/v1`.

## Configuration

In Settings → Devices, add a device of type **Bouygues Bbox**. Use the Bbox
address reachable from the NetPulse server (normally `mabbox.bytel.fr` or the
router's LAN address) and the administrator password used by the Bbox web
interface. This is not necessarily the Wi-Fi password. Enable the explicit
self-signed-certificate option when the local Bbox certificate is not trusted.

Authentication sends `POST /login` with the form fields `password` and
`remember=1`, retains the `BBOX_ID` session cookie, and sends its value again
as `X-BBOX-ID` on API requests. A 401/403 causes one re-login and request
retry. NetPulse never logs the password or cookie.

## Bbox → NetPulse field mapping

| Bbox endpoint and field | NetPulse field / view | Mapping and caveats |
| --- | --- | --- |
| `/device`: `modelname` | Router model | Used as the displayed model. |
| `/device`: `main.version` | Firmware version | Preferred over `running`, `reco`, SPL/TPL and bootloader component versions. |
| `/device`: `uptime` | Router uptime and estimated last-reboot time | Seconds; parsed as a JSON number then formatted by NetPulse as days/hours. Last reboot is estimated as poll time minus uptime. Serial number is discarded. |
| `/device/cpu`: `time.total`, `time.idle` | Router CPU percentage | Calculated from deltas between polls; first poll has no CPU delta and reports 0. |
| `/device/cpu`: `temperature.main` | Router temperature | Raw value divided by 1000, rounded to whole °C. This scale is supported by the exporter Grafana dashboard and is still worth checking against the Bbox UI. |
| `/device/mem`: `total`, `free`, `cached` | Router RAM percentage | Uses `(total - free - cached) / total`; raw counters are not exposed. |
| `/hosts`: active `hosts.list` records: `macaddress`, `ipaddress`, `hostname` | NetPulse connected-device inventory | MAC is normalized to lowercase to match the fleet's canonical client identity; inactive records are filtered when `active` is present. |
| `/hosts`: active `hosts.list[].wireless.rxUsage`, `txUsage` | Connected-device **Traffic** column | The two captures supplied for the Pure Fibre, 8 seconds apart, show active-host values changing (e.g. 13/2039 to 9/1435) independently from `wireless.rate`. They are treated as kbit/s and summed, then divided by 1000 for NetPulse's combined Mbps field. Inactive hosts are filtered. Unit is empirically inferred from those captures and still needs correlation with a controlled per-device transfer. |
| `/hosts`: `wireless.band`, `wireless.rssi0` | Client band and signal | Added only when present. `wireless.rate`/`estimatedRate` remain link-rate metadata and are not used as traffic. |
| `/wireless`: enabled radio state, `current_channel`, `current_bandwidth` | Router radio panel | Radios are identified as 2.4/5/6 GHz. Configured SSID BSSID is mapped when present. Security/passphrase objects are deliberately ignored. |
| `/wireless/{24,5,6}/stats`: `wireless.ssid.stats.rx/tx.{bytes,packets,packetserrors,packetsdiscards}` | Per-band radio counters | Cumulative RX/TX bytes, packets, errors and discards are attached to the matching radio. The detail panel currently displays byte totals; counters are not per-client or rates. |
| `/lan/ip`: `switch.ports[].id`, `state`, `link_mode` | Ethernet port panel | Ports and labels (`LAN <id>`) come from the API-reported IDs. Speed is shown only when the port is up and is derived from its API-reported negotiated `link_mode`; maximum hardware capacity is not inferred. |
| `/lan/ip`: `lan.ip.ipaddress`, `lan.ip.mac` | LAN IP and router MAC in information panel / overview | LAN IP is validated as an IP address; MAC is normalized to uppercase colon-separated form. |
| `/lan/stats`: `port[].index`, `rx/tx.bandwidth` | Per-port live RX/TX rates | Bandwidth is interpreted as kbit/s and converted to bit/s, following the exporter conversion and observed values. Per-port cumulative bytes/packets are intentionally ignored because they do not reconcile with aggregate LAN counters. |
| `/wan/ip`: `ip.address`, `ip.ip6address[].ipaddress`, `ip.dnsservers`, `ip.dnsserversv6` | WAN public IPv4, IPv6 and DNS servers | IPv4 and IPv6 are validated and displayed as plain addresses separated by a middle dot (`·`); the API port range is intentionally not displayed. DNS servers are split, validated, and deduplicated. ISP is identified as Bouygues from the configured provider type. |
| `/wan/ip`: `ip.state`, `/summary`: `internet.state` | WAN connectivity state | Not currently mapped into NetPulse's normalized WAN model, which has no explicit connection-state field. The adapter does not infer WAN-down from missing stats or IP data. |
| `/wan/ip/stats`: `rx.bandwidth`, `tx.bandwidth` | Global WAN download/upload rate | Confirmed kbit/s; multiplied by 1000 to NetPulse bit/s. If missing or invalid, uses cumulative `rx.bytes`/`tx.bytes` deltas, converted from bytes/s to bits/s. These are router-wide, not per-client. |
| `/wan/ip/stats`: `rx.contractualBandwidth`, `tx.contractualBandwidth` | Contracted WAN download/upload speeds | Interpreted as kbit/s like sibling bandwidth fields and converted to Mbps. Bbox-reported contract values take precedence over NetPulse's manually configured speeds; units should be confirmed against the router UI/ISP contract. |
| `/wan/ftth/stats`: `wan.ftth.mode`, `state` | FTTH access mode/state | Kept distinct from optical PON mode/state. |
| `/wan/sfp`: PON mode/class/status | Normalized WAN FTTH metadata | `receive_power` and `transmit_power` are scaled by 1/1000 to dBm; `temperature` and `voltage` by 1/10 to °C/V. These scales are inferred from observed Bbox responses, not specified by OpenAPI. Optical values are exposed only when the SFP is enabled and up. Serial fields are discarded. The detail-panel presentation is deferred to a follow-up. |

Missing optional endpoints/sections are tolerated. Changes in which optional
endpoints fail are recorded in the server log by endpoint path only; response
bodies and credentials are not logged. The adapter does not read `/summary`
at present. It does not claim per-client bandwidth:
the WAN rate is global, and the observed LAN per-port counters do not reconcile
with the aggregate counters. `/wan/sfp` advertised speeds are also omitted
because their units/semantics remain unclear.

## NetPulse fields supported by other integrations but not mapped from Bbox

These fields exist in NetPulse's normalized model, but the Bbox adapter does
not currently provide them:

| NetPulse capability | Current Bbox status / reason |
| --- | --- |
| WAN latency and packet loss | `/wan/diags` exposes indexed ping/DNS/HTTP diagnostic results, but entries have no target labels and include disabled/zero-try rows. They are active diagnostic aggregates, not a validated continuous WAN latency sample, so they are not mapped to NetPulse WAN latency/loss. |
| WAN protocol, interface, gateway and physical WAN port | Not populated from `/wan/ip`; no verified mapping has been added. |
| Per-Ethernet-port cumulative RX/TX bytes, packets, errors and discards | Not populated. `/lan/stats` has these fields, but the supplied port-5 byte/packet counters disagree with aggregate LAN counters and appear implausible. Instantaneous per-port bandwidth is mapped separately. |
| Port-to-device topology, FDB neighbors, LLDP, VLANs and backhaul details | Not populated. The adapter has no verified Bbox mapping for these NetPulse topology fields. |
| Per-client traffic history | Current Bbox `rxUsage`/`txUsage` rates feed the live device Traffic value; no verified per-client byte counters or historical series have been established. Do not derive per-client traffic from global WAN totals. |
| Client vendor/manufacturer, model/type, OS, IPv6, lease expiry and DHCP client ID/vendor class | Not populated. The exporter has type declarations for some `/hosts` details, but those fields/semantics have not been confirmed against a redacted Pure Fibre response and NetPulse's current host parser does not consume them. |
| Client's physical Ethernet port / negotiated speed | Not populated per device. Router port status is available, but matching a host record to a specific port has not been verified. |
| DNS query count and response timings | `/dns/stats` exposes `nbqueries`, `min`, `max`, and `avg`; exporter dashboard divides timings by 1,000, suggesting microseconds. The unit and sampling window are not API-verified, and NetPulse has no suitable router-wide DNS-stat destination today. |
| Radio transmit power, noise floor and associated-client count | Not populated; the observed `/wireless` data lacks reliable values for these metrics. |
| Router-provided WAN history | `/graphs/wan/.../day` is a candidate source, but response semantics and normalized UI mapping remain to be validated. |
| Multi-WAN/uplink details and failover | Not populated. NetPulse supports a multi-WAN model; Bbox backup/failover endpoints need investigation and a confirmed response sample. |
| Service state, VoIP/IPTV, USB and summary status | Not populated. `/summary` and other service endpoints expose data, but NetPulse has no corresponding Bbox status view/model mapping today. |
| Boot count, hardware revision, model class, detailed CPU process counts and individual firmware-component versions | Not mapped to the current router summary model. |

The exporter source is useful implementation evidence, not a substitute for
validating field units and semantics. In particular, its WAN conversion
multiplies bandwidth by 1000, while its LAN conversion does not resolve the
counter mismatch observed on this router.

## Support status

The implementation is model-agnostic where the API is shared, but the initial target is Bbox Pure Fibre. 
The client and parser are covered by mocked HTTP tests; memory, wireless and per-band counters, hosts, CPU temperature, uptime, LAN MAC and port state/speed, WAN bandwidth/contract values, and FTTH/SFP response shapes have been observed on a physical Bbox Pure Fibre.
The `/hosts` per-client traffic unit is inferred as kbit/s from two live captures, then checked against a controlled transfer. 
