#!/usr/bin/env bash
# install-agent.sh — join an EVDR K3s worker node to the cluster.
#
# Supply-chain policy: the pinned upstream binary is downloaded and verified
# against the release SHA256 before installation. Nothing is piped from the
# network into a shell, and no downloaded script is executed.
#
# Usage:
#   ./install-agent.sh <server-ip>
#
# Required environment:
#   K3S_TOKEN    shared cluster secret — from environment or Vault, never on
#                the command line. See install-server.sh header.
# Optional environment:
#   K3S_VERSION       pinned version (must match the servers).
#   K3S_BINARY_PATH   pre-staged binary for air-gapped installs.
#   K3S_SHA256        expected SHA256 of the binary (mandatory with
#                     K3S_BINARY_PATH, optional extra pin otherwise).
set -euo pipefail

K3S_VERSION="${K3S_VERSION:-v1.32.5+k3s1}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG_DST="/etc/rancher/k3s/config.yaml"
ENV_DST="/etc/rancher/k3s/agent.env"
BIN_DST="/usr/local/bin/k3s"

: "${K3S_TOKEN:?K3S_TOKEN must be set in the environment (see install-server.sh header)}"

SERVER_IP="${1:-}"
if [[ -z "${SERVER_IP}" ]]; then
  echo "Usage: $0 <server-ip>" >&2
  exit 1
fi

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: run as root (or via sudo -E to preserve K3S_TOKEN)." >&2
  exit 1
fi

# --- 1. Install the verified k3s binary --------------------------------------

if [[ -n "${K3S_BINARY_PATH:-}" ]]; then
  : "${K3S_SHA256:?K3S_SHA256 is mandatory when K3S_BINARY_PATH is used}"
  echo "${K3S_SHA256}  ${K3S_BINARY_PATH}" | sha256sum -c -
  install -m 0755 "${K3S_BINARY_PATH}" "${BIN_DST}"
else
  DOWNLOAD_DIR="$(mktemp -d)"
  trap 'rm -rf "${DOWNLOAD_DIR}"' EXIT
  RELEASE_BASE="https://github.com/k3s-io/k3s/releases/download/${K3S_VERSION}"

  curl -sfL "${RELEASE_BASE}/k3s" -o "${DOWNLOAD_DIR}/k3s"
  curl -sfL "${RELEASE_BASE}/sha256sum-amd64.txt" -o "${DOWNLOAD_DIR}/sha256sum-amd64.txt"

  (cd "${DOWNLOAD_DIR}" && grep -E '\sk3s$' sha256sum-amd64.txt | sha256sum -c -)
  if [[ -n "${K3S_SHA256:-}" ]]; then
    echo "${K3S_SHA256}  ${DOWNLOAD_DIR}/k3s" | sha256sum -c -
  fi

  install -m 0755 "${DOWNLOAD_DIR}/k3s" "${BIN_DST}"
fi

ln -sf "${BIN_DST}" /usr/local/bin/crictl
ln -sf "${BIN_DST}" /usr/local/bin/ctr

# --- 2. Configuration ----------------------------------------------------------

install -d -m 0755 /etc/rancher/k3s
install -m 0644 "${REPO_ROOT}/config/agent.yaml" "${CONFIG_DST}"

{
  echo "K3S_TOKEN=${K3S_TOKEN}"
  echo "K3S_URL=https://${SERVER_IP}:6443"
} > "${ENV_DST}"
chmod 0600 "${ENV_DST}"

# --- 3. systemd unit -------------------------------------------------------------

cat > /etc/systemd/system/k3s-agent.service <<'UNIT'
[Unit]
Description=Lightweight Kubernetes (K3s agent)
Documentation=https://k3s.io
Wants=network-online.target
After=network-online.target

[Service]
Type=exec
EnvironmentFile=/etc/rancher/k3s/agent.env
Delegate=yes
KillMode=process
LimitNOFILE=1048576
LimitNPROC=infinity
LimitCORE=infinity
TasksMax=infinity
TimeoutStartSec=0
Restart=always
RestartSec=5s
ExecStartPre=-/sbin/modprobe br_netfilter
ExecStartPre=-/sbin/modprobe overlay
ExecStart=/usr/local/bin/k3s agent --config /etc/rancher/k3s/config.yaml

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now k3s-agent.service

echo "K3s agent joined https://${SERVER_IP}:6443 at ${K3S_VERSION}."
