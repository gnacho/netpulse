/**
 * Adapter live para WireGuard: parsea `wg show <iface> dump` vía SSH en el
 * gateway. Formato dump (TSV):
 *   línea 1 (interfaz): private-key  public-key  listen-port  fwmark
 *   peers:              public-key  preshared-key  endpoint  allowed-ips
 *                       latest-handshake  transfer-rx  transfer-tx  persistent-keepalive
 *
 * NOTA SANDBOX: sin servidor WG real aquí; el parseo está cubierto por tests
 * unitarios con salidas de ejemplo.
 */
import { execFile } from 'node:child_process'
import { sshBaseArgs } from '../sshkey.js'

const SSH_TIMEOUT_MS = 5000

function ssh(host, keyPath, cmd) {
  return new Promise((resolve, reject) => {
    execFile(
      'ssh',
      [
        ...sshBaseArgs(keyPath),
        '-o', 'ControlMaster=auto',
        '-o', 'ControlPath=/tmp/netpulse-ssh-%r@%h:%p',
        '-o', 'ControlPersist=60',
        `root@${host}`,
        cmd,
      ],
      { timeout: SSH_TIMEOUT_MS, maxBuffer: 1024 * 1024 },
      (err, stdout, stderr) => {
        if (err) return reject(new Error(`ssh ${host}: ${err.message} ${stderr || ''}`.trim()))
        resolve(stdout)
      },
    )
  })
}

/** bytes → formato ES ("1,2 GB" / "214 MB") coherente con el contrato. */
export function fmtBytes(bytes) {
  if (bytes >= 1e9) {
    const v = bytes / 1e9
    return Number.isInteger(v) ? `${v} GB` : `${v.toFixed(1).replace('.', ',')} GB`
  }
  if (bytes >= 1e6) return `${Math.round(bytes / 1e6)} MB`
  if (bytes >= 1e3) return `${Math.round(bytes / 1e3)} KB`
  return `${bytes} B`
}

/** Epoch seg → texto relativo ES ("hace 38 s", "hace 2 días"). */
export function relTime(epochSec, nowSec = Math.floor(Date.now() / 1000)) {
  const diff = Math.max(0, nowSec - epochSec)
  if (diff < 60) return `hace ${diff} s`
  if (diff < 3600) return `hace ${Math.floor(diff / 60)} min`
  if (diff < 86400) return `hace ${Math.floor(diff / 3600)} h`
  return `hace ${Math.floor(diff / 86400)} días`
}

/**
 * Parsea la salida de `wg show <iface> dump`.
 * @returns {{ peers: Array<{pubkey, endpoint, allowedIps, handshakeSec, rxBytes, txBytes}> }}
 */
export function parseWgDump(dump) {
  const lines = dump.trim().split('\n')
  const peers = []
  for (const line of lines.slice(1)) {
    const f = line.split('\t')
    if (f.length < 8) continue
    peers.push({
      pubkey: f[0],
      endpoint: f[2] === '(none)' ? null : f[2],
      allowedIps: f[3],
      handshakeSec: parseInt(f[4], 10) || 0,
      rxBytes: parseInt(f[5], 10) || 0,
      txBytes: parseInt(f[6], 10) || 0,
    })
  }
  return { peers }
}

/**
 * Obtiene WireGuardStats del gateway.
 * @param {object} opts { host, sshKeyPath, iface, subnet, peerNames }
 *   peerNames: mapa opcional allowedIp/tunnelIp → { name, type } para etiquetar.
 */
export async function getWireGuardStats({ host, sshKeyPath, iface = 'wg0', subnet = '', peerNames = {} }) {
  const dump = await ssh(host, sshKeyPath, `wg show ${iface} dump`)
  const { peers } = parseWgDump(dump)
  const nowSec = Math.floor(Date.now() / 1000)
  const HANDSHAKE_ACTIVE_SEC = 180 // handshake < 3 min ⇒ peer activo

  return {
    interface: iface,
    subnet,
    status: peers.length >= 0 ? 'active' : 'inactive',
    peers: peers.map((p, i) => {
      const tunnelIp = (p.allowedIps.split(',')[0] || '').replace('/32', '')
      const named = peerNames[tunnelIp] || peerNames[p.pubkey] || {}
      const active = p.handshakeSec > 0 && nowSec - p.handshakeSec < HANDSHAKE_ACTIVE_SEC
      return {
        id: named.id || `peer-${i + 1}`,
        name: named.name || tunnelIp || `Peer ${p.pubkey.slice(0, 8)}`,
        type: named.type || 'desconocido',
        tunnelIp,
        active,
        lastHandshake: p.handshakeSec > 0 ? relTime(p.handshakeSec, nowSec) : 'nunca',
        rx: fmtBytes(p.rxBytes),
        tx: fmtBytes(p.txBytes),
        endpoint: p.endpoint,
        allowedIps: p.allowedIps,
      }
    }),
  }
}
