# Realtek switch beacon

[Versión en español](AGENTE-RTL-SWITCH.md)

NetPulse can receive port state, counters and the MAC forwarding table directly from a compatible Realtek switch running RTLPlayground. The switch sends UDP datagrams to NetPulse, so no Linux agent or intermediate scraper is required.

This is an advanced firmware setup. The NetPulse beacon is maintained in the [gnacho/RTLPlayground fork](https://github.com/gnacho/RTLPlayground), not in upstream RTLPlayground. Installing it replaces the switch firmware.

## What NetPulse receives

With a current beacon build, NetPulse can show:

- Port link state and negotiated speed every 30 seconds.
- Cumulative frame and byte counters, with rates calculated by the server.
- Port error counters when the firmware build includes `ie` and `oe`.
- The MAC forwarding table every 300 seconds.
- Firmware version, management MAC and uptime when included by the build.

The beacon does not provide CPU, RAM or switch temperature. It also does not carry an LLDP neighbour table. LLDP transmission and RMA handling are switch configuration features documented in the firmware guide.

A beacon-fed device cannot currently use SNMP as a second NetPulse source. Optional SNMP support in the firmware can be used independently, but it does not add another source to the same NetPulse device.

## Requirements

- A supported RTL8372 or RTL8373 web-managed switch and its exact `MACHINE` profile. Check the [supported device list](https://github.com/logicog/RTLPlayground/blob/main/doc/supported_devices.md).
- A way to back up and restore the original flash. RTLPlayground recommends a SOIC clip for recovery. A failed firmware update can leave the switch unbootable.
- Network connectivity from the switch to the NetPulse UDP listener.
- A unique agent slug. The slug is compiled into the firmware and must match the agent registered in NetPulse.

Follow the [firmware and beacon guide](https://github.com/gnacho/RTLPlayground/blob/netpulse/beacon-octets/doc/beacon.md) for the reproducible build, firmware installation, port configuration and datagram reference. The guide uses a fixed tag and distinguishes fields added in later commits.

## 1. Enable the UDP listener

Set the listener address in the NetPulse server environment and restart the server through your normal deployment process:

```sh
NETPULSE_BEACON_LISTEN=:5140
```

An empty value disables the listener. Allow inbound UDP port 5140 from the switch through any firewall between them.

## 2. Register the agent

Create an external router in NetPulse, then create its agent token through the admin API:

```text
POST /api/agents
Content-Type: application/json

{"slug":"edge-switch"}
```

The endpoint requires an admin session and returns the token. Creating or rotating a token invalidates the previous one.

Use the same `edge-switch` identifier in the two compiled slug literals in `beacon.c` before building the firmware. Do not reuse a slug from another switch.

## 3. Configure the switch

From the RTLPlayground command console, set the NetPulse address and the agent token:

```text
beacon 192.0.2.10 <agent-token>
```

Replace `192.0.2.10` with the NetPulse server address. This command starts both periodic state and FDB transmission. It takes two arguments, because the slug is already compiled into the firmware.

Useful commands:

```text
beacon                 # show status
beacon fdb             # show whether FDB transmission is enabled
beacon fdb off         # disable FDB at runtime
beacon fdb on          # enable FDB at runtime
beacon off             # stop the beacon
```

To announce the switch through LLDP without forwarding reserved LLDP frames to other ports, the firmware guide also documents:

```text
lldp on
rma 0e drop
```

Save the startup configuration with the normal RTLPlayground configuration save mechanism. Current beacon branches include the flash-save fixes. The firmware guide distinguishes those branches from older alpha builds.

## Security

The v1 beacon sends its token in clear text and does not sign UDP datagrams. Use it only on a trusted LAN or management network. Keep UDP 5140 unreachable from untrusted networks and the internet.

NetPulse validates the token hash, applies rate limiting by source IP and logs suspicious sequence changes. These controls do not encrypt the traffic.

## Verification and troubleshooting

1. Check that the switch reports the expected NetPulse address with `beacon`.
2. Confirm that the compiled slug matches the registered agent and that the token has not been rotated.
3. Check that UDP 5140 reaches the NetPulse listener.
4. Look for `[netpulse:beacon]` messages in the server log. Malformed datagrams, invalid tokens and sequence anomalies are logged there.
5. Wait for the next 30-second state datagram. FDB updates arrive every 300 seconds when enabled.

The FDB datagram has a fixed size budget. A missing MAC in one update does not prove that the client disappeared.

The RTLPlayground web server handles one connection at a time. Avoid parallel requests against its console. This does not affect UDP beacon transmission.
