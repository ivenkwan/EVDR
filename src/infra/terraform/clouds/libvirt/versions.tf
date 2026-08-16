terraform {
  required_version = ">= 1.7"

  required_providers {
    libvirt = {
      source = "dmacvicar/libvirt"
      # Pinned to the classic 0.7.x schema. The 0.8+ line is a breaking
      # rewrite (new resource schema); adopting it is a deliberate migration,
      # not a version bump.
      version = "~> 0.7.6"
    }
  }
}
