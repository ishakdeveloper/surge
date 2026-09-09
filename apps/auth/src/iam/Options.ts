import { betterAuth } from "better-auth";
import { getMigrations } from "better-auth/db/migration";
import { emailOTP, magicLink } from "better-auth/plugins";
import { jwt } from "better-auth/plugins/jwt";
import type * as Pg from "pg";
import type { EmailMessage } from "../email/Mailer.js";
import * as Templates from "../email/Templates.js";

/**
 * Everything better-auth needs, resolved before it is constructed.
 *
 * This module imports nothing from `effect` on purpose: the Effect boundary
 * lives in `Auth.ts`, and `sendEmail` arrives already bound to a runtime.
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
}

/** The roles a caller may assign themselves. `ops` is granted, never claimed. */
const SELF_ASSIGNABLE_ROLES = new Set(["rider", "driver"]);

const DEFAULT_ROLE = "rider";

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
       * Which surface this account signed up for — `rider`, `driver` or `ops`.
       *
       * Accepted as sign-up input because a driver has to be able to register
       * as one, and clamped below: an account cannot make itself `ops` by
       * asking. Promotion is an operator action against the database.
       */
      role: { type: "string" as const, required: false, input: true, defaultValue: DEFAULT_ROLE },
    },
  },

  emailAndPassword: {
    enabled: true,
    sendResetPassword: async ({ user, url }: { user: { email: string; }; url: string; }) => {
      await options.sendEmail({ to: user.email, ...Templates.resetPassword(url) });
    },
  },

  emailVerification: {
    sendOnSignUp: true,
    autoSignInAfterVerification: true,
    sendVerificationEmail: async ({ user, url }: { user: { email: string; }; url: string; }) => {
      await options.sendEmail({ to: user.email, ...Templates.verifyEmail(url) });
    },
  },

  socialProviders: options.google === undefined ? {} : { google: options.google },

  databaseHooks: {
    user: {
      create: {
        /**
         * The clamp. `role` is client-supplied input, so anything outside the
         * self-assignable set — `ops` above all — becomes the default before
         * the row is written, rather than being trusted and audited later.
         */
        before: async (user: Record<string, unknown>) => {
          const requested = user["role"];
          const role = typeof requested === "string" && SELF_ASSIGNABLE_ROLES.has(requested)
            ? requested
            : DEFAULT_ROLE;

          return { data: { ...user, role } };
        },
      },
    },
  },

  plugins: [
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
        // Short, because nothing revokes a JWT. The browser re-fetches from
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
    magicLink({
      sendMagicLink: async ({ email, url }) => {
        await options.sendEmail({ to: email, ...Templates.magicLink(url) });
      },
    }),
    emailOTP({
      sendVerificationOTP: async ({ email, otp }) => {
        await options.sendEmail({ to: email, ...Templates.emailOtp(otp) });
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
 * Emits the DDL better-auth needs for this exact plugin set, so the committed
 * migration is generated rather than guessed.
 */
export const compileAuthMigrations = async (options: MakeAuthOptions): Promise<string> => {
  const { compileMigrations } = await getMigrations(authOptions(options));

  return compileMigrations();
};
