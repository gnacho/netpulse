# External router integrations

NetPulse is OpenWrt-first. The native agent belongs on an OpenWrt router when that is possible, and the server stays a small self-contained Go binary.

Other router families can integrate without adding their vendor API, authentication, parsers or polling loops to the NetPulse server. The recommended model is a separate Go agent, maintained by somebody who owns and tests the hardware.

This document is for contributors building that kind of integration.

## The model

```text
Vendor router API
       |
       | vendor-specific client and parsers
       v
<vendor>-pusher, in its own repository
       |
       | HTTPS, probe.Payload, Bearer token + HMAC
       v
NetPulse POST /api/ingest/agent
       |
       v
Normal fleet state, history and UI
```

The external agent runs on any always-on host that can reach both the router and NetPulse. It can be a small static binary managed by systemd. Docker is optional, never a requirement.

The split of responsibility is deliberate:

| External agent repository | NetPulse core |
| --- | --- |
| Vendor authentication and sessions | External agent registration and tokens |
| Vendor endpoints, retries and quirks | Signed ingestion and replay protection |
| Fixtures and parser tests | Canonical payload contract |
| Mapping to neutral NetPulse fields | Fleet identity, freshness, history and UI |
| Vendor-specific releases and compatibility | Neutral contract extensions when needed |
| Validation on the real router | Review of core changes and generic behavior |

A vendor integration is not a reason to make the NetPulse server larger for every user. It is also not a reason to build a second monitoring server. The external agent should translate into the existing ingestion contract.

## Start with an issue

Open an issue before implementing a core change or proposing a new integration for the documentation. Include:

- Router vendor, models and firmware versions available for testing.
- Public API documentation, when it exists.
- The first read-only slice and what it contributes to NetPulse.
- Which existing payload sections can carry the data.
- Which data does not fit the current contract.
- Who will maintain the external repository and validate releases.

Follow the hardware evidence rules in [CONTRIBUTING.md](../CONTRIBUTING.md). A new router family, protocol or vendor API needs real fixtures, explicit gaps and a public validation history. Do not replace missing evidence with simulated success.

## Choose the smallest first slice

Start read-only. A useful first slice normally contains only data that already fits the contract:

- System and board information through `data.system`.
- WAN state through `data.wan`.
- Associated clients and radios through `data.wireless`.
- DHCP leases through `data.dhcp`.
- FDB and Ethernet ports through `data.fdb`.
- ARP through `data.arp` and `data.arpDevs`.
- Interface counters through `data.netIf`.
- LLDP neighbors through `data.lldp`.

For example, a Bbox, MikroTik or UniFi integration can begin with system, WAN, wireless, DHCP and port data. Optical-line details, instant per-client rates or vendor-specific actions may need a later contract proposal.

Do not convert an instantaneous rate into a fake cumulative counter. Do not invent FTTH fields inside an unrelated section. If the data matters and has no neutral home, stop and propose the contract change.

## The ingestion contract

The canonical wire types live in [`agent/probe/payload.go`](../agent/probe/payload.go) and the related types in [`agent/probe/probe.go`](../agent/probe/probe.go). Read those files before defining the mapping.

The endpoint is:

```text
POST /api/ingest/agent
Content-Type: application/json
Authorization: Bearer <agent-token>
X-Agent-Signature: <lowercase-hex-hmac-sha256>
```

The signature is HMAC-SHA256 using the agent token as the key and the exact HTTP request body as the message:

```go
mac := hmac.New(sha256.New, []byte(token))
_, _ = mac.Write(body)
signature := hex.EncodeToString(mac.Sum(nil))
```

Sign the bytes after marshaling. Do not marshal again after signing. A formatting or field-order change produces a different signature.

A successful ingestion returns `202 Accepted`. Clients may also accept `200 OK` for compatibility, but must not treat 202 as an error.

A minimal envelope looks like this:

```json
{
  "router": "gateway-example",
  "ts": 1793000000,
  "version": "0.1.0",
  "kind": "external",
  "interval": 60,
  "data": {}
}
```

That envelope demonstrates the wire format only. Add real sections from `probe.PayloadData` to populate the fleet.

Contract rules:

- `router` is the registered agent slug, not a display name.
- `ts` is the sample time in Unix seconds and must be current enough for replay protection.
- `version` identifies the external agent release.
- `kind` is `external`.
- `interval` is the push cadence in seconds. Declare the real cadence so freshness handling can account for it.
- Keep payloads below the server body limit, currently 2 MiB.
- Stay below the ingestion rate limit. Do not use one HTTP request per client or per metric.
- Send one coherent snapshot per cycle. If a section fails, follow the nil and empty semantics instead of fabricating data.

The working RouterOS implementation in [`agent/cmd/routeros-pusher`](../agent/cmd/routeros-pusher/) shows configuration, token use, signing and pushing. It is a reference for the transport, not a template that requires copying the whole native agent.

## Preserve the meaning of the data

The contract carries semantics, not just JSON names.

- Use Unix seconds where the contract says seconds.
- Use bytes for cumulative byte counters and bits per second for `*Bps` rates.
- Use Celsius for temperatures and dBm for wireless signal or optical power.
- Use canonical MAC formatting expected by the field. Many payload maps and lease types use uppercase MACs.
- Keep stable identifiers across pushes. Do not use a list index as a device, port or client identity.
- Distinguish a failed probe from a successful empty result. A nil section can mean that the server should retain the last good value. An empty client map can mean that there really are no clients.
- Distinguish absent from zero. Do not report 0 °C, 0 dBm, 0 bytes of memory or a down port just because the vendor API did not expose the value.
- Keep counters cumulative when the contract expects counters. The server computes deltas where needed.
- Document vendor resets, session reconnects and counter wraparound when they affect the mapping.

If a value cannot be represented honestly, omit it and record the gap.

## Propose neutral core changes

Some integrations need a field that the current contract does not have. A good core change is useful beyond one vendor.

Good candidates include neutral optical-line information or generic per-client traffic rates. Poor candidates are fields named after one vendor response, private endpoint payloads or vendor-specific behavior inside shared polling code.

Before implementing a core extension, open or update the issue with:

1. The raw vendor fixture, redacted for credentials and personal data.
2. The neutral field and its units.
3. Whether it is a counter, an instantaneous rate, state or metadata.
4. How absence and failure are represented.
5. Which generic UI or history consumer will use it.
6. How an older external agent and an older server behave with the field.

Vendor-specific parsing remains in the external repository even when the neutral field is added to core.

## Build the external repository

A recommended repository contains:

```text
cmd/<vendor>-pusher/main.go
internal/vendorapi/       # vendor HTTP client and session handling
internal/translate/       # vendor responses to probe.Payload
internal/translate/testdata/
README.md
CHANGELOG.md
LICENSE
```

Recommended properties:

- Go with no CGO requirement, producing a static binary.
- Clear environment or flag configuration for the vendor router and NetPulse.
- No credentials in the repository, logs, fixtures or release notes.
- Small timeouts, bounded retries and one coherent push loop.
- No write operations against the vendor router in the first version.
- A documented minimum NetPulse version when a newer contract field is used.
- Releases with binaries and a changelog, maintained by the integration owner.

Name the project clearly, for example `netpulse-bbox-pusher` or `netpulse-unifi-pusher`. Do not describe a community integration as maintained by the NetPulse project unless that is actually the case.

## Run it without Docker

A systemd deployment is enough:

```ini
[Unit]
Description=NetPulse external router pusher
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=netpulse-pusher
Group=netpulse-pusher
EnvironmentFile=/etc/netpulse-vendor-pusher/env
ExecStart=/usr/local/bin/vendor-pusher
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true

[Install]
WantedBy=multi-user.target
```

Keep the environment file readable only by the service user. Run the pusher on a host that stays on. If it runs on a laptop and the laptop sleeps or shuts down, NetPulse will correctly mark the router stale.

External agents do not use the native OpenWrt self-upgrade or reinstall path. Release and upgrade procedures belong to the external repository.

## Tests and hardware validation

At minimum, include:

- Parser tests using real, redacted vendor responses.
- Mapping tests for units, stable IDs, absence, empty results and counter behavior.
- An `httptest.Server` ingestion test that verifies the Bearer token and the HMAC over the exact body.
- Tests for 202 responses, authentication failures, timeouts and malformed vendor responses.
- A documented live test against the real router and NetPulse.

For a new family or a core contract change, follow the longer public validation required by [CONTRIBUTING.md](../CONTRIBUTING.md). Record the exact agent and server versions, firmware, push interval, errors and unsupported features. Screenshots can support the result, but they do not replace parser fixtures and API evidence.

## Registration and security

Create the router as type `external` in Settings, then create or rotate its agent token through the admin API:

```text
POST /api/agents
Content-Type: application/json

{"slug":"gateway-example"}
```

This endpoint requires an admin session and returns the token. Creating or rotating a token invalidates the previous one, so update the external service configuration before expecting the next push.

Store the token like a password. Do not put it in a public repository, a ticket, a fixture or a command transcript. Test with a local or disposable NetPulse instance when possible.

## Definition of done

An external integration is ready to propose when:

- The vendor-specific code lives in the external repository.
- The first version is read-only and uses the existing ingestion contract where possible.
- Every core change is neutral, documented and covered by tests.
- Fixtures and mappings preserve units, identity and absence semantics.
- HMAC, 202 handling, retries and credential handling are tested.
- The README explains installation, systemd operation, upgrades and compatibility.
- The maintainer and the tested hardware are identified.
- The validation history lists what works, what fails and what is not implemented.

The goal is a small NetPulse core with reliable community integrations, not a collection of vendor APIs compiled into every installation.
