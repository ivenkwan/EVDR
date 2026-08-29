"use client"

import { create } from "zustand"

interface AdminUiState {
  /** Mobile sidebar drawer visibility (desktop sidebar is always visible). */
  sidebarOpen: boolean
  setSidebarOpen: (open: boolean) => void
}

/**
 * Admin console client state (TR-3.3): UI-only state that does not belong in
 * the URL. Server state (rooms, users, audit events) goes through TanStack
 * Query instead — never through this store.
 */
export const useAdminStore = create<AdminUiState>()((set) => ({
  sidebarOpen: false,
  setSidebarOpen: (sidebarOpen) => set({ sidebarOpen }),
}))
