/**
 * Clave SSH propia de NetPulse para sondear routers (solo lectura).
 * La app genera su par ed25519 en DATA_DIR/.ssh la primera vez y expone la
 * pública vía /api/config/sshkey para que el usuario la autorice a mano en
 * cada router (/etc/dropbear/authorized_keys).
 */
import fs from 'node:fs'
import path from 'node:path'
import { execFile } from 'node:child_process'

/**
 * Args SSH comunes: known_hosts JUNTO a la clave (dentro de DATA_DIR).
 * Imprescindible con systemd ProtectSystem=strict: el HOME del usuario del
 * servicio es de solo lectura y ssh no podría escribir ~/.ssh/known_hosts.
 */
export function sshBaseArgs(keyPath) {
  return [
    '-i', keyPath,
    '-o', 'BatchMode=yes',
    '-o', 'ConnectTimeout=4',
    '-o', 'StrictHostKeyChecking=accept-new',
    '-o', `UserKnownHostsFile=${path.join(path.dirname(keyPath), 'known_hosts')}`,
  ]
}

/** Garantiza que existe un par de claves en keyPath; devuelve la ruta. */
export async function ensureSshKeypair(keyPath) {
  if (fs.existsSync(keyPath) && fs.existsSync(`${keyPath}.pub`)) return keyPath
  fs.mkdirSync(path.dirname(keyPath), { recursive: true, mode: 0o700 })
  await new Promise((resolve, reject) => {
    execFile('ssh-keygen', ['-t', 'ed25519', '-f', keyPath, '-N', '', '-C', 'netpulse', '-q'], (err) =>
      err ? reject(err) : resolve(),
    )
  })
  try {
    fs.chmodSync(path.dirname(keyPath), 0o700)
    fs.chmodSync(keyPath, 0o600)
  } catch {}
  return keyPath
}

/** Lee la clave pública y su fingerprint (null si no existe). */
export async function getPublicKey(keyPath) {
  try {
    const publicKey = fs.readFileSync(`${keyPath}.pub`, 'utf8').trim()
    const fingerprint = await new Promise((resolve) => {
      execFile('ssh-keygen', ['-lf', `${keyPath}.pub`], (err, stdout) => {
        if (err) return resolve('')
        const m = /^\d+\s+(\S+)/.exec(stdout.trim())
        resolve(m ? m[1] : '')
      })
    })
    return { publicKey, fingerprint }
  } catch {
    return null
  }
}
