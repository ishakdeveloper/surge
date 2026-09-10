import * as path from "node:path";
import { configDefaults, mergeConfig } from "vitest/config";
import shared from "../../vitest.shared.ts";

export default mergeConfig(shared, {
  test: {
    name: "client",
    // The tests that need the whole stack running are their own project —
    // `vitest.integration.config.ts` — so they run after this one rather than
    // alongside it.
    exclude: [...configDefaults.exclude, "test/**/*.integration.test.ts"],
    alias: {
      "@/": path.join(import.meta.dirname, "src") + "/",
      "@test/": path.join(import.meta.dirname, "test") + "/",
    },
  },
});
