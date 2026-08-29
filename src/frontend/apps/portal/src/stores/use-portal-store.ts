"use client"

import { create } from "zustand"

interface PortalUiState {
  /** Mobile navigation drawer visibility (desktop nav is always visible). */
  mobileMenuOpen: boolean
  setMobileMenuOpen: (open: boolean) => void
}

/**
 * Portal client state (TR-3.3): UI-only state that does not belong in the
 * URL. Server state (rooms, documents) goes through TanStack Query instead —
 * never through this store.
 */
export const usePortalStore = create<PortalUiState>()((set) => ({
  mobileMenuOpen: false,
  setMobileMenuOpen: (mobileMenuOpen) => set({ mobileMenuOpen }),
}))
