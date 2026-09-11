import * as path from "node:path";
import { mergeConfig } from "vitest/config";
import shared from "../../vitest.shared.ts";

export default mergeConfig(shared, {
  test: {
    name: "common",
    alias: {
      "@/": path.join(import.meta.dirname, "src") + "/",
    },
  },
});
