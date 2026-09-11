import { platformAtom, PlatformNotProvided } from "@/atom/runtime.js";
import { activeTripAtom } from "@/atom/trip-atoms.js";
import { fakePlatform } from "@/testing/fake-platform.js";
import { Cause, Option } from "effect";
import { AsyncResult, type Atom, AtomRegistry } from "effect/unstable/reactivity";
import * as fs from "node:fs";
import * as path from "node:path";
import { describe, expect, it } from "vitest";

/**
 * The seam both apps depend on: shared atoms, one app's platform.
 *
 * Only the true boundary is fake — `fakePlatform`, the same one the mobile
 * screen tests use — and the atoms, the runtime and the generated client are
 * production code. The response is one the running gateway actually sent,
 * captured by `pnpm capture:api-fixtures`.
 */
const responses = JSON.parse(
  fs.readFileSync(
    path.join(
      import.meta.dirname,
      "..",
      "..",
      "..",
      "domain",
      "test",
      "api",
      "testdata",
      "responses.json",
    ),
    "utf8",
  ),
) as { readonly listed: unknown; };

/** Resolves once the atom has an answer that is not still loading. */
const settled = <A, E>(
  registry: AtomRegistry.AtomRegistry,
  atom: Atom.Atom<AsyncResult.AsyncResult<A, E>>,
) =>
  new Promise<AsyncResult.AsyncResult<A, E>>((resolve) => {
    registry.subscribe(atom, (result) => {
      if (!AsyncResult.isInitial(result) && !result.waiting) resolve(result);
    }, { immediate: true });
  });

describe("the shared runtime", () => {
  it("runs the shared atoms over whichever platform the registry was given", async () => {
    const authorizations: Array<string | undefined> = [];
    const platform = fakePlatform({
      routes: [{ method: "GET", path: "/v1/trips", body: responses.listed }],
      onRequest: (request) => authorizations.push(request.authorization),
    });
    const registry = AtomRegistry.make({ initialValues: [[platformAtom, platform]] });

    const active = await settled(registry, activeTripAtom);

    // The captured list holds one trip still waiting for a driver.
    expect(AsyncResult.isSuccess(active) && Option.isSome(active.value)).toBe(true);
    // Through the app's HTTP client, carrying the app's token.
    expect(authorizations).toEqual(["Bearer fake-token"]);
    registry.dispose();
  });

  it("fails loudly when an app forgets to give one", async () => {
    const registry = AtomRegistry.make();

    const active = await settled(registry, activeTripAtom);

    expect(AsyncResult.isFailure(active)).toBe(true);
    if (AsyncResult.isFailure(active)) {
      expect(Cause.pretty(active.cause)).toContain(PlatformNotProvided.name);
    }
    registry.dispose();
  });
});
