import * as path from "node:path";
import { mergeConfig } from "vitest/config";
import shared from "../../vitest.shared.ts";

export default mergeConfig(shared, {
  test: {
    name: "database",
    alias: {
      "@/": path.join(import.meta.dirname, "src") + "/",
      "@test/": path.join(import.meta.dirname, "test") + "/",
    },
    // Serial, and ahead of the auth project, because both share one database.
    // Migrations.test.ts applies the whole set — twice, deliberately, to hold
    // idempotency honest — so anything reading that database concurrently sees
    // the schema mid-flight.
    //
    // The group is 1 rather than 0 so the parallel projects keep the default:
    // Vitest refuses to put two projects in one group when their worker counts
    // differ, and running one worker is what serial means.
    sequence: { concurrent: false, groupOrder: 1 },
    fileParallelism: false,
  },
});
