import { describe, expect, it } from "@effect/vitest";
import { Sms } from "@surge/auth/sms/Sms";
import { ConfigProvider, Effect } from "effect";

/**
 * The contract, against Twilio with its test credentials — which accept a
 * message and send nothing, and answer Twilio's magic numbers with the errors a
 * real number would get.
 *
 * `Sms.test.ts` tests the mapping against a fake HTTP client; this is what holds
 * that fake honest. It runs only with the TWILIO_TEST_* credentials — never the
 * live ones beside them, so a test never texts anyone — and skips otherwise.
 */
const accountSid = process.env["TWILIO_TEST_ACCOUNT_SID"] ?? "";
const authToken = process.env["TWILIO_TEST_AUTH_TOKEN"] ?? "";

/** The one sender test credentials accept. */
const MAGIC_FROM = "+15005550006";

const send = (to: string) =>
  Effect.gen(function*() {
    const sms = yield* Sms;
    yield* sms.send({ to, text: "Your Surge code is 123456." });
  }).pipe(
    Effect.provide(Sms.layer),
    Effect.provideService(
      ConfigProvider.ConfigProvider,
      ConfigProvider.fromEnvRecord({
        TWILIO_ACCOUNT_SID: accountSid,
        TWILIO_AUTH_TOKEN: authToken,
        TWILIO_FROM: MAGIC_FROM,
      }),
    ),
  );

describe.skipIf(accountSid === "" || authToken === "")("Twilio, with test credentials", () => {
  it.effect("accepts a message to a number that can take one", () => send("+14108675310"));

  /** Twilio's magic numbers, and what each stands for. */
  const undeliverable = [
    ["+15005550001", "an invalid number"],
    ["+15005550002", "a number Twilio cannot route to"],
    ["+15005550004", "a number that has blocked the sender"],
    ["+15005550009", "a number that cannot receive texts"],
  ] as const;

  for (const [to, what] of undeliverable) {
    it.effect(`refuses ${what} as undeliverable, which the person can act on`, () =>
      Effect.gen(function*() {
        const failure = yield* Effect.flip(send(to));

        expect(failure.reason).toBe("Undeliverable");
      }));
  }
});
