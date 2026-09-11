import { expo } from "@better-auth/expo";
import { phoneAccountEmail } from "@surge/domain/iam/Contact";
import { betterAuth } from "better-auth";
import { APIError, createAuthMiddleware } from "better-auth/api";
import { getMigrations } from "better-auth/db/migration";
import { emailOTP, phoneNumber } from "better-auth/plugins";
import { jwt } from "better-auth/plugins/jwt";
import type * as Pg from "pg";
import type { EmailMessage } from "../email/Mailer.js";
import * as Templates from "../email/Templates.js";
import type { SmsMessage } from "../sms/Sms.js";
import { changesRole, clampRole, DEFAULT_ROLE } from "./Roles.js";

/**
 * Everything better-auth needs, resolved before it is constructed.
 *
 * This module imports nothing from `effect` on purpose: the Effect boundary
 * lives in `Auth.ts`, and the senders arrive already bound to a runtime.
 */
export interface MakeAuthOptions {
  readonly pool: Pg.Pool;
  readonly baseURL: string;
  readonly secret: string;
  readonly trustedOrigins: ReadonlyArray<string>;
  readonly google: { readonly clientId: string; readonly clientSecret: string; } | undefined;
  /**
   * The parent domain the session cookie is scoped to, when the web app and the
   * auth service are on different subdomains of it — `.example.com` for
   * `app.example.com` and `auth.example.com`.
   */
  readonly cookieDomain: string | undefined;
  readonly sendEmail: (message: EmailMessage) => Promise<void>;
  readonly sendSms: (message: SmsMessage) => Promise<void>;
  /**
   * The web app's host, for the `@host #code` line that lets iOS and Android
   * offer an SMS code for autofill.
   */
  readonly smsDomain: string;
}

/** What a verified session carries. Narrower than better-auth's own shape. */
export interface AuthSession {
  readonly user: {
    readonly id: string;
    readonly email: string;
    readonly emailVerified: boolean;
    readonly role: string;
  };
  readonly session: {
    readonly id: string;
  };
}

/**
 * The only surface the rest of the service sees.
 *
 * Deliberately narrow: better-auth's inferred types are not portable across a
 * `declaration: true` build — they reach into zod internals that pnpm's store
 * layout makes unnameable — and letting them escape would leak its whole type
 * surface into every consumer. `authOptions` stays unexported for that reason.
 */
export interface AuthInstance {
  readonly handler: (request: Request) => Promise<Response>;
  readonly getSession: (headers: Record<string, string>) => Promise<AuthSession | null>;
}

/**
 * How long a code lives, and how many guesses it gets.
 *
 * Six digits is about twenty bits, so the attempt limit is what makes a code
 * safe, together with the rate limit in `AuthHttp.ts`. Three wrong answers
 * burn the code; five minutes is long enough to switch to a mail app and back.
 */
const CODE = { otpLength: 6, expiresIn: 300, allowedAttempts: 3 } as const;

/** E.164: a plus, a country code, and at most fifteen digits in all. */
const E164 = /^\+[1-9]\d{6,14}$/;

/** Single source of truth for the runtime instance and for schema generation. */
const authOptions = (options: MakeAuthOptions) => ({
  // The shared pool, so the process keeps one connection pool rather than two.
  database: options.pool,
  baseURL: options.baseURL,
  secret: options.secret,
  trustedOrigins: [...options.trustedOrigins],

  ...(options.cookieDomain === undefined ? {} : {
    advanced: {
      crossSubDomainCookies: { enabled: true, domain: options.cookieDomain },
    },
  }),

  user: {
    additionalFields: {
      /**
       * Which surface this account is for — `rider`, `driver` or `ops`.
       *
       * Accepted as input because a driver has to be able to sign up as one,
       * and clamped at both doors in `databaseHooks` below — see `Roles.ts`.
       */
      role: { type: "string" as const, required: false, input: true, defaultValue: DEFAULT_ROLE },
    },
  },

  /**
   * No passwords. Every account signs in with a one-time code sent to an email
   * address or a phone number, and an account is made the first time a code is
   * verified — so there is no separate sign-up, no reset, and no address to
   * confirm afterwards: entering the code is the confirmation.
   */
  emailAndPassword: { enabled: false },

  socialProviders: options.google === undefined ? {} : { google: options.google },

  hooks: {
    /**
     * The front door for role changes: `/update-user` parses every
     * `input: true` field, and `role` is one, so a signed-in rider could
     * otherwise ask to be `ops`. Refused with a 400 before better-auth parses
     * anything; the database hook below is the backstop for any other path to
     * the same write.
     */
    before: createAuthMiddleware(async (ctx) => {
      const body: unknown = ctx.body;
      if (
        ctx.path === "/update-user" && typeof body === "object" && body !== null
        && changesRole(body as Record<string, unknown>)
      ) {
        throw new APIError("BAD_REQUEST", {
          message: "A role is chosen once, when the account is made.",
        });
      }
    }),
  },

  databaseHooks: {
    user: {
      create: {
        /**
         * The clamp at creation. `role` is client-supplied input, so anything
         * outside the self-assignable set — `ops` above all — becomes the
         * default before the row is written.
         */
        before: async (user: Record<string, unknown>) => ({
          data: { ...user, role: clampRole(user["role"]) },
        }),
      },
      update: {
        /**
         * And at every write: an update that touches the role is cancelled.
         * Returning the change without `role` would not do — better-auth merges
         * a hook's result into the original, so `false` is the only answer that
         * holds. A role is chosen once, at creation, and changed only by an
         * operator against the database.
         */
        before: async (user: Record<string, unknown>) => (changesRole(user) ? false : undefined),
      },
    },
  },

  plugins: [
    /**
     * The native app. A phone has no web origin, so better-auth's Expo client
     * sends its scheme as `expo-origin` and this promotes it to `Origin` for the
     * CSRF check, which then consults `trustedOrigins` like any other.
     */
    expo(),
    /**
     * The bridge to Go.
     *
     * Every Go service verifies these locally against `/api/auth/jwks`, so no
     * request path touches this process or its session table. The payload is
     * the `Identity` contract in `@surge/domain/iam/Identity` — the field names
     * here and the struct in `services/pkg/authz` are one contract in two
     * languages, and they move together.
     *
     * EdDSA/Ed25519 by default, which is what the Go verifier expects.
     */
    jwt({
      jwt: {
        issuer: options.baseURL,
        audience: "surge",
        // Short, because nothing revokes a JWT. The clients re-fetch from
        // /api/auth/token, which does consult the session and so does revoke.
        expirationTime: "15m",
        definePayload: ({ user }) => ({
          userId: user.id,
          email: user.email,
          emailVerified: user.emailVerified,
          role: typeof user["role"] === "string" ? user["role"] : DEFAULT_ROLE,
        }),
      },
    }),
    /**
     * Email codes. `sign-in` verifies an existing account or makes a new one,
     * taking `role` from the same request.
     */
    emailOTP({
      ...CODE,
      sendVerificationOTP: async ({ email, otp }) => {
        await options.sendEmail({ to: email, ...Templates.emailOtp(otp) });
      },
    }),
    /**
     * Phone codes, the same way: verifying a number nobody has used makes the
     * account, with `role` from the request and a placeholder email from
     * `@surge/domain/iam/Contact`, because better-auth's user needs one.
     */
    phoneNumber({
      ...CODE,
      phoneNumberValidator: (candidate) => E164.test(candidate),
      sendOTP: async ({ phoneNumber: to, code }) => {
        await options.sendSms({ to, text: Templates.smsOtp(code, options.smsDomain) });
      },
      signUpOnVerification: {
        getTempEmail: phoneAccountEmail,
        getTempName: (number) => number,
      },
    }),
  ],
});

export const makeAuth = (options: MakeAuthOptions): AuthInstance => {
  const auth = betterAuth(authOptions(options));

  return {
    handler: (request) => auth.handler(request),

    // Effect models headers as a string record; better-auth wants web Headers.
    getSession: async (headers) => {
      const session = await auth.api.getSession({ headers: new Headers(headers) });
      if (session === null) return null;

      const role = (session.user as Record<string, unknown>)["role"];

      return {
        user: {
          id: session.user.id,
          email: session.user.email,
          emailVerified: session.user.emailVerified,
          role: typeof role === "string" ? role : DEFAULT_ROLE,
        },
        session: { id: session.session.id },
      };
    },
  };
};

/**
 * Emits the DDL better-auth needs for this exact plugin set against the
 * database the pool points at, so a committed migration is generated rather
 * than guessed.
 */
export const compileAuthMigrations = async (options: MakeAuthOptions): Promise<string> => {
  const { compileMigrations } = await getMigrations(authOptions(options));

  return compileMigrations();
};
