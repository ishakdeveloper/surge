import { sendMessage } from "@/atom/chat-atoms.js";
import { platformAtom } from "@/atom/runtime.js";
import { fakePlatform, type FakeRequest } from "@/testing/fake-platform.js";
import { ConversationId } from "@surge/domain/api/Primitives";
import { AsyncResult, type Atom, AtomRegistry } from "effect/unstable/reactivity";
import { describe, expect, it } from "vitest";

/**
 * Sending, over the shared atoms and the fake platform both apps' tests use.
 * What matters is what goes out: the draft as written, with the key the
 * *caller* holds, so a retry from the screen is the same message.
 */
const settled = <A, E>(
  registry: AtomRegistry.AtomRegistry,
  atom: Atom.Atom<AsyncResult.AsyncResult<A, E>>,
) =>
  new Promise<AsyncResult.AsyncResult<A, E>>((resolve) => {
    registry.subscribe(atom, (result) => {
      if (!AsyncResult.isInitial(result) && !result.waiting) resolve(result);
    }, { immediate: true });
  });

const stored = {
  message: {
    id: "m-1",
    conversationId: "c1",
    seq: 1,
    senderId: "rider-1",
    senderRole: "PARTICIPANT_ROLE_RIDER",
    quickReply: "",
    body: "Coming out now",
    clientMessageId: "key-1",
    createdAt: "2026-09-11T12:00:00Z",
  },
};

describe("sending a message", () => {
  it("sends the draft with the caller's key, as the signed-in caller", async () => {
    const requests: Array<FakeRequest> = [];
    const registry = AtomRegistry.make({
      initialValues: [[
        platformAtom,
        fakePlatform({
          routes: [{ method: "POST", path: "/v1/conversations/c1/messages", body: stored }],
          onRequest: (request) => requests.push(request),
        }),
      ]],
    });

    const outcome = settled(registry, sendMessage);
    registry.set(sendMessage, {
      conversationId: ConversationId.make("c1"),
      draft: { body: "Coming out now", quickReply: "", clientMessageId: "key-1" },
    });

    const result = await outcome;
    expect(AsyncResult.isSuccess(result) && result.value.seq).toBe(1);
    expect(requests).toEqual([{
      method: "POST",
      path: "/v1/conversations/c1/messages",
      authorization: "Bearer fake-token",
      body: { body: "Coming out now", quickReply: "", clientMessageId: "key-1" },
    }]);
    registry.dispose();
  });
});
