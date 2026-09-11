import { PgPool } from "@surge/database/PgPool";
import { Config, Context, Effect, Layer, Option, Redacted, Schema } from "effect";
import type { EmailMessage } from "../email/Mailer.js";
import { Mailer } from "../email/Mailer.js";
import { Sms, type SmsMessage } from "../sms/Sms.js";
import { deliver } from "./Delivery.js";
import type { AuthInstance } from "./Options.js";
import { makeAuth } from "./Options.js";

/**
 * better-auth, behind the only seam in the codebase that leaves the Effect
 * runtime.
 *
 * better-auth invokes its `sendX` callbacks from its own promise chain, so an
 * Effect has to be run to completion inside them. Because `Mailer` and `Sms`
 * are resolved to values here, their `send` carries no remaining requirements,
 * and `deliver` runs it — answering a code that cannot be sent with a status the
 * apps can word, rather than letting every failure become a 500.
 */
export class Auth extends Context.Service<Auth, AuthInstance>()("Auth") {
  static layer: Layer.Layer<Auth, never, PgPool | Mailer | Sms> = Layer.effect(Auth)(
    Effect.gen(function*() {
      const pool = yield* PgPool;
      const mailer = yield* Mailer;
      const sms = yield* Sms;

      const baseURL = yield* Config.nonEmptyString("AUTH_BASE_URL").pipe(
        Config.withDefault("http://localhost:3000"),
      );
      const secret = yield* Config.redacted("AUTH_SECRET");
      const webUrl = yield* Config.url("WEB_URL").pipe(
        Config.withDefault(new URL("http://localhost:5273")),
      );

      /**
       * The native app's origins. `surge://` is its own scheme; Expo Go, which
       * is how the app runs in development, sends `exp://` followed by the dev
       * server's address. Origins that are not http match by prefix, so the
       * scheme alone covers every address.
       *
       * Configured rather than fixed so `exp://` can stay out of production.
       * The check is a browser defence — a native client can send any origin it
       * likes — but a list that says what is expected is worth keeping exact.
       */
      const mobileOrigins = yield* Config.schema(
        Config.Array(Schema.String),
        "AUTH_MOBILE_ORIGINS",
      ).pipe(Config.withDefault(["surge://"]));

      /**
       * Set only when the web app and the API are on different subdomains of one
       * parent — `.example.com`. Left unset they share a host and better-auth's
       * default is correct.
       */
      const cookieDomain = yield* Config.option(Config.nonEmptyString("AUTH_COOKIE_DOMAIN"));

      const google = yield* Config.all({
        clientId: Config.nonEmptyString("GOOGLE_CLIENT_ID"),
        clientSecret: Config.nonEmptyString("GOOGLE_CLIENT_SECRET"),
      }).pipe(Config.option);

      const sendEmail = (message: EmailMessage): Promise<void> => deliver(mailer.send(message));

      const sendSms = (message: SmsMessage): Promise<void> => deliver(sms.send(message));

      return makeAuth({
        pool,
        baseURL,
        secret: Redacted.value(secret),
        trustedOrigins: [webUrl.origin, ...mobileOrigins],
        cookieDomain: Option.getOrUndefined(cookieDomain),
        google: Option.getOrUndefined(google),
        sendEmail,
        sendSms,
        smsDomain: webUrl.host,
      });
    }),
  ).pipe(Layer.orDie);
}
