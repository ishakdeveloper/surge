import { Config, Context, Effect, Layer, Option, Schema } from "effect";
import { FetchHttpClient, HttpClient, HttpClientRequest } from "effect/unstable/http";
import { outboxRecorder } from "../dev/DevOutbox.js";

/**
 * Text messages — phone sign-in codes, and nothing else.
 *
 * Twilio when an account is configured; the log when it is not, the way mail
 * goes without a Resend key — which is enough to sign in on a laptop or a
 * simulator, and what the dev outbox reads codes back from.
 */
export interface SmsMessage {
  /** E.164, as better-auth's phone-number plugin hands it over. */
  readonly to: string;
  readonly text: string;
}

/**
 * The only send failures a caller can do anything about.
 *
 * Everything else Twilio reports — credentials it rejects, a sender the account
 * does not own — is a misconfiguration, so it becomes a defect rather than a
 * typed error.
 */
export class SmsSendFailed extends Schema.TaggedError<SmsSendFailed>()("SmsSendFailed", {
  reason: Schema.Literals(["RateLimited", "Undeliverable", "Unavailable"]),
}) {}

export interface SmsService {
  readonly send: (message: SmsMessage) => Effect.Effect<void, SmsSendFailed>;
}

/**
 * Twilio's codes for a number that cannot take this text: invalid (21211), in a
 * country the account may not send to (21408), blocked by its owner (21610),
 * unroutable (21612), or unable to receive SMS (21614). The person can act on
 * each — with another number, or by signing in with email instead.
 */
const UNDELIVERABLE: ReadonlySet<number> = new Set([21211, 21408, 21610, 21612, 21614]);

/** Twilio's "too many requests", which it also sends as HTTP 429. */
const TOO_MANY_REQUESTS = 20429;

/** Twilio's error body. The code decides; the message only goes into a defect. */
const TwilioError = Schema.Struct({ code: Schema.Number, message: Schema.String });

const messagesUrl = (accountSid: string) =>
  `https://api.twilio.com/2010-04-01/Accounts/${encodeURIComponent(accountSid)}/Messages.json`;

export class Sms extends Context.Service<Sms, SmsService>()("Sms") {
  /**
   * Written to the log instead of sent. A development affordance, and it says
   * so, loudly, once per send.
   */
  static layerLogging: Layer.Layer<Sms> = Layer.effect(Sms)(
    Effect.gen(function*() {
      const record = yield* outboxRecorder;

      return {
        send: Effect.fn("Sms.send")(function*(message: SmsMessage) {
          yield* record(message.to, message.text);
          yield* Effect.logWarning(
            `TWILIO_ACCOUNT_SID is unset — not sending.\n\n${message.text}`,
          )
            .pipe(Effect.annotateLogs({ to: message.to }));
        }),
      };
    }),
  );

  /**
   * Twilio's Messages API, over HTTP rather than its SDK: one form POST is the
   * whole integration, and auth stays the small service it is meant to be.
   *
   * Nothing is recorded in the dev outbox — a code a provider delivers goes to
   * the phone, and only there.
   */
  static layerTwilio: Layer.Layer<Sms, never, HttpClient.HttpClient> = Layer.effect(Sms)(
    Effect.gen(function*() {
      const accountSid = yield* Config.nonEmptyString("TWILIO_ACCOUNT_SID");
      const authToken = yield* Config.redacted("TWILIO_AUTH_TOKEN");
      const from = yield* Config.nonEmptyString("TWILIO_FROM");
      const client = yield* HttpClient.HttpClient;

      // A Messaging Service picks a sender per country; a number is the sender.
      const sender = from.startsWith("MG") ? { MessagingServiceSid: from } : { From: from };

      const send = Effect.fn("Sms.send")(function*(message: SmsMessage) {
        const response = yield* client.execute(
          HttpClientRequest.post(messagesUrl(accountSid)).pipe(
            HttpClientRequest.basicAuth(accountSid, authToken),
            HttpClientRequest.bodyUrlParams({ To: message.to, ...sender, Body: message.text }),
          ),
        ).pipe(Effect.mapError(() => new SmsSendFailed({ reason: "Unavailable" })));

        if (response.status >= 200 && response.status < 300) return;

        const error = yield* response.json.pipe(
          Effect.flatMap(Schema.decodeUnknownEffect(TwilioError)),
          Effect.match({ onFailure: () => Option.none(), onSuccess: Option.some }),
        );
        const code = Option.match(error, { onNone: () => undefined, onSome: (body) => body.code });

        if (response.status === 429 || code === TOO_MANY_REQUESTS) {
          return yield* new SmsSendFailed({ reason: "RateLimited" });
        }
        if (code !== undefined && UNDELIVERABLE.has(code)) {
          return yield* new SmsSendFailed({ reason: "Undeliverable" });
        }
        if (response.status >= 500) {
          return yield* new SmsSendFailed({ reason: "Unavailable" });
        }
        return yield* Effect.die(
          new Error(
            `Twilio refused the message (${response.status}): ${
              Option.match(error, {
                onNone: () => "no error body",
                onSome: (body) => `${body.code} ${body.message}`,
              })
            }`,
          ),
        );
      });

      return { send };
    }),
  ).pipe(Layer.orDie);

  /**
   * Twilio when an account SID is configured, the log when it is not.
   *
   * Read as a plain string and tested here, as `Mailer.layer` reads its key:
   * `.env.example` ships it empty, and empty has to mean unset rather than a
   * configuration error.
   */
  static layer: Layer.Layer<Sms> = Layer.unwrap(
    Effect.gen(function*() {
      const configured = yield* Config.option(Config.string("TWILIO_ACCOUNT_SID"));

      return Option.isSome(configured) && configured.value.trim() !== ""
        ? Sms.layerTwilio.pipe(Layer.provide(FetchHttpClient.layer))
        : Sms.layerLogging;
    }).pipe(Effect.orDie),
  );
}
