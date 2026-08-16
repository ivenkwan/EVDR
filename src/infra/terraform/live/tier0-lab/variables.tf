variable "cluster_name" {
  description = "Cell/cluster identifier."
  type        = string
  default     = "evdr-t0-lab"
}

variable "network_cidr" {
  description = "Private CIDR for cluster nodes."
  type        = string
  default     = "10.40.0.0/24"
}

variable "network_domain" {
  description = "Internal DNS domain for the cell."
  type        = string
  default     = "evdr.internal"
}

variable "node_count" {
  description = "K3s server nodes (1 = single-node lab, 3 = HA)."
  type        = number
  default     = 3
}

variable "node_vcpus" {
  type    = number
  default = 4
}

variable "node_memory_mb" {
  type    = number
  default = 8192
}

variable "node_disk_gb" {
  type    = number
  default = 80
}

variable "data_disk_gb" {
  description = "Per-node data disk for Rook/Ceph (Phase 1)."
  type        = number
  default     = 200
}

variable "ssh_authorized_keys" {
  description = "Admin SSH public keys (content). Never commit real values — see terraform.tfvars.example."
  type        = list(string)
  sensitive   = true
}

variable "base_image" {
  description = "Ubuntu 24.04 cloud image path or URL on the hypervisor."
  type        = string
}

variable "ip_offset" {
  type    = number
  default = 11
}

variable "libvirt_uri" {
  type    = string
  default = "qemu:///system"
}
