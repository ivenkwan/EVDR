output "node_names" {
  description = "Ordered node names."
  value       = local.node_names
}

output "node_ips" {
  description = "Ordered node IPv4 addresses. Index 0 is the initial K3s server."
  value       = local.node_ips
}

output "network_name" {
  description = "libvirt network name."
  value       = libvirt_network.cell.name
}

output "kube_context_hint" {
  description = "Next step for the operator (see docs/runbooks/phase-0-foundation-rebuild.md)."
  value       = "Install K3s server on ${local.node_names[0]} (${local.node_ips[0]}) using src/infra/k3s/scripts/install-server.sh"
}
