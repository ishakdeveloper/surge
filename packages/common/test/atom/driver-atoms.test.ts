import { followAtom } from "@/atom/driver-atoms.js";
import { platformAtom } from "@/atom/runtime.js";
import { fakePlatform } from "@/testing/fake-platform.js";
import { AsyncResult, AtomRegistry } from "effect/unstable/reactivity";
import { describe, expect, it } from "vitest";

/**
 * Following the device's position is something a driver turns on. Until they
 * do, there is nothing to report — and in particular no failure, which the
 * driver's screen shows beside the toggle as a tracking error.
 */
describe("following the device's position", () => {
  it("reports nothing, and no failure, until the driver asks for it", async () => {
    const registry = AtomRegistry.make({
      initialValues: [[platformAtom, fakePlatform({ routes: [] })]],
    });
    const unmount = registry.mount(followAtom);

    // Long enough for the runtime to build and an empty stream to have ended.
    await new Promise((resolve) => setTimeout(resolve, 50));

    expect(AsyncResult.isInitial(registry.get(followAtom))).toBe(true);
    unmount();
    registry.dispose();
  });
});
