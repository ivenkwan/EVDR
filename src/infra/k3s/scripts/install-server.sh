#!/usr/bin/env bash
# install-server.sh — bootstrap or join an EVDR K3s server node.
#
# Supply-chain policy: the pinned upstream binary is downloaded and verified
# against the release SHA256 before installation. Nothing is piped from the
# network into a shell, and no downloaded script is executed.
#
# Usage:
#   ./install-server.sh init                 # first server: embedded etcd, cluster-init
#   ./install-server.sh join <server-ip>     # additional HA servers
#
# Required environment:
#   K3S_TOKEN    shared cluster secret. Read from the environment or Vault —
#                NEVER pass on the command line (visible in ps/history):
#                  export K3S_TOKEN="$(vault kv get -field=token secret/evdr/k3s/cluster)"
#                On very first bootstrap (Vault not yet live), generate one:
#                  export K3S_TOKEN="$(openssl rand -hex 32)"
#                and store it in Vault during the Vault bootstrap step.
#
# Optional environment:
#   K3S_VERSION       pinned version (default below). Bump deliberately via PR.
#   K3S_BINARY_PATH   use a local pre-staged k3s binary instead of downloading
#                     (air-gapped installs; the checksum is still verified if
#                     K3S_SHA256 is provided).
#   K3S_SHA256        expected SHA256 of the binary; mandatory when
#                     K3S_BINARY_PATH is used, optional extra pin otherwise.
set -euo pipefail

K3S_VERSION="${K3S_VERSION:-v1.32.5+k3s1}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG_DST="/etc/rancher/k3s/config.yaml"
ENV_DST="/etc/rancher/k3s/service.env"
BIN_DST="/usr/local/bin/k3s"

: "${K3S_TOKEN:?K3S_TOKEN must be set in the environment (see header)}"

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: run as root (or via sudo -E to preserve K3S_TOKEN)." >&2
  exit 1
fi

MODE="${1:-}"
case "${MODE}" in
  init) ;;
  join)
    SERVER_IP="${2:-}"
    if [[ -z "${SERVER_IP}" ]]; then
      echo "ERROR: join requires the initial server IP." >&2
      exit 1
    fi
    ;;
  *)
    echo "Usage: $0 init | join <server-ip>" >&2
    exit 1
    ;;
esac

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

  # Verify against the pinned release checksum (and an optional operator pin).
  (cd "${DOWNLOAD_DIR}" && grep -E '\sk3s$' sha256sum-amd64.txt | sha256sum -c -)
  if [[ -n "${K3S_SHA256:-}" ]]; then
    echo "${K3S_SHA256}  ${DOWNLOAD_DIR}/k3s" | sha256sum -c -
  fi

  install -m 0755 "${DOWNLOAD_DIR}/k3s" "${BIN_DST}"
fi

ln -sf "${BIN_DST}" /usr/local/bin/kubectl
ln -sf "${BIN_DST}" /usr/local/bin/crictl
ln -sf "${BIN_DST}" /usr/local/bin/ctr

# --- 2. Configuration ----------------------------------------------------------

install -d -m 0755 /etc/rancher/k3s
install -m 0644 "${REPO_ROOT}/config/server.yaml" "${CONFIG_DST}"

# Secrets go in a root-only systemd EnvironmentFile, never in the unit file,
# the repo, or the process command line.
{
  echo "K3S_TOKEN=${K3S_TOKEN}"
  if [[ "${MODE}" == "init" ]]; then
    echo "K3S_CLUSTER_INIT=true"
  else
    echo "K3S_URL=https://${SERVER_IP}:6443"
  fi
} > "${ENV_DST}"
chmod 0600 "${ENV_DST}"

# --- 3. systemd unit -------------------------------------------------------------

cat > /etc/systemd/system/k3s.service <<'UNIT'
[Unit]
Description=Lightweight Kubernetes (K3s server)
Documentation=https://k3s.io
Wants=network-online.target
After=network-online.target

[Service]
Type=notify
EnvironmentFile=/etc/rancher/k3s/service.env
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
ExecStart=/usr/local/bin/k3s server --config /etc/rancher/k3s/config.yaml

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now k3s.service

echo "K3s server (${MODE}) installed at ${K3S_VERSION}."
echo "Check: systemctl status k3s && kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml get nodes"
