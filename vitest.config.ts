import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    // `packages/*` finds each package's `vitest.config.ts`. The integration
    // project shares a directory with `client`, so it is named explicitly.
    projects: ["apps/*", "packages/*", "packages/client/vitest.integration.config.ts"],
    coverage: {
      provider: "v8",
      include: ["apps/*/src/**/*.ts", "packages/*/src/**/*.ts"],
      thresholds: {
        lines: 80,
        functions: 80,
        branches: 80,
        statements: 80,
      },
    },
  },
});
