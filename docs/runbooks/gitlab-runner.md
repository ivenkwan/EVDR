# GitLab Self-Hosted Runner Setup (TR-1.3)

The CI pipeline (`.gitlab-ci.yml`) runs on a self-hosted runner tagged `evdr-docker`. This keeps builds inside our infrastructure (data-sovereignty posture) and lets security stages pin their own tool images.

## 1. Placement

- A dedicated VM (not a K3s node): 4 vCPU / 8 GiB / 100 GiB disk, created via the same Terraform cell module pattern (`src/infra/terraform/`).
- Egress per `src/infra/network/egress-allowlist.md`: GitLab instance, OCI registries, OS mirrors, Go module proxy (`proxy.golang.org`) for builds.

## 2. Install and register (Docker executor)

```bash
# On the runner VM (Ubuntu 24.04)
curl -fsSL https://packages.gitlab.com/install/repositories/runner/gitlab-runner/script.deb.sh -o /tmp/gl-runner.sh
# Review the script before executing; then:
sudo bash /tmp/gl-runner.sh
sudo apt-get install -y gitlab-runner docker.io
sudo usermod -aG docker gitlab-runner

# Register — token from GitLab Admin/Group → CI/CD → Runners. Pass it via the
# environment, never in shell history:
read -rs GITLAB_REGISTRATION_TOKEN   # paste, no echo
sudo gitlab-runner register \
  --non-interactive \
  --url "https://gitlab.example.internal/" \
  --token "$GITLAB_REGISTRATION_TOKEN" \
  --executor "docker" \
  --docker-image "alpine:3.21" \
  --description "evdr-docker-runner-01" \
  --tag-list "evdr-docker" \
  --locked="true" \
  --run-untagged="false"
unset GITLAB_REGISTRATION_TOKEN
```

## 3. Required masked CI/CD variables (GitLab group/project settings)

| Variable | Masked | Protected | Purpose |
|---|---|---|---|
| `KUBECONFIG_B64` | yes | yes | Staging-cluster kubeconfig, base64 (used only by `deploy:sample-staging`) |
| `VAULT_ADDR` | no | yes | Vault address for jobs that need it (none at Phase 0; reserved) |

`CI_REGISTRY_USER` / `CI_REGISTRY_PASSWORD` are provided by GitLab automatically per job — do not create custom copies.

## 4. Runner hardening

- `run-untagged=false`, `--locked=true`: only this project group's tagged jobs land here.
- No privileged mode: Kaniko builds images daemonless inside its container; the Docker executor's daemon is used only to launch job containers.
- Runner host patching follows the SR-4.4 SLA (`docs/security/vulnerability-management.md`).
- Job logs may contain scanner output but must never contain secrets; masked variables plus the Semgrep credential-literal rule are the two guardrails.

## 5. Verification

1. Push a branch: every stage through `sbom` must go green with no manual steps.
2. Confirm artifacts exist: `semgrep.sarif`, `trivy-deps.json`, `sbom-fs.cdx.json`, `trivy-image.json`, `sbom-image.*.json`.
3. Run `deploy:sample-staging` (manual), then `dast:zap-baseline` (manual); confirm `zap-report.html` artifact.
4. Record the green pipeline URL as Phase 0 exit-criterion evidence.
