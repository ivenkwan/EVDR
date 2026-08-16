# Phase 0 lab environment — composes the libvirt cell module.
# See docs/runbooks/phase-0-foundation-rebuild.md for the full build order.

terraform {
  required_version = ">= 1.7"

  # Phase 0 lab keeps state locally (git-ignored). Before any real cell,
  # add a remote backend here with locking — credentials via environment,
  # never literals. See src/infra/terraform/README.md.
}

module "cell" {
  source = "../../clouds/libvirt"

  cluster_name        = var.cluster_name
  network_cidr        = var.network_cidr
  network_domain      = var.network_domain
  node_count          = var.node_count
  node_vcpus          = var.node_vcpus
  node_memory_mb      = var.node_memory_mb
  node_disk_gb        = var.node_disk_gb
  data_disk_gb        = var.data_disk_gb
  ssh_authorized_keys = var.ssh_authorized_keys
  base_image          = var.base_image
  ip_offset           = var.ip_offset
  libvirt_uri         = var.libvirt_uri
}
