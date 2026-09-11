import { EmailSendFailed } from "@surge/auth/email/Mailer";
import { deliver } from "@surge/auth/iam/Delivery";
import { makeAuth } from "@surge/auth/iam/Options";
import { SmsSendFailed } from "@surge/auth/sms/Sms";
import { testDbUrl } from "@surge/database/PgTest";
import { Effect } from "effect";
import { randomInt, randomUUID } from "node:crypto";
import { Pool } from "pg";
import { afterAll, beforeEach, describe, expect, it } from "vitest";

/**
 * What asking for a code answers when the code cannot be sent — through the real
 * better-auth, because what matters is the status it gives the apps, and that is
 * decided in its request handling rather than in `deliver`.
 *
 * The senders are the boundary: they fail with whatever the provider would have
 * said, and go through `deliver` exactly as `Auth.ts` wires them.
 */
const WEB = "http://localhost:5273";
const url = testDbUrl();

let failure: EmailSendFailed | SmsSendFailed | undefined;
const sendOrFail = () => deliver(failure === undefined ? Effect.void : Effect.fail(failure));

const pool = url === undefined ? undefined : new Pool({ connectionString: url });
const auth = pool === undefined ? undefined : makeAuth({
  pool,
  baseURL: "http://localhost:3200",
  secret: "a-test-secret-that-is-comfortably-long-enough",
  trustedOrigins: [WEB],
  google: undefined,
  cookieDomain: undefined,
  sendEmail: sendOrFail,
  sendSms: sendOrFail,
  smsDomain: "localhost:5273",
});

afterAll(async () => {
  await pool?.end();
});

beforeEach(() => {
  failure = undefined;
});

const post = (path: string, body: object) =>
  auth!.handler(
    new Request(`http://localhost:3200/api/auth${path}`, {
      method: "POST",
      headers: { "content-type": "application/json", origin: WEB },
      body: JSON.stringify(body),
    }),
  );

const textACode = () =>
  post("/phone-number/send-otp", {
    phoneNumber: `+316${String(randomInt(10_000_000, 99_999_999))}`,
  });

const emailACode = () =>
  post("/email-otp/send-verification-otp", {
    email: `someone-${randomUUID()}@example.com`,
    type: "sign-in",
  });

describe.skipIf(auth === undefined)("asking for a code that cannot be sent", () => {
  it("answers 200 when the code went out", async () => {
    expect((await textACode()).status).toBe(200);
    expect((await emailACode()).status).toBe(200);
  });

  it("answers a number that cannot take a text with a 400, not a 500", async () => {
    failure = new SmsSendFailed({ reason: "Undeliverable" });

    expect((await textACode()).status).toBe(400);
  });

  /**
   * better-auth awaits the email sender but catches what it throws, and logs it
   * — so the answer is the same whether a code went out or not, and asking for
   * codes cannot be used to learn which addresses a provider accepts. Only the
   * phone route lets a refusal through.
   */
  it("answers 200 for an address the provider refuses, saying nothing about it", async () => {
    failure = new EmailSendFailed({ reason: "InvalidRecipient" });

    expect((await emailACode()).status).toBe(200);
  });

  it("answers too many codes with a 429", async () => {
    failure = new SmsSendFailed({ reason: "RateLimited" });

    expect((await textACode()).status).toBe(429);
  });

  it("answers a provider outage with a 503, which the apps word as ours", async () => {
    failure = new SmsSendFailed({ reason: "Unavailable" });

    expect((await textACode()).status).toBe(503);
  });
});
