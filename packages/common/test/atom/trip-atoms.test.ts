import { platformAtom } from "@/atom/runtime.js";
import { bookTrip } from "@/atom/trip-atoms.js";
import { fakePlatform, type FakeRequest } from "@/testing/fake-platform.js";
import { FareId } from "@surge/domain/api/Primitives";
import { AsyncResult, type Atom, AtomRegistry } from "effect/unstable/reactivity";
import * as fs from "node:fs";
import * as path from "node:path";
import { describe, expect, it } from "vitest";

/**
 * Booking, over the shared atoms and the fake platform both apps' tests use.
 *
 * The contract that matters is what goes out: the fare the rider chose, and
 * the idempotency key the *caller* holds — a key made inside the atom would be
 * a new key on every retry, which is a second ride rather than a retry. The
 * response is the one the running gateway sent for a real booking.
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
) as { readonly created: unknown; };

const settled = <A, E>(
  registry: AtomRegistry.AtomRegistry,
  atom: Atom.Atom<AsyncResult.AsyncResult<A, E>>,
) =>
  new Promise<AsyncResult.AsyncResult<A, E>>((resolve) => {
    registry.subscribe(atom, (result) => {
      if (!AsyncResult.isInitial(result) && !result.waiting) resolve(result);
    }, { immediate: true });
  });

describe("booking a trip", () => {
  it("sends the chosen fare with the caller's idempotency key, as the signed-in caller", async () => {
    const requests: Array<FakeRequest> = [];
    const registry = AtomRegistry.make({
      initialValues: [[
        platformAtom,
        fakePlatform({
          routes: [{ method: "POST", path: "/v1/trips", body: responses.created }],
          onRequest: (request) => requests.push(request),
        }),
      ]],
    });

    const outcome = settled(registry, bookTrip);
    registry.set(bookTrip, { fareId: FareId.make("fare-1"), idempotencyKey: "key-1" });

    expect(AsyncResult.isSuccess(await outcome)).toBe(true);
    expect(requests).toEqual([{
      method: "POST",
      path: "/v1/trips",
      authorization: "Bearer fake-token",
      body: { fareId: "fare-1", idempotencyKey: "key-1" },
    }]);
    registry.dispose();
  });
});
