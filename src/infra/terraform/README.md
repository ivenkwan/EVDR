# EVDR Terraform Baseline (TR-1.2)

Infrastructure-as-code for stamping EVDR cells: **VMs, networks, storage**. Cloud-agnostic by construction for the multi-region future (HK now, mainland evaluated in P5).

## Layout

```
terraform/
├── clouds/                     # one directory per cloud/hypervisor target
│   └── libvirt/                # reference implementation: on-prem KVM (Phase 0 lab / Tier 0)
├── live/                       # per-environment root modules (composition + tfvars)
│   └── tier0-lab/              # Phase 0 lab environment
└── README.md
```

## The cloud-agnostic contract

Terraform cannot share resources across providers; it *can* share an interface. Every target under `clouds/<name>/` implements the **same module contract** so that `live/` roots, the rebuild runbook, and operator knowledge transfer unchanged between clouds:

**Inputs (variables):**

| Variable | Type | Meaning |
|---|---|---|
| `cluster_name` | string | Cell/cluster identifier; prefixes all resources |
| `network_cidr` | string | Private CIDR for cluster nodes (e.g. `10.40.0.0/24`) |
| `node_count` | number | K3s server nodes (1 = single-node lab, 3 = HA) |
| `node_vcpus` / `node_memory_mb` / `node_disk_gb` | number | Per-node sizing |
| `ssh_authorized_keys` | list(string) | Admin SSH keys (content, not paths — inject from file/CI var) |
| `base_image` | string | OS cloud image reference for the target |
| `ip_offset` | number | First node host-number inside `network_cidr` |

**Outputs:**

| Output | Meaning |
|---|---|
| `node_names` | Ordered node names |
| `node_ips` | Ordered node IPv4 addresses (index 0 = initial K3s server) |
| `network_name` | Provider network identifier |
| `kube_context_hint` | Human-readable next-step pointer for the runbook |

Adding a cloud (AWS, Azure, Aliyun ACK-adjacent VMs for the mainland evaluation, …) = new `clouds/<name>/` implementing exactly this contract, plus a `live/` root. Mapping notes for public clouds: `network_cidr` → VPC/subnets; egress controls (SR-3.1) → security groups / NAT egress allow-list; `base_image` → AMI/image ID.

## Egress controls (SR-3.1)

Tier 1 runs in a **shared VPC with egress controls**. The allow-list of permitted outbound destinations (OCI registries, OS package mirrors, IdP endpoints, NTP) is maintained in `src/infra/network/egress-allowlist.md`. On libvirt the controls are enforced by host firewall rules applied in the rebuild runbook; public-cloud implementations express the same list as security-group/NAT rules inside their `clouds/<name>/` module.

## Usage

```bash
cd src/infra/terraform/live/tier0-lab
cp terraform.tfvars.example terraform.tfvars   # fill in environment specifics
terraform init
terraform plan -var-file=terraform.tfvars
terraform apply -var-file=terraform.tfvars
```

## State

- Phase 0 lab: local state is acceptable **inside the lab only**. `terraform.tfstate*` is git-ignored; never commit it.
- Before any real cell: move state to a remote backend with locking, with credentials injected from the environment (never literals in `.tf`). Backend config lives in the `live/<env>/` root, not in shared modules.

## Rules

- All infrastructure changes in production go through Terraform or Helm (`AGENTS.md` §7). No manual console changes.
- No secrets in `.tf` files or tfvars committed to git — tfvars with sensitive values are git-ignored; CI injects them via masked variables.
- Pin provider versions in each module's `versions.tf`; upgrades are deliberate PRs, not floating.
