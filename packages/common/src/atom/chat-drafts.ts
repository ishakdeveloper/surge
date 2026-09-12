import type { ConversationId } from "@surge/domain/api/Primitives";
import { Atom } from "effect/unstable/reactivity";

/**
 * What someone has typed in a conversation and not sent.
 *
 * Kept alive for the session, one per conversation, so leaving a chat and
 * coming back finds the words where they were. That is also why the composer
 * needs no leave-the-page guard: nothing typed there is lost by leaving.
 *
 * Wrapped in an object, because `Atom.make` reads anything iterable — a bare
 * string included — as an Effect.
 */
export interface Draft {
  readonly text: string;
}

export const draftAtom = Atom.family((_conversationId: ConversationId) =>
  Atom.keepAlive(Atom.make<Draft>({ text: "" }))
);
