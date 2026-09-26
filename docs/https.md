# HTTPS with NetPulse's own certificate authority

NetPulse can serve its web UI and its agent API over HTTPS with a certificate
signed by its **own private certificate authority (CA)**. You install that
CA's root certificate once on each device you use NetPulse from; after that
browsers trust the server without warnings. That is also what lets the PWA
be installed and Web Push work: browsers do not allow them over a
certificate you have only clicked through a warning for.

Agents pin the root rather than the server's certificate, so the certificate
can renew itself, or follow a change of the server's address, without any
agent noticing.

Nothing changes until an admin turns it on.

## Turning it on

In **Settings → HTTPS** (admins only), switch on **HTTPS on port 3443**.
Every change on that card asks for your password again.

Turning HTTPS on only **adds** the HTTPS port. Plain HTTP on the usual port
keeps working exactly as before, so turning it on cannot lock you out.

The first time, the server creates its CA in `DATA_DIR/tls/` and logs the
root's fingerprint:

```
[netpulse] TLS: root certificate SHA-256 AB:CD:…
[netpulse] TLS: agents pin 1f2e…
```

The port is `NETPULSE_TLS_PORT` (3443 by default).

## Installing the root certificate

The HTTPS card links the root as `netpulse-ca.crt` (for phones) and
`netpulse-ca.pem`. Anyone can download it, since devices need it before they
can trust the server. It lists the names it may vouch for (see below), so it
tells whoever downloads it the server's host name and local domains.

**Before trusting it, compare fingerprints.** Your device shows the
certificate's SHA-256 fingerprint when installing it. It must match both the
one on the HTTPS card and the one in the server's log. Read the log over SSH,
not over the network you are securing:

```
journalctl -u netpulse | grep 'root certificate SHA-256'
```

- **Android:** open `netpulse-ca.crt`, or install it from Settings → Security →
  Encryption & credentials → Install a certificate → CA certificate.
- **iPhone / iPad:** open `netpulse-ca.crt` and allow the profile download.
  Then install it in Settings → General → VPN & Device Management, and turn
  on full trust in Settings → General → About → Certificate Trust Settings.
- **Windows:** open `netpulse-ca.crt` → Install Certificate → Local Machine →
  "Trusted Root Certification Authorities".
- **macOS:** open `netpulse-ca.crt` in Keychain Access, add it to the System
  keychain, and set "When using this certificate" to Always Trust.
- **Linux:** copy `netpulse-ca.pem` to
  `/usr/local/share/ca-certificates/netpulse-ca.crt` and run
  `update-ca-certificates` (Debian, Ubuntu). Chrome and Chromium use this store.
- **Firefox** keeps its own store: Settings → Privacy & Security → View
  Certificates → Authorities → Import, then tick "Trust this CA to identify
  websites".

Then open `https://<server>:3443`.

### What the root can and cannot vouch for

The root carries **name constraints**. It can only vouch for:

- private addresses: 10/8, 172.16/12, 192.168/16, 100.64/10, loopback, and
  IPv6 fc00::/7;
- local names: `lan`, `home.arpa`, `internal`, `local`, the server's own
  host name, that name under each of its search domains, and anything in
  `NETPULSE_TLS_NAMES` when the CA was created. A name there covers
  everything below it, so the server logs a warning for one that is not a
  local name.

A device that enforces these constraints therefore cannot be fooled by it for
a public site, even if someone copied the CA's key - short of a public name
you added through `NETPULSE_TLS_NAMES` or `NETPULSE_PUBLIC_URL`. Chrome,
Firefox and desktop systems enforce them; check your phones before relying
on it.

The certificate the server serves covers every private address and local
name the host has. It lasts 90 days, is renewed automatically 30 days before
it expires, and is re-issued within minutes when the server's addresses
change.

**Renew now** on the HTTPS card does that check at once, instead of at the
next one within ten minutes: useful right after the server's address or name
changes. It keeps the same root, so nothing is reinstalled anywhere. The card
also lists any of the server's names or addresses the root cannot cover.

To reach the server under a name outside those constraints, the CA has to
be created again with that name in `NETPULSE_TLS_NAMES`. See "Starting over"
below.

## Moving agents to HTTPS

Settings → HTTPS lists the agents still reporting over plain HTTP. Each
moves in one of these ways:

- **Reinstall from NetPulse** (Agents → Reinstall, or the automatic
  reinstall). The agent is moved to HTTPS, pins the root, and verifies its
  downloads against it. The root reaches the router over the SSH session,
  whose host key NetPulse checks.
- **The install command** shown when adding an agent. Once HTTPS is on, it
  carries the root and the pin.
- **NetGrip:** in its NetPulse settings, set the server to
  `https://<server>:3443` and the server fingerprint to the **agent pin**
  shown on the HTTPS card.
- **Any other agent:** set `NETPULSE_SERVER=https://<server>:3443` and
  `NETPULSE_SERVER_FP=<agent pin>` in its env file.
- **Pairing with the pairing token:** an agent given only an https server URL
  and the pairing token proves the server's key with that token before
  trusting it. The token is never sent before the key is proven.
- **`deploy/switch-scraper.sh`:** fetch the root once into a file its user
  can read (`curl -fsS http://127.0.0.1:3000/netpulse-ca.pem -o …`; the
  server's own copy is private to it), then set
  `NETPULSE_URL=https://127.0.0.1:3443` and `NETPULSE_CA` to that file.

Zero-touch enrolment (`AGENT_AUTOENROLL`) hands its token to anyone who asks
over UDP, so it cannot prove anything. An agent enrolled that way trusts
whichever server answered first. Prefer the pairing token when that matters.

## Retiring plain HTTP

The same card sets what plain HTTP may still do:

| Mode | Plain HTTP |
|---|---|
| **Everything** (`full`) | serves everything, as before |
| **Agents only** (`migrate`) | page loads are redirected to HTTPS; sign-ins, sessions and API tokens are refused; agents keep reporting until each one has moved |
| **Nothing** (`redirect`) | only redirects to HTTPS and serves the root certificate; `/api/health` answers the server itself |

A stricter mode **does not take effect when you click it.** The card shows a
link to the HTTPS address. The mode only applies once you open that link,
signed in, within five minutes. That way you have proved HTTPS works for you
before plain HTTP stops taking sign-ins. If you do not confirm, nothing
changes. Going back to **Everything** is immediate. If the HTTPS page asks
you to sign in first, sign in and open the link again.

**Nothing** is refused while agents are still on plain HTTP, unless you choose
"Switch anyway". The list comes from each agent's last report in the past
day, and survives a restart of the server.

HTTPS can only be turned off from **Everything**. Browsers told to always use
HTTPS in a stricter mode need it answering, on either port, to be told to
stop. Turning it off is also refused while agents report over HTTPS, since
they would stop reporting, unless you choose "Switch anyway".

Once the CA exists, the plain port also answers HTTPS. A browser that learned
to always use HTTPS for the server's name (HSTS) still reaches it on either
port, in any mode.

## If you cannot get in

Over SSH, add to the server's env file (`/var/lib/netpulse/.env` on a
standard install) and restart:

```
NETPULSE_HTTP_MODE=full
```

Plain HTTP serves everything again, and the mode is locked until you remove
the line. Settings the environment fixes appear locked on the card.

To manage HTTPS from the environment instead of the UI, set
`NETPULSE_TLS_ENABLED=1` and `NETPULSE_TLS_CA=1`. `NETPULSE_TLS_ENABLED=0`, as
in `.env.example`, does not lock anything.

## Backups

`DATA_DIR/tls/ca-key.pem` is the key every device you installed the root on
trusts. Anything that backs up `DATA_DIR` holds it, just as it already holds
the agents' tokens. Keep such backups as private as the server itself.

If the key file or its directory can be read by other users, HTTPS does not
start: turned on from the environment, the server refuses to start at all;
turned on from the UI, the card shows the error and plain HTTP keeps
working.

## Starting over

To create a new CA, for example to cover a new name:

1. Choose **Everything**.
2. Stop the server and move `DATA_DIR/tls` away.
3. Start the server again.
4. Install the new root on your devices, and move each agent to the new pin
   (reinstalling does it).

Removing only one of `ca.pem` and `ca-key.pem` is refused: a lost half is
restored from a backup, never silently replaced.

## Other HTTPS set-ups

- **On-box** (`NETPULSE_ONBOX=1`) keeps its own HTTPS on the server's port.
- **A certificate of your own** (`NETPULSE_TLS_CERT` / `NETPULSE_TLS_KEY`)
  keeps working as before. If agents pin its key, renew it keeping the same
  key (for example `certbot --reuse-key`). Agents pinning a CA need that CA's
  certificate in the chain the server sends.
- **Behind a TLS-terminating proxy**, agents see the proxy's certificate. Set
  `NETPULSE_SERVER_FP` on them by hand; pairing cannot prove a key the server
  does not hold.
