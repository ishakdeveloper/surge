import { addressOf, devCodeFamily } from "@/iam/dev-code.js";
import { Option } from "effect";
import { AsyncResult, type Atom, AtomRegistry } from "effect/unstable/reactivity";
import { afterEach, describe, expect, it, vi } from "vitest";

/** The settled value, ignoring when it settled — a result carries its timestamp. */
const settled = <A, E>(
  registry: AtomRegistry.AtomRegistry,
  atom: Atom.Atom<AsyncResult.AsyncResult<A, E>>,
) =>
  new Promise<AsyncResult.AsyncResult<A, E>>((resolve) => {
    registry.subscribe(atom, (result) => {
      if (!AsyncResult.isInitial(result) && !result.waiting) resolve(result);
    }, { immediate: true });
  });

/** The outbox, as far as the atom can tell: `fetch` is the boundary. */
const outboxAnswers = (answer: () => Promise<Response>) => {
  const fetch = vi.fn(answer);
  vi.stubGlobal("fetch", fetch);
  return fetch;
};

/** The code shown for `address`, or undefined if the atom did not settle on one. */
const codeFor = async (address: string) => {
  const registry = AtomRegistry.make();
  const result = await settled(registry, devCodeFamily("http://auth.test")(address));
  registry.dispose();
  return AsyncResult.isSuccess(result) ? result.value : undefined;
};

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("the code a development build shows", () => {
  it("is the one the outbox holds for the address", async () => {
    const fetch = outboxAnswers(async () =>
      new Response(JSON.stringify({ to: "+31612345678", code: "123456", text: "…", atMs: 1 }))
    );

    expect(await codeFor("+31612345678")).toEqual(Option.some("123456"));
    expect(fetch).toHaveBeenCalledWith("http://auth.test/dev/outbox?to=%2B31612345678");
  });

  it("is nothing when nothing was sent there, or there is no outbox", async () => {
    outboxAnswers(async () => new Response("Nothing has been sent there.", { status: 404 }));

    expect(await codeFor("ada@example.com")).toEqual(Option.none());
  });

  it("is nothing, not an error, when the auth service cannot be reached", async () => {
    outboxAnswers(() => Promise.reject(new TypeError("fetch failed")));

    expect(await codeFor("ada@example.com")).toEqual(Option.none());
  });

  it("files a code under the address or the number it went to", () => {
    expect(addressOf({ kind: "email", email: "ada@example.com" })).toBe("ada@example.com");
    expect(addressOf({ kind: "phone", phoneNumber: "+31612345678" })).toBe("+31612345678");
  });
});
