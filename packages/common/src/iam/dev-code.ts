import type { Contact } from "@surge/domain/iam/Contact";
import { Effect, Option, Schema } from "effect";
import { Atom } from "effect/unstable/reactivity";

/** Where a code went, as the dev outbox files it: the address, or the number. */
export const addressOf = (to: Contact): string => to.kind === "email" ? to.email : to.phoneNumber;

/** What the dev outbox answers with. Only the code is read. */
const Sent = Schema.Struct({ code: Schema.NullOr(Schema.String) });

const none = () => Option.none<string>();
const nothing = Effect.sync(none);

/**
 * The code the auth service last "sent" to an address or number, read back from
 * its dev outbox — for a development build's code step, so a demo signs in by
 * phone with no SMS provider, and nobody has to read the auth service's log.
 *
 * The outbox exists only when auth runs with AUTH_DEV_OUTBOX=true, never in a
 * deployment, and each app reads this only in a development build. Anything
 * else — no outbox, nothing sent there yet, auth unreachable — is no code to
 * show, not an error to show.
 *
 * A family per auth service, because each app knows its address differently.
 */
export const devCodeFamily = (authUrl: string) =>
  Atom.family((address: string) =>
    Atom.make(
      Effect.tryPromise(async () => {
        const response = await fetch(`${authUrl}/dev/outbox?to=${encodeURIComponent(address)}`);
        return response.ok ? ((await response.json()) as unknown) : undefined;
      }).pipe(
        Effect.flatMap((body) =>
          body === undefined
            ? nothing
            : Schema.decodeUnknownEffect(Sent)(body).pipe(
              Effect.map((sent) => Option.fromNullishOr(sent.code)),
            )
        ),
        Effect.orElseSucceed(none),
      ),
    )
  );
