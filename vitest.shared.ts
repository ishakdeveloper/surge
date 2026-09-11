import * as path from "node:path";
import type { ViteUserConfig } from "vitest/config";

/**
 * Mirrors the `@surge/*` mappings in tsconfig.base.json so runtime
 * resolution matches what the compiler sees.
 */
const workspaceAliases = {
  "@surge/client/": path.join(import.meta.dirname, "packages", "client", "src") + "/",
  "@surge/common/": path.join(import.meta.dirname, "packages", "common", "src") + "/",
  "@surge/domain/": path.join(import.meta.dirname, "packages", "domain", "src") + "/",
};

const config: ViteUserConfig = {
  /**
   * The workspace packages expose source under a `development` condition and
   * built JavaScript otherwise. Vitest compiles TypeScript, so it wants the
   * source — without this it resolves `build/`, which need not exist.
   *
   * The aliases above predate this and still short-circuit those
   * packages; the condition is what covers `@surge/database`.
   */
  resolve: { conditions: ["development"] },
  test: {
    setupFiles: [path.join(import.meta.dirname, "setupTests.ts")],
    fakeTimers: {
      toFake: undefined,
    },
    sequence: {
      concurrent: true,
    },
    // Vitest 4 moved `pool`/`isolate` to the top level; `poolOptions` is gone.
    pool: "threads",
    isolate: false,
    slowTestThreshold: 5_000,
    testTimeout: 30_000,
    include: ["test/**/*.test.ts", "src/**/*.test.ts"],
    alias: { ...workspaceAliases },
  },
};

export default config;
