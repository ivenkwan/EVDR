# Lab environment notes — Windows 11 + Docker Desktop (WSL2)

**Scope:** these notes cover ONLY the Phase 0 lab on a Windows workstation
(Docker Desktop 28.1.1, WSL2 backend, containerd image store enabled). They
are the lab-specific deviations from `docs/runbooks/phase-0-foundation-rebuild.md`,
which targets the production libvirt/K3s path. None of this applies to
production hosts; none of it belongs in the production runbook.

Credentials and tokens are NOT in this document. They live outside the repo
under the lab secrets directory (see §7) and are referenced by path only.

## 0. Cluster create (rebuild drill §2 equivalent)

The lab substitutes k3d for the Terraform/libvirt VM layer (§1–2 of the
production runbook). Exact create invocation:

```bash
k3d cluster create evdr-lab \
  --servers 3 \
  --image rancher/k3s:v1.32.5-k3s1 \
  --k3s-arg "--config=/etc/rancher/k3s/server-lab.yaml@server:*" \
  --volume <repo>/src/infra/k3s/config/server-lab.yaml:/etc/rancher/k3s/server-lab.yaml@server:* \
  --port "443:443@loadbalancer" \
  --port "80:80@loadbalancer" \
  --api-port 6443
```

(`<repo>` = absolute Windows path of the checkout; k3d accepts
`Z:/GITHUB/EVDR/...` form on this host.) kubeconfig merges into the default
`~/.kube/config`, context `k3d-evdr-lab`. Then continue at runbook §3
(namespaces/policies). The GitLab container must join the `k3d-evdr-lab`
network with its alias (§4 below) — do this right after cluster create, not
after helm installs.

## 1. Docker socket for containers that need the Docker API (GitLab Runner)

With Docker Desktop 28.1.1 and the containerd snapshotter enabled, mounting
the Windows named pipe (`-v //./pipe/docker_engine:/var/run/docker.sock`)
produces an **empty directory** inside Linux containers. The "Expose daemon on
tcp://localhost:2375" setting is Hyper-V-backend-only and never listens under
WSL2. Both were verified empirically; do not retry them.

The working socket lives inside the `docker-desktop` WSL VM:

```
/run/guest-services/docker.container-proxy.sock
```

Bind-mount sources that look like POSIX absolute paths resolve VM-locally, so:

```bash
docker run -v /run/guest-services/docker.container-proxy.sock:/var/run/docker.sock ...
```

gives the container a real Docker API socket — no TCP exposure, no privileged
dind. The GitLab Runner container must use this mount.

## 2. Windows host paths from runner-spawned job containers

The runner's Linux binary rejects `C:/...` volume specs ("invalid volume
specification"). Inside the docker-desktop VM, Windows `C:` is `/mnt/host/c`,
so `config.toml` `[runners.docker].volumes` entries use the VM path form, e.g.:

```toml
volumes = [
  "/mnt/host/c/temp/evdr-lab/runner-builds:/builds",
  "/mnt/host/c/temp/evdr-lab/runner-cache:/cache",
  "...ca cert mounts, see §5..."
]
```

A bare `/cache` entry alongside the `/cache` bind fails with "volume for
container path already defined" — keep exactly one entry per destination.

## 3. GitLab artifact upload 500 on Windows bind mounts

GitLab's carrierwave does `rename` + immediate `chmod` inside
`gitlab-rails/shared/artifacts/tmp/work`; on a gRPC-FUSE Windows share this
races and dies with `Errno::ENOENT @ apply2files` → artifact upload HTTP 500.

Fix: a named ext4 volume over ONLY the artifacts path; the rest of GitLab
state stays on the Windows binds:

```
-v evdr-gitlab-artifacts:/var/opt/gitlab/gitlab-rails/shared/artifacts
```

## 4. GitLab container lifecycle

- Image `gitlab/gitlab-ce:17.7.0-ce.0`, restart policy `no`. After ANY Docker
  Desktop restart: `docker start evdr-gitlab` (and the runner container).
- Ports: `8443:8443` (web/API), `5050:5050` (registry), `2224:22` (ssh).
- Omnibus config via `GITLAB_OMNIBUS_CONFIG`: external_url
  `https://gitlab.evdr.internal:8443`, registry_external_url
  `https://gitlab.evdr.internal:5050`, letsencrypt off, prometheus off,
  registry enabled.
- The container must carry the network alias `gitlab.evdr.internal` on the
  `k3d-evdr-lab` network so job containers and the cluster resolve it. After
  any recreate (new bridge IP), re-pin:

  ```bash
  docker network disconnect k3d-evdr-lab evdr-gitlab
  docker network connect k3d-evdr-lab evdr-gitlab --alias gitlab.evdr.internal
  ```

## 5. Runner container and config.toml essentials

- Image `gitlab/gitlab-runner:alpine-v17.7.0`, restart `unless-stopped`,
  networks bridge + `k3d-evdr-lab`, socket mount per §1.
- `config.toml` (full file lives in the lab runner config dir, token inside —
  never copy into the repo):
  - `concurrent = 2`, executor `docker`, `builds_dir = "/builds"`
  - `tls-ca-file` → lab root CA cert mount
  - `environment = ["GIT_SSL_CAINFO=/etc/ssl/certs/evdr-root-ca.pem"]`
  - `[runners.docker]`: `image = "alpine:3.21"`, `privileged = false`,
    `extra_hosts = ["gitlab.evdr.internal:host-gateway",
    "hello.staging.evdr.internal:host-gateway"]`
  - volume mounts for builds/cache (§2) plus the lab root CA at BOTH
    `/etc/ssl/certs/evdr-root-ca.pem` and `/kaniko/ssl/certs/evdr-root-ca.pem`
    — kaniko honours only `SSL_CERT_DIR=/kaniko/ssl/certs` (image env), so a
    single `/etc/ssl/certs` mount is not sufficient for the build job.

## 6. CI image gotchas (encoded in `.gitlab-ci.yml`; repeated for rebuilds)

- Tool images (terraform, trivy, syft, kubectl, ZAP) set ENTRYPOINT to the
  tool; GitLab passes its shell script as argv → "Terraform has no command
  named sh". Fix in the pipeline: `image: { name: ..., entrypoint: [""] }`.
- `anchore/syft` (plain tag) has no shell at all → use the `-debug` tag; the
  binary is `/syft`, off the busybox PATH, so call it by absolute path.
- Private registry pulls in scan jobs: export `TRIVY_USERNAME` /
  `TRIVY_PASSWORD` and `SYFT_REGISTRY_AUTH_USERNAME` /
  `SYFT_REGISTRY_AUTH_PASSWORD` from the predefined `CI_REGISTRY_USER` /
  `CI_REGISTRY_PASSWORD`.
- bitnami/kubectl versioned tags were removed from the free Docker Hub tier;
  the deploy job installs kubectl v1.32.5 on `alpine:3.21` from dl.k8s.io with
  sha256 verification instead.
- `cytopia/yamllint` was unpublished; `lint:yaml` uses `python:3.13-alpine`
  with a pinned `yamllint==1.35.1`.

## 7. Secrets inventory (all OUTSIDE the repo, lab secrets directory)

`vault-init.json` (5 unseal keys, threshold 3; original root token revoked),
`vault-admin-token` (orphan token for day-to-day), `bootstrap-ca.crt`,
`evdr-root-ca.crt`/`.pem` (trust store for curl/runner/kaniko),
`gitlab-root-password`, `gitlab-root-pat`, `gitlab-runner-token`,
`gitlab-pull-token` (registry deploy token, `read_registry`, user `k3s-pull`),
`vault-root-recovered` (break-glass drill output).

## 8. Cluster state across Docker restarts

- Vault (3-node raft) seals when its containers restart. Unseal with 3 of the
  5 shares from `vault-init.json`. cert-manager-issued certs persist; nothing
  else breaks.
- kubeconfig: default `~/.kube/config`, context `k3d-evdr-lab`.
- The staging namespace pull secret `gitlab-registry-pull` (created per
  runbook §8 from the deploy token) persists in the cluster; recreate it only
  after a full cluster rebuild.

## 9. Phase 0 gate evidence location

- Pipeline: `https://gitlab.evdr.internal:8443/evdr/evdr/-/pipelines/8`
  (commit `4b24dddd`, all stages green including manual deploy + DAST).
- Extracted artifacts + `EVIDENCE-SUMMARY.txt`: lab evidence directory
  `evidence/phase-0-pipeline-8/` under the lab temp root (outside the repo).
- Live check: `curl --cacert <lab root CA> --resolve
  hello.staging.evdr.internal:443:127.0.0.1
  https://hello.staging.evdr.internal/version` → `{"version":"4b24dddd"}`
  with the full security-header set.
