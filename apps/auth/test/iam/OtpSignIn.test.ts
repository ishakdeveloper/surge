import { makeAuth } from "@surge/auth/iam/Options";
import { testDbUrl } from "@surge/database/PgTest";
import { contactOf } from "@surge/domain/iam/Contact";
import { randomInt, randomUUID } from "node:crypto";
import { Pool } from "pg";
import { afterAll, describe, expect, it } from "vitest";

/**
 * Signing in with a code, through the real better-auth against a migrated
 * database — the only place the plugin set, the role clamps and the phone
 * placeholder email are exercised together.
 *
 * The senders are the true boundary, so they are the only fake: they keep what
 * would have been sent, and the test reads the code back out, as a person
 * would read it off their phone.
 */
const WEB = "http://localhost:5273";
const url = testDbUrl();

const sent: Array<{ readonly to: string; readonly text: string; }> = [];
const pool = url === undefined ? undefined : new Pool({ connectionString: url });
const auth = pool === undefined ? undefined : makeAuth({
  pool,
  baseURL: "http://localhost:3200",
  secret: "a-test-secret-that-is-comfortably-long-enough",
  trustedOrigins: [WEB],
  google: undefined,
  cookieDomain: undefined,
  sendEmail: async (message) => {
    sent.push({ to: message.to, text: message.text });
  },
  sendSms: async (message) => {
    sent.push(message);
  },
  smsDomain: "localhost:5273",
});

afterAll(async () => {
  await pool?.end();
});

const post = (path: string, body: object, cookie?: string) =>
  auth!.handler(
    new Request(`http://localhost:3200/api/auth${path}`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        origin: WEB,
        ...(cookie === undefined ? {} : { cookie }),
      },
      body: JSON.stringify(body),
    }),
  );

/** The newest six-digit code sent to `to`. */
const codeFor = (to: string) => {
  const message = [...sent].reverse().find((entry) => entry.to === to);
  const code = message?.text.match(/\b(\d{6})\b/)?.[1];
  expect(code, `no code was sent to ${to}`).toBeDefined();
  return code ?? "";
};

const cookieOf = (response: Response) =>
  response.headers.getSetCookie().map((cookie) => cookie.split(";")[0]).join("; ");

const roleOf = async (cookie: string) => (await auth!.getSession({ cookie }))?.user.role;

describe.skipIf(auth === undefined)("signing in with a code", () => {
  it("makes a driver from an email code, who cannot then make themselves ops", async () => {
    const email = `driver-${randomUUID()}@example.com`;

    expect((await post("/email-otp/send-verification-otp", { email, type: "sign-in" })).status)
      .toBe(200);
    const signedIn = await post("/sign-in/email-otp", {
      email,
      otp: codeFor(email),
      role: "driver",
    });
    expect(signedIn.status).toBe(200);

    const cookie = cookieOf(signedIn);
    expect(await roleOf(cookie)).toBe("driver");

    // The escalation this closes: `/update-user` parses every `input: true`
    // field, and `role` is one. Whatever it answers, the role must not move.
    const escalation = await post("/update-user", { role: "ops" }, cookie);
    expect(escalation.status).toBe(400);
    expect(await roleOf(cookie)).toBe("driver");
  });

  it("makes a driver from a phone code, with a placeholder email", async () => {
    const phoneNumber = `+316${String(randomInt(10_000_000, 99_999_999))}`;

    expect((await post("/phone-number/send-otp", { phoneNumber })).status).toBe(200);
    const verified = await post("/phone-number/verify", {
      phoneNumber,
      code: codeFor(phoneNumber),
      role: "driver",
    });
    expect(verified.status).toBe(200);

    const session = await auth!.getSession({ cookie: cookieOf(verified) });
    // The role travels with the verification that makes the account.
    expect(session?.user.role).toBe("driver");
    expect(contactOf(session?.user.email ?? "")).toEqual({ kind: "phone", phoneNumber });
  });

  it("clamps a new account that asks to be ops", async () => {
    const email = `hopeful-${randomUUID()}@example.com`;
    await post("/email-otp/send-verification-otp", { email, type: "sign-in" });

    const signedIn = await post("/sign-in/email-otp", { email, otp: codeFor(email), role: "ops" });

    expect(await roleOf(cookieOf(signedIn))).toBe("rider");
  });

  it("refuses a wrong code", async () => {
    const email = `rider-${randomUUID()}@example.com`;
    await post("/email-otp/send-verification-otp", { email, type: "sign-in" });

    const wrong = codeFor(email) === "000000" ? "111111" : "000000";
    const refused = await post("/sign-in/email-otp", { email, otp: wrong });

    expect(refused.ok).toBe(false);
    expect(refused.headers.getSetCookie()).toEqual([]);
  });
});
