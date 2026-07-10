#!/usr/bin/env bash
# ComputeBox Craftpanel installer for Linux (systemd).
#
# One command install:
#   curl -fsSL https://raw.githubusercontent.com/computenord/craftpanel/main/install/install.sh | sudo bash
#
# Uninstall (server data in /var/lib/craftpanel is kept):
#   curl -fsSL https://raw.githubusercontent.com/computenord/craftpanel/main/install/install.sh | sudo bash -s -- --uninstall
#
# Environment overrides:
#   CRAFTPANEL_REPO      GitHub repo to download from (default: computenord/craftpanel)
#   CRAFTPANEL_VERSION   Release tag (default: latest)
#   CRAFTPANEL_PORT      Panel port (default: 8420)
#   CRAFTPANEL_URL       Download the binary from this URL instead of a GitHub
#                        release. Useful to test a build before publishing:
#                          sudo CRAFTPANEL_URL=http://192.168.1.5:8000/craftpanel-linux-amd64 bash install.sh
set -euo pipefail

REPO="${CRAFTPANEL_REPO:-computenord/craftpanel}"
VERSION="${CRAFTPANEL_VERSION:-latest}"
PORT="${CRAFTPANEL_PORT:-8420}"
BIN=/usr/local/bin/craftpanel
DATA_DIR=/var/lib/craftpanel
UNIT=/etc/systemd/system/craftpanel.service
SERVICE_USER=craftpanel

say()  { printf '\033[1;34m[craftpanel]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[craftpanel]\033[0m %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || fail "Please run as root, for example: curl -fsSL .../install.sh | sudo bash"
command -v systemctl >/dev/null 2>&1 || fail "systemd is required"

if [ "${1:-}" = "--uninstall" ]; then
  say "Stopping and removing the craftpanel service"
  systemctl disable --now craftpanel 2>/dev/null || true
  rm -f "$UNIT" "$BIN"
  systemctl daemon-reload
  say "Removed. Server data is still in $DATA_DIR, delete it yourself if you want a full wipe."
  exit 0
fi

case "$(uname -m)" in
  x86_64|amd64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) fail "Unsupported architecture: $(uname -m)" ;;
esac

if [ -n "${CRAFTPANEL_URL:-}" ]; then
  URL="$CRAFTPANEL_URL"
  say "Downloading craftpanel from $URL"
elif [ "$VERSION" = "latest" ]; then
  URL="https://github.com/$REPO/releases/latest/download/craftpanel-linux-$ARCH"
  say "Downloading craftpanel ($ARCH) from $REPO ($VERSION)"
else
  URL="https://github.com/$REPO/releases/download/$VERSION/craftpanel-linux-$ARCH"
  say "Downloading craftpanel ($ARCH) from $REPO ($VERSION)"
fi

TMP=$(mktemp)
trap 'rm -f "$TMP"' EXIT
curl -fSL --progress-bar -o "$TMP" "$URL" || fail "Download failed: $URL"
chmod 755 "$TMP"

WAS_RUNNING=0
if systemctl is-active --quiet craftpanel; then
  WAS_RUNNING=1
  say "Stopping running panel for the upgrade"
  systemctl stop craftpanel
fi

if ! id -u "$SERVICE_USER" >/dev/null 2>&1; then
  say "Creating system user $SERVICE_USER"
  useradd --system --home-dir "$DATA_DIR" --shell /usr/sbin/nologin "$SERVICE_USER"
fi

# The binary belongs to the service user so the panel can update itself from
# the web UI. ReadWritePaths below limits that write access to exactly this
# one file.
install -m 755 -o "$SERVICE_USER" -g "$SERVICE_USER" "$TMP" "$BIN"

mkdir -p "$DATA_DIR"
chown -R "$SERVICE_USER:$SERVICE_USER" "$DATA_DIR"
chmod 750 "$DATA_DIR"

if [ -f "$UNIT" ]; then
  cp -a "$UNIT" "$UNIT.bak"
  say "Existing unit backed up to $UNIT.bak"
fi

say "Writing systemd service"
cat > "$UNIT" <<EOF
[Unit]
Description=ComputeBox Craftpanel (Minecraft server panel)
Documentation=https://github.com/$REPO
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_USER
ExecStart=$BIN -data $DATA_DIR -addr :$PORT
Restart=always
RestartSec=3

# Only signal the panel itself on stop. It shuts its Minecraft servers down
# gracefully (world save) and exits; systemd kills leftovers after the timeout.
KillMode=mixed
TimeoutStopSec=90

# Hardening. MemoryDenyWriteExecute stays off because the Java JIT needs it.
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$DATA_DIR $BIN
PrivateTmp=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LockPersonality=true

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now craftpanel
if [ "$WAS_RUNNING" -eq 1 ]; then
  say "Upgrade complete, panel restarted"
fi

if ! command -v java >/dev/null 2>&1; then
  say "NOTE: Java was not found. Minecraft servers need a Java runtime:"
  say "  Debian/Ubuntu:  apt install -y openjdk-21-jre-headless"
  say "  RHEL/Alma:      dnf install -y java-21-openjdk-headless"
  say "  Minecraft 26.1 and newer needs Java 25, install openjdk-25 instead."
else
  say "Java found: $(java -version 2>&1 | head -1)"
  say "Note that Minecraft 26.1 and newer needs Java 25. The panel tells you"
  say "before a server starts if the installed Java is too old."
fi

IP=$( (hostname -I 2>/dev/null || true) | awk '{print $1}' || true)
[ -n "$IP" ] || IP=127.0.0.1
say ""
say "Done. Open http://$IP:$PORT in your browser."
say "The first visit creates your admin account."
say ""
say "Useful commands:"
say "  systemctl status craftpanel        service status"
say "  journalctl -u craftpanel -f        live panel logs"
say "  echo 'newpass' | sudo -u $SERVICE_USER -- $BIN -data $DATA_DIR reset-password <user>"
