# @evdr/ui — EVDR shared UI kit

shadcn/ui **owned** components (copy-paste, not a dependency — TR-3.2) plus the
Tailwind CSS theme tokens shared by every EVDR frontend app.

## Layout

```
packages/ui/src/
├── index.ts                 # public exports
├── components/
│   ├── button.tsx           # shadcn Button (cva variants + Slot asChild)
│   └── card.tsx             # shadcn Card family
├── lib/
│   └── utils.ts             # cn() = tailwind-merge(clsx(...))
└── styles/
    └── tokens.css           # design tokens (CSS custom properties) — source of truth
```

## How apps consume it

- **Components**: `import { Button, Card } from "@evdr/ui"` (declared as
  `"@evdr/ui": "workspace:*"`). The package ships TypeScript source; apps
  transpile it via `transpilePackages: ["@evdr/ui"]` in `next.config.ts`.
- **Tailwind utilities**: apps add `@source "<rel>/packages/ui/src"` to their
  `globals.css` so Tailwind v4 scans these components, and map tokens through
  their own `@theme inline` block.
- **Theme tokens**: apps `@import` `tokens.css`; per-room branding overrides the
  CSS custom properties at runtime.

## Adding components

```bash
pnpm --filter @evdr/ui exec shadcn@latest add <component>
```

(uses the app `components.json` aliases; components land here in
`src/components/`). Not part of the baseline — only `button` and `card` ship.
