# libvirt reference implementation of the EVDR cell contract.
# See src/infra/terraform/README.md for the contract and cloud-agnostic rules.

provider "libvirt" {
  uri = var.libvirt_uri
}

locals {
  prefix     = split("/", var.network_cidr)[1]
  node_ips   = [for i in range(var.node_count) : cidrhost(var.network_cidr, var.ip_offset + i)]
  node_names = [for i in range(var.node_count) : "${var.cluster_name}-node-${i}"]
}

# --- Network ---------------------------------------------------------------

resource "libvirt_network" "cell" {
  name      = "${var.cluster_name}-net"
  mode      = "nat" # isolated private segment; outbound via host NAT + egress firewall (SR-3.1)
  domain    = var.network_domain
  addresses = [var.network_cidr]
  autostart = true

  dhcp {
    enabled = false # static addressing via cloud-init for reproducibility
  }

  dns {
    enabled    = true
    local_only = false
  }
}

# --- Storage ----------------------------------------------------------------

resource "libvirt_volume" "base" {
  name   = "${var.cluster_name}-base.qcow2"
  source = var.base_image
  format = "qcow2"
}

resource "libvirt_volume" "root" {
  count          = var.node_count
  name           = "${local.node_names[count.index]}-root.qcow2"
  base_volume_id = libvirt_volume.base.id
  size           = var.node_disk_gb * 1024 * 1024 * 1024
  format         = "qcow2"
}

# Unformatted data disk per node — consumed by the Rook Ceph operator in
# Phase 1 (TR-2.5). Terraform provisions; Ceph owns it thereafter.
resource "libvirt_volume" "data" {
  count  = var.data_disk_gb > 0 ? var.node_count : 0
  name   = "${local.node_names[count.index]}-data.qcow2"
  size   = var.data_disk_gb * 1024 * 1024 * 1024
  format = "qcow2"
}

# --- Cloud-init --------------------------------------------------------------

resource "libvirt_cloudinit_disk" "node" {
  count = var.node_count
  name  = "${local.node_names[count.index]}-cloudinit.iso"

  user_data = templatefile("${path.module}/templates/cloud-init.yaml.tftpl", {
    hostname = local.node_names[count.index]
    domain   = var.network_domain
    ssh_keys = var.ssh_authorized_keys
  })

  network_config = templatefile("${path.module}/templates/network-config.yaml.tftpl", {
    ip      = local.node_ips[count.index]
    prefix  = local.prefix
    gateway = cidrhost(var.network_cidr, 1)
  })
}

# --- Compute ------------------------------------------------------------------

resource "libvirt_domain" "node" {
  count  = var.node_count
  name   = local.node_names[count.index]
  memory = var.node_memory_mb
  vcpu   = var.node_vcpus

  cpu {
    mode = "host-passthrough"
  }

  network_interface {
    network_id     = libvirt_network.cell.id
    addresses      = [local.node_ips[count.index]]
    wait_for_lease = false # DHCP disabled; address assigned by cloud-init
  }

  disk {
    volume_id = libvirt_volume.root[count.index].id
  }

  dynamic "disk" {
    for_each = var.data_disk_gb > 0 ? [1] : []
    content {
      volume_id = libvirt_volume.data[count.index].id
    }
  }

  cloudinit = libvirt_cloudinit_disk.node[count.index].id

  console {
    type        = "pty"
    target_type = "serial"
    target_port = "0"
  }

  graphics {
    type        = "vnc"
    listen_type = "address"
    autoport    = true
  }
}
