#!/bin/sh
# =============================================================================
# NetPulse — one-liner installer (Linux server)
#
#   Read-only PWA dashboard for monitoring OpenWrt/GL.iNet home networks.
#   Installs the single static Go binary (frontend embedded) as a sandboxed
#   systemd service.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/gnacho/netpulse/main/install.sh | sh
#   sh install.sh --version=1.0.0 --unattended
#   sh install.sh --dry-run          # describe every step, touches nothing
#   sh install.sh --uninstall        # keeps /var/lib/netpulse (data + .env)
#   sh install.sh --uninstall --purge
#
# Requirements: Linux with systemd (Debian/Ubuntu/Fedora/Arch/...),
# amd64 / arm64 / armv7. Verifies sha256 of every download (checksums.txt).
#
# Layout (capistrano-less: single binary + state dir):
#   /usr/local/bin/netpulse      binary
#   /var/lib/netpulse            working dir: .env, data/ (SQLite), .ssh/
# =============================================================================
set -eu

APP_NAME="netpulse"
GH_REPO="gnacho/netpulse"
BIN_NAME="netpulse"
DEFAULT_PORT="3000"
INSTALL_DIR="/usr/local/bin"
STATE_DIR="/var/lib/$APP_NAME"
SERVICE_NAME="$APP_NAME"

VERSION=""; UNATTENDED=0; DRY_RUN=0; UNINSTALL=0; PURGE=0; DEMO=0

# ---------------------------------------------------------------- logging ---
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
    C_G=$(printf '\033[32m'); C_R=$(printf '\033[31m'); C_Y=$(printf '\033[33m'); C_B=$(printf '\033[1m'); C_0=$(printf '\033[0m')
else C_G=""; C_R=""; C_Y=""; C_B=""; C_0=""; fi
info()  { printf '%s--%s %s\n' "$C_B" "$C_0" "$*"; }
ok()    { printf '%s✓%s %s\n' "$C_G" "$C_0" "$*"; }
warn()  { printf '%s!%s %s\n' "$C_Y" "$C_0" "$*" >&2; }
err()   { printf '%s✗ %s%s\n' "$C_R" "$*" "$C_0" >&2; }
fatal() { _c=$1; shift; err "$*"; exit "$_c"; }
run()   { if [ "$DRY_RUN" -eq 1 ]; then info "[dry-run] $*"; else "$@"; fi; }

usage() {
    cat <<EOF
NetPulse — installer (network monitoring dashboard for OpenWrt/GL.iNet)

Usage: sh install.sh [options]
  --version=X.Y.Z   version to install (default: latest stable release)
  --demo            install in demo mode with sample data (DEMO_MODE=1)
  --unattended      no questions (automatic when there's no TTY)
  --dry-run         describe each step without touching the system
  --uninstall       remove service, binary and user (keeps $STATE_DIR)
  --purge           with --uninstall: also remove data and configuration
  -h, --help        this help

Update = re-run this script (your data and .env are preserved).
Repo: https://github.com/$GH_REPO
EOF
    exit 0
}

for arg in "$@"; do
    case "$arg" in
        --version=*)  VERSION="${arg#*=}" ;;
        --demo)       DEMO=1 ;;
        --unattended) UNATTENDED=1 ;;
        --dry-run)    DRY_RUN=1 ;;
        --uninstall)  UNINSTALL=1 ;;
        --purge)      PURGE=1 ;;
        -h|--help)    usage ;;
        *) fatal 10 "unknown option: $arg (try --help)" ;;
    esac
done
[ -t 0 ] || UNATTENDED=1   # pipe-to-shell: never prompt

# ------------------------------------------------------- interactive helpers -
tty_ok() { (exec 3<>/dev/tty) 2>/dev/null; }

# ask_yes_no "question" [default: 0=no, 1=yes] — prompts on /dev/tty; returns 0/1.
# Non-interactive (--unattended or no TTY): returns the default silently.
ask_yes_no() {
    _q=$1; _def=${2:-0}
    [ "$UNATTENDED" -eq 1 ] && return "$_def"
    tty_ok || return "$_def"
    if [ "$_def" -eq 1 ]; then _hint="Y/n"; else _hint="y/N"; fi
    while :; do
        printf '%s [%s] ' "$_q" "$_hint" > /dev/tty
        IFS= read -r _r < /dev/tty || _r=""
        case "$(printf '%s' "$_r" | tr '[:upper:]' '[:lower:]')" in
            "") return "$_def" ;;
            y|yes|s|si) return 0 ;;
            n|no) return 1 ;;
            *) printf 'Please answer y or n.\n' > /dev/tty ;;
        esac
    done
}

# -------------------------------------------------------------- elevation ---
if [ "$(id -u)" -eq 0 ]; then SUDO=""
elif command -v sudo >/dev/null 2>&1; then SUDO="sudo -E"
elif command -v doas >/dev/null 2>&1; then SUDO="doas"
else fatal 22 "I need root (or sudo/doas). Download the script and run it as root: su -c 'sh install.sh'"
fi

# --------------------------------------------------------------- uninstall --
if [ "$UNINSTALL" -eq 1 ]; then
    info "uninstalling $APP_NAME"
    if [ -f "/etc/systemd/system/$SERVICE_NAME.service" ]; then
        run $SUDO systemctl stop "$SERVICE_NAME" 2>/dev/null || true
        run $SUDO systemctl disable "$SERVICE_NAME" 2>/dev/null || true
        run $SUDO rm -f "/etc/systemd/system/$SERVICE_NAME.service"
        # Units de reinicio bajo demanda (issue #4): si existen, quitarlas
        if [ -f "/etc/systemd/system/$SERVICE_NAME-restart.path" ]; then
            run $SUDO systemctl disable --now "$SERVICE_NAME-restart.path" 2>/dev/null || true
            run $SUDO rm -f "/etc/systemd/system/$SERVICE_NAME-restart.path" \
                "/etc/systemd/system/$SERVICE_NAME-restart.service"
        fi
        run $SUDO systemctl daemon-reload
        ok "systemd unit removed"
    fi
    run $SUDO rm -f "$INSTALL_DIR/$BIN_NAME" "$INSTALL_DIR/$BIN_NAME.bak"
    ok "binary removed from $INSTALL_DIR"
    if id "$APP_NAME" >/dev/null 2>&1; then
        run $SUDO userdel "$APP_NAME" 2>/dev/null || warn "could not delete user $APP_NAME"
        ok "system user removed"
    fi
    if [ "$PURGE" -eq 1 ]; then
        run $SUDO rm -rf "$STATE_DIR"
        ok "data and configuration removed (--purge)"
    else
        info "data kept in $STATE_DIR (remove with --purge)"
    fi
    ok "$APP_NAME uninstalled"
    exit 0
fi

# --------------------------------------------------------------- detection --
# /etc/os-release define su propia VERSION ("13 (trixie)" en Debian) y pisaría
# la variable VERSION del script; guardar y restaurar alrededor del source.
_saved_version="$VERSION"
. /etc/os-release 2>/dev/null || true
VERSION="$_saved_version"
unset _saved_version
OS_PRETTY="${PRETTY_NAME:-$(uname -s)}"

ARCH=$(uname -m)
case "$ARCH" in
    x86_64|amd64)   GOARCH=amd64 ;;
    aarch64|arm64)  GOARCH=arm64 ;;
    armv7l|armv7)   GOARCH=armv7 ;;
    *) fatal 20 "unsupported architecture: $ARCH (released: amd64, arm64, armv7)"
esac

if [ ! -d /run/systemd/system ] || ! command -v systemctl >/dev/null 2>&1; then
    fatal 23 "NetPulse needs systemd (this machine doesn't run it). See https://github.com/$GH_REPO for manual setup"
fi
info "detected: $OS_PRETTY · linux/$GOARCH · systemd"

if command -v curl >/dev/null 2>&1; then FETCH="curl -fsSL --retry 3 --connect-timeout 10"
elif command -v wget >/dev/null 2>&1; then FETCH="wget -q -O-"
else fatal 21 "I need curl or wget (install it with your package manager)"
fi
fetch_to() { $FETCH "$1" > "$2"; }
command -v sha256sum >/dev/null 2>&1 || fatal 21 "missing sha256sum (coreutils package)"

# ------------------------------------------------- disk / memory pre-flight --
AVAIL_MB=$(df -Pm / 2>/dev/null | awk 'NR==2 {print $4}' || true)
if [ -n "${AVAIL_MB:-}" ]; then
    [ "$AVAIL_MB" -lt 150 ] && fatal 24 "not enough disk space: ${AVAIL_MB} MB free (minimum 150 MB)"
    [ "$AVAIL_MB" -lt 300 ] && warn "low disk space: ${AVAIL_MB} MB free (recommended: 300+ MB)"
    ok "disk space: ${AVAIL_MB} MB free"
fi
MEM_MB=$(awk '/^MemAvailable:/ {print int($2/1024)}' /proc/meminfo 2>/dev/null || true)
if [ -n "${MEM_MB:-}" ] && [ "$MEM_MB" -lt 128 ]; then
    warn "low memory: ${MEM_MB} MB available — NetPulse needs very little, but 128+ MB is recommended"
fi

# ------------------------------------------------------ port pre-flight -----
port_in_use() {
    if command -v ss >/dev/null 2>&1; then
        ss -tln 2>/dev/null | awk '{print $4}' | grep -qE "[:.]${1}\$"
    elif command -v netstat >/dev/null 2>&1; then
        netstat -tln 2>/dev/null | awk '{print $4}' | grep -qE "[:.]${1}\$"
    else
        return 1  # can't check: assume free
    fi
}
pick_port() {
    _p=$1; _end=$((_p + 20))
    while [ "$_p" -le "$_end" ]; do
        if ! port_in_use "$_p"; then printf '%s' "$_p"; return 0; fi
        _p=$((_p + 1))
    done
    return 1
}

# choose_port WANT — interactive (TTY): asks which port to use, suggesting the
# next free one and rejecting busy/invalid answers. Non-interactive: prints the
# next free port. Prints the chosen port on stdout; fails if none is free.
choose_port() {
    _want=$1
    _next=$(pick_port "$((_want + 1))") || _next=""
    if [ "$UNATTENDED" -eq 0 ] && tty_ok; then
        while :; do
            printf 'Port %s is already in use.\nWhich port should %s listen on? [%s] ' \
                "$_want" "$APP_NAME" "${_next:-none free}" > /dev/tty
            IFS= read -r _r < /dev/tty || _r=""
            _r="${_r:-$_next}"
            case "$_r" in
                ''|*[!0-9]*) printf 'Please enter a port number.\n' > /dev/tty; continue ;;
            esac
            if [ "$_r" -lt 1 ] || [ "$_r" -gt 65535 ]; then
                printf 'Out of range (1-65535).\n' > /dev/tty; continue
            fi
            if port_in_use "$_r"; then
                printf 'Port %s is also in use.\n' "$_r" > /dev/tty; continue
            fi
            printf '%s' "$_r"; return 0
        done
    fi
    [ -n "$_next" ] || return 1
    printf '%s' "$_next"
}

# Fresh install only: an upgrade keeps the port from the existing .env file.
PORT="$DEFAULT_PORT"
if [ ! -f "$STATE_DIR/.env" ]; then
    if port_in_use "$DEFAULT_PORT"; then
        PORT=$(choose_port "$DEFAULT_PORT") \
            || fatal 25 "port $DEFAULT_PORT is busy and no free port found in $((DEFAULT_PORT + 1))-$((DEFAULT_PORT + 21)) — set one manually in $STATE_DIR/.env after install"
        warn "port $DEFAULT_PORT is already in use — NetPulse will listen on $PORT instead"
    else
        ok "port $DEFAULT_PORT is free"
    fi
fi

# Demo mode (issue #4): solo con --demo explícito. El default es BD limpia;
# la demo se activa después desde Ajustes (el botón llama a
# POST /api/demo/enable y reinicia el servicio vía .restart-me).

# --------------------------------------------------------- resolve version --
if [ -z "$VERSION" ]; then
    info "resolving latest stable version"
    VERSION=$($FETCH "https://api.github.com/repos/$GH_REPO/releases/latest" \
        | grep '"tag_name"' | head -1 | cut -d'"' -f4) \
        || fatal 31 "could not resolve the latest version (GitHub rate-limit?). Use --version=X.Y.Z"
    [ -n "$VERSION" ] || fatal 31 "no stable release found yet. Use --version=X.Y.Z"
fi
VERSION_NORM=$(echo "$VERSION" | sed 's/^v//')
ASSET="${BIN_NAME}_${VERSION_NORM}_linux_${GOARCH}.tar.gz"
BASE_URL="https://github.com/$GH_REPO/releases/download/$VERSION"
info "version: $VERSION"

# connectivity pre-flight BEFORE touching the system
$FETCH "https://github.com/$GH_REPO" >/dev/null \
    || fatal 30 "no access to github.com (proxy? DNS? firewall?)"

UPGRADING=0
if [ -x "$INSTALL_DIR/$BIN_NAME" ]; then
    UPGRADING=1
    info "previous install detected: upgrade mode (data and .env are preserved)"
fi

# ---------------------------------------------------------- download+verify --
TMP=$(mktemp -d) || fatal 34 "mktemp failed"
cleanup() { rm -rf "$TMP"; return 0; }
trap cleanup EXIT INT TERM

info "downloading $ASSET"
fetch_to "$BASE_URL/$ASSET" "$TMP/$ASSET" || fatal 32 "download failed: $BASE_URL/$ASSET"
fetch_to "$BASE_URL/checksums.txt" "$TMP/checksums.txt" || fatal 32 "checksums.txt not found in release $VERSION"
[ -s "$TMP/$ASSET" ] || fatal 32 "downloaded asset is empty"

SUM_FILE=$(sha256sum "$TMP/$ASSET" | awk '{print $1}')
SUM_REF=$(grep "  $ASSET\$" "$TMP/checksums.txt" | awk '{print $1}')
[ -n "$SUM_REF" ] || fatal 33 "$ASSET not listed in checksums.txt"
[ "$SUM_FILE" = "$SUM_REF" ] || fatal 33 "sha256 MISMATCH for $ASSET — corrupt or tampered download"
ok "sha256 verified"

tar -tzf "$TMP/$ASSET" >/dev/null 2>&1 || fatal 34 "tarball is corrupt"
tar -xzf "$TMP/$ASSET" -C "$TMP"
[ -s "$TMP/$BIN_NAME" ] || fatal 34 "tarball does not contain $BIN_NAME"

# ------------------------------------------------------------------ install --
run $SUDO install -d -m 0755 "$INSTALL_DIR"
if [ "$UPGRADING" -eq 1 ]; then
    run $SUDO cp -a "$INSTALL_DIR/$BIN_NAME" "$INSTALL_DIR/$BIN_NAME.bak"
    info "previous binary backed up: $INSTALL_DIR/$BIN_NAME.bak"
fi
run $SUDO install -T -m 0755 "$TMP/$BIN_NAME" "$INSTALL_DIR/$BIN_NAME"
ok "binary installed at $INSTALL_DIR/$BIN_NAME"

if ! id "$APP_NAME" >/dev/null 2>&1; then
    run $SUDO useradd --system --home-dir "$STATE_DIR" --shell /usr/sbin/nologin "$APP_NAME"
    ok "system user $APP_NAME created"
fi
run $SUDO install -d -m 0750 -o "$APP_NAME" -g "$APP_NAME" "$STATE_DIR"

# SELinux: context fix, degrade to warning
if command -v getenforce >/dev/null 2>&1 && [ "$(getenforce)" = "Enforcing" ]; then
    run $SUDO chcon -t bin_t "$INSTALL_DIR/$BIN_NAME" 2>/dev/null \
        || warn "SELinux Enforcing: if the service fails, check 'ausearch -m avc -ts recent'"
fi

# Initial config ONLY on fresh install (upgrades never touch .env or data)
if [ ! -f "$STATE_DIR/.env" ]; then
    ADMIN_PASS=$(head -c 18 /dev/urandom | base64 | tr -d '/+=' | head -c 16)
    if [ "$DRY_RUN" -eq 1 ]; then info "[dry-run] would generate $STATE_DIR/.env (0600, random admin password, PORT=$PORT, DEMO_MODE=$DEMO)"; else
        $SUDO tee "$STATE_DIR/.env" >/dev/null <<EOF
PORT=$PORT
DATA_DIR=./data
AUTH_USER=admin
AUTH_PASS=$ADMIN_PASS
DEMO_MODE=$DEMO
SSH_KEY_PATH=$STATE_DIR/.ssh/id_ed25519
EOF
        $SUDO chmod 0600 "$STATE_DIR/.env"
        $SUDO chown "$APP_NAME:$APP_NAME" "$STATE_DIR/.env"
    fi
    FRESH_CREDENTIALS=1
else
    FRESH_CREDENTIALS=0
    PORT=$(sed -n 's/^PORT=//p' "$STATE_DIR/.env" | head -1)
    PORT="${PORT:-$DEFAULT_PORT}"
    DEMO=$(sed -n 's/^DEMO_MODE=//p' "$STATE_DIR/.env" | head -1)
    DEMO="${DEMO:-0}"
    info "existing config kept ($STATE_DIR/.env)"
fi

# ----------------------------------------------------------------- service --
if [ "$DRY_RUN" -eq 1 ]; then info "[dry-run] would write systemd unit + enable --now"; else
    $SUDO tee "/etc/systemd/system/$SERVICE_NAME.service" >/dev/null <<EOF
[Unit]
Description=NetPulse — network monitoring dashboard (OpenWrt/GL.iNet)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$APP_NAME
Group=$APP_NAME
WorkingDirectory=$STATE_DIR
ExecStart=$INSTALL_DIR/$BIN_NAME
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
PrivateTmp=true
LockPersonality=true
RemoveIPC=true
StateDirectory=$APP_NAME
UMask=007

[Install]
WantedBy=multi-user.target
EOF

    # Unidad de reinicio bajo demanda (issue #4 + updater): una unit .path
    # vigila $STATE_DIR/data/.restart-me; cuando el servidor lo toca (p.ej.
    # POST /api/demo/enable) este oneshot lo borra y reinicia el servicio.
    $SUDO tee "/etc/systemd/system/$SERVICE_NAME-restart.path" >/dev/null <<EOF
[Unit]
Description=Reinicia $SERVICE_NAME cuando el servidor toca .restart-me

[Path]
PathChanged=$STATE_DIR/data/.restart-me

[Install]
WantedBy=multi-user.target
EOF
    $SUDO tee "/etc/systemd/system/$SERVICE_NAME-restart.service" >/dev/null <<EOF
[Unit]
Description=Reinicio de $SERVICE_NAME solicitado por el servidor

[Service]
Type=oneshot
ExecStart=/bin/sh -c "rm -f $STATE_DIR/data/.restart-me; /bin/systemctl restart $SERVICE_NAME.service"
EOF
fi
run $SUDO systemctl daemon-reload
if [ "$UPGRADING" -eq 1 ]; then run $SUDO systemctl restart "$SERVICE_NAME"
else run $SUDO systemctl enable --now "$SERVICE_NAME"; fi
run $SUDO systemctl enable --now "$SERVICE_NAME-restart.path" 2>/dev/null \
    || warn "could not enable $SERVICE_NAME-restart.path (demo/update auto-restart)"

if [ "$DRY_RUN" -eq 0 ]; then
    sleep 3
    if ! $SUDO systemctl is-active --quiet "$SERVICE_NAME"; then
        err "service failed to start; diagnostics:"
        $SUDO systemctl status "$SERVICE_NAME" --no-pager || true
        $SUDO journalctl -u "$SERVICE_NAME" -n 50 --no-pager || true
        exit 40
    fi
    ok "service $SERVICE_NAME active"
    if command -v curl >/dev/null 2>&1; then
        curl -fsS --max-time 5 "http://127.0.0.1:$PORT/api/health" >/dev/null 2>&1 \
            && ok "HTTP health check OK on :$PORT" \
            || warn "service is up but http://127.0.0.1:$PORT didn't answer yet (give it a few seconds)"
    fi
fi

# ------------------------------------------------------------------ summary --
printf '\n%s================ %s installed ================%s\n' "$C_G" "$APP_NAME" "$C_0"
printf 'Version:  %s%s\n' "$VERSION" "$( [ "$UPGRADING" -eq 1 ] && echo " (upgrade — previous binary at $INSTALL_DIR/$BIN_NAME.bak)" || true)"
printf 'Binary:   %s\n' "$INSTALL_DIR/$BIN_NAME"
printf 'Data:     %s (SQLite, .env, SSH keypair)\n' "$STATE_DIR"
printf 'Access:   http://<this-machine-ip>:%s\n' "$PORT"
if [ "${FRESH_CREDENTIALS:-0}" -eq 1 ] && [ "$DRY_RUN" -eq 0 ]; then
    printf '\nInitial credentials (shown ONCE — change them after logging in):\n'
    printf '  user:     admin\n  password: %s\n' "$ADMIN_PASS"
fi
printf '\nUseful commands:\n'
printf '  systemctl status %s\n  journalctl -u %s -f\n' "$SERVICE_NAME" "$SERVICE_NAME"
printf '  sh install.sh              # update to the latest stable version\n'
printf '  sh install.sh --uninstall\n'
printf '\nNotes:\n'
if [ "${DEMO:-0}" = "1" ]; then
    printf '  - DEMO MODE: sample network with 60+ devices; your routers are untouched.\n'
    printf '    To go live: set DEMO_MODE=0 in %s/.env and systemctl restart %s\n' "$STATE_DIR" "$SERVICE_NAME"
else
    printf '  - To explore with sample data first: set DEMO_MODE=1 in %s/.env\n' "$STATE_DIR"
    printf '    and systemctl restart %s (demo mode never touches your routers).\n' "$SERVICE_NAME"
fi
printf '  - No firewall port was opened. If you need one:\n'
printf '      firewall-cmd --permanent --add-port=%s/tcp && firewall-cmd --reload\n' "$PORT"
printf '      ufw allow %s/tcp\n' "$PORT"
printf '  - Live mode: authorize the server SSH public key on each router\n'
printf '    (Settings shows it; append to /etc/dropbear/authorized_keys).\n'
printf '  - The in-app updater follows rolling builds from main; for stable\n'
printf '    updates re-run this script.\n'
printf '%s================================================%s\n\n' "$C_G" "$C_0"
