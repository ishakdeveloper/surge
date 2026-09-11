import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    // `packages/*` finds each package's `vitest.config.ts`. The integration
    // project shares a directory with `client`, so it is named explicitly.
    projects: ["apps/*", "packages/*", "packages/client/vitest.integration.config.ts"],
    coverage: {
      provider: "v8",
      include: ["apps/*/src/**/*.ts", "packages/*/src/**/*.ts"],
      /**
       * A ratchet toward the 80% in RULES.md, not the rule itself.
       *
       * Nothing enforced the rule until CI ran `pnpm coverage`, and by then the
       * suite stood at 65% of lines and 37% of branches. A gate that fails every
       * run is a gate nobody keeps, so these are what it had when CI began
       * checking, less a point of headroom. Raise them as coverage rises; never
       * lower them. New code is still held to the rule in review.
       */
      thresholds: {
        lines: 64,
        functions: 41,
        branches: 35,
        statements: 63,
      },
    },
  },
});
