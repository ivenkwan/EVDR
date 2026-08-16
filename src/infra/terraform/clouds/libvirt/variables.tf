variable "cluster_name" {
  description = "Cell/cluster identifier; prefixes all resources."
  type        = string
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
  description = "Number of K3s server nodes (1 = single-node lab, 3 = HA)."
  type        = number
  default     = 3

  validation {
    condition     = contains([1, 3, 5], var.node_count)
    error_message = "node_count must be 1, 3, or 5 (etcd quorum sizes)."
  }
}

variable "node_vcpus" {
  description = "vCPUs per node."
  type        = number
  default     = 4
}

variable "node_memory_mb" {
  description = "Memory per node in MiB."
  type        = number
  default     = 8192
}

variable "node_disk_gb" {
  description = "Root disk per node in GiB."
  type        = number
  default     = 80
}

variable "data_disk_gb" {
  description = "Additional per-node data disk in GiB (used later by Rook/Ceph). 0 = disabled."
  type        = number
  default     = 200
}

variable "ssh_authorized_keys" {
  description = "Admin SSH public keys (content, not file paths). Inject via tfvars or CI masked variable."
  type        = list(string)
  sensitive   = true
}

variable "base_image" {
  description = "Path or URL to the OS cloud image (Ubuntu Server 24.04 cloudimg recommended)."
  type        = string
}

variable "ip_offset" {
  description = "Host number inside network_cidr assigned to the first node."
  type        = number
  default     = 11
}

variable "libvirt_uri" {
  description = "libvirt connection URI of the target hypervisor."
  type        = string
  default     = "qemu:///system"
}
