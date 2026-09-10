import * as path from "node:path";
import { mergeConfig } from "vitest/config";
import shared from "../../vitest.shared.ts";

/**
 * The tests that need the whole stack — auth, gateway, trip, ingest, matcher —
 * and skip when it is not running.
 *
 * Their own project, in the last group, one file at a time. Not for tidiness:
 * run alongside the rest of the suite on one laptop, the CPU burst of every
 * other project starves the single-core Redpanda badly enough (reactor stalls
 * of three seconds, measured) that the matcher loses its consumer-group session
 * and every partition with it. A test that books a ride and waits for a driver
 * then fails for a reason that has nothing to do with the code under test, and
 * a suite that fails that way two runs in three teaches people to ignore it.
 *
 * Ordered after everything, so `pnpm test` still runs all of it.
 */
const config = mergeConfig(shared, {
  test: {
    name: "client-integration",
    alias: {
      "@/": path.join(import.meta.dirname, "src") + "/",
      "@test/": path.join(import.meta.dirname, "test") + "/",
    },
    sequence: { concurrent: false, groupOrder: 3 },
    fileParallelism: false,
    // Each test bounds its own waits and names what it was waiting for; this
    // is only the backstop.
    testTimeout: 180_000,
  },
});

// Assigned rather than merged: `mergeConfig` concatenates arrays, and merging
// this into the shared `include` would run every unit test a second time here.
config.test!.include = ["test/**/*.integration.test.ts"];

export default config;
