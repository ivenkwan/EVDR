# EVDR Frontend — Monorepo

Next.js 15 (App Router) + TypeScript frontend for the EVDR Enterprise Virtual
Data Room. Two applications share one UI kit and one set of state conventions:

| Package | What it is | Surface |
|---|---|---|
| `apps/portal` (`@evdr/portal`) | **External portal** — guest-facing, branded room pages for counterparties (TR-3.1) | Port `3000` |
| `apps/admin` (`@evdr/admin`) | **Admin console** — internal tenant admin / platform operator UI (TR-3.4, FR-4.7) | Port `3001` |
| `packages/ui` (`@evdr/ui`) | Shared UI kit — shadcn/ui owned components + Tailwind theme tokens (TR-3.2) | — |

**Scope of this baseline:** routing shells and state wiring only (TR-3.1–3.4).
Rooms, documents, viewer, NDA/consent gates and branding features (FR-1.x)
arrive in later waves — see `Todo.md` Phase 1.

## Requirements

- Node.js ≥ 22 (`v22.23.2` used in CI/dev)
- **pnpm only** — never npm/yarn (AGENTS.md hard rule). `pnpm 11.24.0` pinned
  in `packageManager`.

## Getting started

```bash
cd src/frontend
pnpm install        # install the whole workspace

pnpm dev            # both apps, portal :3000 + admin :3001
pnpm dev:portal     # portal only
pnpm dev:admin      # admin only

pnpm build          # production build of BOTH apps (portal then admin)
pnpm build:portal
pnpm build:admin

pnpm typecheck      # tsc --noEmit across all packages
pnpm lint           # ESLint (apps)
```

## Structure

```
src/frontend/
├── package.json            # workspace scripts (dev/build/typecheck/lint)
├── pnpm-workspace.yaml     # apps/* + packages/*
├── apps/
│   ├── portal/             # external guest-facing portal (App Router)
│   │   └── src/
│   │       ├── app/        # routes: /, /rooms, /rooms/[roomId] ((site) shell)
│   │       ├── components/ # site-header (client), site-footer
│   │       ├── providers/  # AppProviders → QueryClientProvider
│   │       ├── stores/     # use-portal-store (Zustand)
│   │       └── lib/        # api gateway client, query-client factory
│   └── admin/              # internal console (App Router)
│       └── src/
│           ├── app/        # routes: /dashboard, /rooms, /users, /settings
│           │               # ((console) shell, / redirects to /dashboard)
│           ├── components/ # admin-sidebar (client), admin-topbar (client)
│           ├── providers/  # AppProviders → QueryClientProvider
│           ├── stores/     # use-admin-store (Zustand)
│           └── lib/        # api gateway client, query-client factory
└── packages/
    └── ui/                 # @evdr/ui — shadcn Button + Card, cn(), tokens.css
```

## Conventions (AGENTS.md)

- **Server Components by default**; `"use client"` only for interactive parts.
- **All data fetching via TanStack Query** against the API gateway — never raw
  `fetch()` in components. The typed gateway client is
  `src/lib/api.ts` (`api.get` / `api.post`, `ApiError`).
- **Zustand for client state** (UI-only: menus, drawers); server state never
  lives in the store.
- **shadcn/ui owned components** (copy-paste into `packages/ui`, not a runtime
  dependency) + **rapid per-room theming** via CSS custom properties: tokens
  live in `packages/ui/src/styles/tokens.css`, each app maps them with its own
  `@theme inline` block, and future room branding overrides the variables.
- **Zod schemas** for API response validation arrive with the first real API
  contracts (no endpoints exist yet in the baseline).
- kebab-case file names; `console` with severity levels, never `print()`.

## Configuration

| Variable | Used by | Default |
|---|---|---|
| `NEXT_PUBLIC_API_BASE_URL` | `src/lib/api.ts` | `/api/v1` (same-origin) |

Copy `.env.example` to `.env.local` to override. No secrets are committed
(AGENTS.md hard rule).

## Adding shadcn/ui components

```bash
# from apps/portal or apps/admin (components.json is pre-configured to write
# into packages/ui/src/components)
pnpm --filter @evdr/ui exec shadcn@latest add <component>
```
