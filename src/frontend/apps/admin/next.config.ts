import type { NextConfig } from "next"

const nextConfig: NextConfig = {
  // Shared workspace packages ship TypeScript source — let Next compile them.
  transpilePackages: ["@evdr/ui"],
  // Linting runs separately (`pnpm lint`); don't let it block builds.
  eslint: {
    ignoreDuringBuilds: true,
  },
}

export default nextConfig
