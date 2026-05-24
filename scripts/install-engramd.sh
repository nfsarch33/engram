#!/usr/bin/env bash
# install-engramd.sh -- canonical installation procedure for engramd on a
# Linux host (bare-metal or WSL).
#
# Usage (from the build machine):
#
#   make build-linux
#   scp ./bin/engramd-linux-amd64 <target-host>:/tmp/engramd
#   scp ./bin/engramcli-linux-amd64 <target-host>:/tmp/engramcli
#   scp ./scripts/install-engramd.sh <target-host>:/tmp/install-engramd.sh
#   ssh <target-host> 'sudo bash /tmp/install-engramd.sh'
#
# The script is idempotent: re-running upgrades the binary and restarts the
# unit without touching the SQLite DB or env file (unless they are missing).

set -euo pipefail

ENGRAM_USER="${ENGRAM_USER:-engram}"
ENGRAM_GROUP="${ENGRAM_GROUP:-engram}"
ENGRAM_HOME="${ENGRAM_HOME:-/var/lib/engram}"
ENGRAM_ETC="${ENGRAM_ETC:-/etc/engramd}"
ENGRAM_BIN="${ENGRAM_BIN:-/usr/local/bin/engramd}"
ENGRAM_CLI="${ENGRAM_CLI:-/usr/local/bin/engramcli}"
ENGRAM_UNIT="${ENGRAM_UNIT:-/etc/systemd/system/engramd.service}"

SRC_DAEMON="${SRC_DAEMON:-/tmp/engramd}"
SRC_CLI="${SRC_CLI:-/tmp/engramcli}"

if [[ $EUID -ne 0 ]]; then
  echo "must run as root (use: sudo bash $0)" >&2
  exit 1
fi

echo "[1/6] ensuring system user '${ENGRAM_USER}' exists"
if ! id -u "${ENGRAM_USER}" >/dev/null 2>&1; then
  useradd --system \
    --home-dir "${ENGRAM_HOME}" \
    --shell /usr/sbin/nologin \
    --user-group \
    "${ENGRAM_USER}"
fi

echo "[2/6] preparing directories"
install -d -o "${ENGRAM_USER}" -g "${ENGRAM_GROUP}" -m 0750 "${ENGRAM_HOME}"
install -d -o root -g "${ENGRAM_GROUP}" -m 0750 "${ENGRAM_ETC}"

echo "[3/6] installing binaries"
install -o root -g root -m 0755 "${SRC_DAEMON}" "${ENGRAM_BIN}"
install -o root -g root -m 0755 "${SRC_CLI}" "${ENGRAM_CLI}"

echo "[4/6] writing default env file (only if missing)"
if [[ ! -f "${ENGRAM_ETC}/engramd.env" ]]; then
  cat >"${ENGRAM_ETC}/engramd.env" <<'EOF'
# Engram daemon environment. All values are read by engramd at start.
# Bind to loopback only -- remote access is through an SSH tunnel or proxy.
ENGRAM_ADDR=127.0.0.1:8280
ENGRAM_DB_PATH=/var/lib/engram/engram.db
ENGRAM_COLLECTION=engram
ENGRAM_EMBEDDING_DIM=768

# Mem0 OSS wire-compatible HTTP shim. Loopback only; reach from a remote
# machine through an SSH tunnel or reverse proxy. Setting ENGRAM_API_KEY
# enables the X-API-Key gate; an empty value disables the gate.
ENGRAM_MEM0COMPAT_ADDR=127.0.0.1:8281
ENGRAM_API_KEY=

# Local Ollama OpenAI-compatible embedder.
ENGRAM_EMBED_URL=http://127.0.0.1:11434/v1
ENGRAM_EMBED_MODEL=nomic-embed-text

ENGRAM_LOG_LEVEL=info
EOF
  chown root:"${ENGRAM_GROUP}" "${ENGRAM_ETC}/engramd.env"
  chmod 0640 "${ENGRAM_ETC}/engramd.env"
else
  # Idempotent upgrade: append Mem0-compat env vars if they are missing
  # from a pre-v7100 env file. Never overwrite operator-set values.
  if ! grep -q '^ENGRAM_MEM0COMPAT_ADDR=' "${ENGRAM_ETC}/engramd.env"; then
    {
      echo ""
      echo "# v7100: Mem0 OSS wire-compatible shim. Loopback only."
      echo "ENGRAM_MEM0COMPAT_ADDR=127.0.0.1:8281"
      echo "ENGRAM_API_KEY="
    } >>"${ENGRAM_ETC}/engramd.env"
  fi
fi

echo "[5/6] installing systemd unit"
cat >"${ENGRAM_UNIT}" <<EOF
[Unit]
Description=Engram personal memory engine
Documentation=https://github.com/nfsarch33/engram
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${ENGRAM_USER}
Group=${ENGRAM_GROUP}
WorkingDirectory=${ENGRAM_HOME}
EnvironmentFile=${ENGRAM_ETC}/engramd.env
ExecStart=${ENGRAM_BIN} --mem0-compat
Restart=on-failure
RestartSec=5s
TimeoutStopSec=15s

# Hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=${ENGRAM_HOME}
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectKernelLogs=true
ProtectControlGroups=true
RestrictNamespaces=true
RestrictRealtime=true
LockPersonality=true
MemoryDenyWriteExecute=true
SystemCallArchitectures=native

[Install]
WantedBy=multi-user.target
EOF
chmod 0644 "${ENGRAM_UNIT}"

echo "[6/6] reloading systemd, enabling + starting engramd"
systemctl daemon-reload
systemctl enable engramd >/dev/null
systemctl restart engramd
sleep 2
systemctl --no-pager --full status engramd | head -25 || true

echo
echo "engramd installed and running."
echo "  daemon          : ${ENGRAM_BIN}"
echo "  cli             : ${ENGRAM_CLI}"
echo "  envfile         : ${ENGRAM_ETC}/engramd.env"
echo "  data            : ${ENGRAM_HOME}/engram.db"
echo "  unit            : ${ENGRAM_UNIT}"
echo "  canonical port  : 8280  (loopback)"
echo "  mem0-compat port: 8281  (loopback, --mem0-compat)"
