import { describe, expect, it } from "@effect/vitest";
import { Sms, type SmsMessage } from "@surge/auth/sms/Sms";
import { Cause, ConfigProvider, Effect, Exit, Layer, Logger } from "effect";
import { HttpClient, HttpClientResponse } from "effect/unstable/http";

interface Sent {
  readonly url: string;
  readonly authorization: string | undefined;
  readonly form: Record<string, string>;
}

/**
 * Twilio, as far as the sender can tell: it answers with `status` and `body`,
 * and keeps each request. HTTP is the boundary — everything above it, the form
 * and the reading of Twilio's error codes, is the code under test.
 * `TwilioContract.test.ts` holds this stand-in to what Twilio really answers.
 */
const twilio = (answer: { readonly status: number; readonly body?: unknown; }, sent: Array<Sent>) =>
  Layer.succeed(HttpClient.HttpClient)(
    HttpClient.make((request, url) => {
      const text = request.body._tag === "Uint8Array"
        ? new TextDecoder().decode(request.body.body)
        : "";
      sent.push({
        url: url.href,
        authorization: request.headers["authorization"],
        form: Object.fromEntries(new URLSearchParams(text)),
      });
      return Effect.succeed(
        HttpClientResponse.fromWeb(
          request,
          new Response(answer.body === undefined ? null : JSON.stringify(answer.body), {
            status: answer.status,
            headers: { "content-type": "application/json" },
          }),
        ),
      );
    }),
  );

const withEnv = (env: Record<string, string>) =>
  Effect.provideService(
    ConfigProvider.ConfigProvider,
    ConfigProvider.fromEnvRecord(env, { preserveEmptyStrings: true }),
  );

const MESSAGE: SmsMessage = { to: "+31612345678", text: "Your Surge code is 123456." };

const ACCOUNT = {
  TWILIO_ACCOUNT_SID: "AC0123",
  TWILIO_AUTH_TOKEN: "token-0123",
  TWILIO_FROM: "+3197010000000",
};

const sendOne = Effect.gen(function*() {
  const sms = yield* Sms;
  yield* sms.send(MESSAGE);
});

const throughTwilio = (
  answer: { readonly status: number; readonly body?: unknown; },
  sent: Array<Sent> = [],
  env: Record<string, string> = ACCOUNT,
) =>
  sendOne.pipe(
    Effect.provide(Sms.layerTwilio.pipe(Layer.provide(twilio(answer, sent)))),
    withEnv(env),
  );

/** The reason a send failed with, for a failure the caller can act on. */
const reasonFor = (status: number, code?: number) =>
  Effect.flip(
    throughTwilio({ status, body: code === undefined ? undefined : { code, message: "refused" } }),
  ).pipe(Effect.map((failure) => failure.reason));

/** Collects what a send would have written, in place of the console. */
const captureInto = (lines: Array<string>) =>
  Logger.layer([Logger.make(({ message }) => lines.push(String(message)))]);

describe("texting a code through Twilio", () => {
  it.effect("posts it to the account's Messages resource, as the account", () =>
    Effect.gen(function*() {
      const sent: Array<Sent> = [];

      yield* throughTwilio({ status: 201, body: { sid: "SM0123" } }, sent);

      expect(sent).toEqual([{
        url: "https://api.twilio.com/2010-04-01/Accounts/AC0123/Messages.json",
        authorization: `Basic ${btoa("AC0123:token-0123")}`,
        form: { To: "+31612345678", From: "+3197010000000", Body: "Your Surge code is 123456." },
      }]);
    }));

  it.effect("sends through a Messaging Service when TWILIO_FROM names one", () =>
    Effect.gen(function*() {
      const sent: Array<Sent> = [];

      yield* throughTwilio({ status: 201, body: { sid: "SM0123" } }, sent, {
        ...ACCOUNT,
        TWILIO_FROM: "MG0123",
      });

      expect(sent[0]?.form).toEqual({
        To: "+31612345678",
        MessagingServiceSid: "MG0123",
        Body: "Your Surge code is 123456.",
      });
    }));

  it.effect("reads a number that cannot take a text as undeliverable", () =>
    Effect.gen(function*() {
      for (const code of [21211, 21408, 21610, 21612, 21614]) {
        expect(yield* reasonFor(400, code)).toBe("Undeliverable");
      }
    }));

  it.effect("reads too many requests as rate limited, by status or by code", () =>
    Effect.gen(function*() {
      expect(yield* reasonFor(429)).toBe("RateLimited");
      expect(yield* reasonFor(400, 20429)).toBe("RateLimited");
    }));

  it.effect("reads Twilio being down as unavailable", () =>
    Effect.gen(function*() {
      expect(yield* reasonFor(503)).toBe("Unavailable");
    }));

  /** Credentials Twilio rejects are ours to fix, not the person's. */
  it.effect("dies on a misconfiguration rather than blaming the number", () =>
    Effect.gen(function*() {
      const exit = yield* Effect.exit(
        throughTwilio({ status: 401, body: { code: 20003, message: "Authenticate" } }),
      );

      expect(Exit.isFailure(exit) && Cause.hasDies(exit.cause)).toBe(true);
    }));
});

describe("texting a code with no provider", () => {
  /**
   * A fresh clone has no Twilio account, and phone sign-in goes through a text
   * — so a sender that refused to build without one would leave it unable to
   * sign anyone in by phone.
   */
  it.effect("logs the text instead of sending it, when the account SID is empty", () =>
    Effect.gen(function*() {
      const lines: Array<string> = [];

      yield* sendOne.pipe(
        Effect.provide(Sms.layer),
        Effect.provide(captureInto(lines)),
        withEnv({ TWILIO_ACCOUNT_SID: "" }),
      );

      const written = lines.join("\n");
      expect(written).toContain("TWILIO_ACCOUNT_SID is unset");
      // The whole point: the code is readable from the log.
      expect(written).toContain("123456");
    }));
});
