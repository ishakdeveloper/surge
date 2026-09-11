import * as path from "node:path";
import { mergeConfig } from "vitest/config";
import shared from "../../vitest.shared.ts";

/**
 * Only what runs without a device. Anything importing `react-native` or an Expo
 * module needs Hermes and the native side, so the logic worth testing lives in
 * plain modules — `lib/`, `drive/driver-status.ts` — and is tested here.
 *
 * `.mts` because this package is CommonJS, which is what Metro, Babel and
 * Tailwind's configs expect, and a Vitest config wants ESM.
 */
export default mergeConfig(shared, {
  test: {
    name: "mobile",
    alias: {
      "@/": path.join(import.meta.dirname, "src") + "/",
    },
    include: ["test/**/*.test.ts"],
  },
});
