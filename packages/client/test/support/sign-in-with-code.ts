/**
 * Signing in the way a person does — a code to an email address, typed back —
 * for the integration tests, which need real accounts on a real auth service.
 *
 * Nobody reads the code from an inbox: the auth service's dev outbox serves
 * the last one it "sent", so these need it started with
 * `AUTH_DEV_OUTBOX=true`. `outboxOpen` is what each test's skip checks, beside
 * the services being up.
 */
export interface AuthServer {
  readonly base: string;
  /**
   * better-auth refuses a request whose Origin it does not trust, and Node's
   * fetch sends a null one — so a call that works from a shell fails here with
   * MISSING_OR_NULL_ORIGIN. The web app's origin is what the browser these
   * tests stand in for would send.
   */
  readonly origin: string;
}

/** An open outbox refuses a question with no address; a closed one has no such route. */
export const outboxOpen = async (auth: AuthServer): Promise<boolean> => {
  try {
    const response = await fetch(`${auth.base}/dev/outbox`, { signal: AbortSignal.timeout(2000) });
    return response.status === 400;
  } catch {
    return false;
  }
};

const post = async (auth: AuthServer, path: string, body: unknown): Promise<Response> => {
  const response = await fetch(`${auth.base}${path}`, {
    method: "POST",
    headers: { "content-type": "application/json", origin: auth.origin },
    body: JSON.stringify(body),
  });
  if (!response.ok) {
    throw new Error(`${path} failed with ${response.status}: ${await response.text()}`);
  }
  return response;
};

/**
 * The code sent to `email` at or after `sinceMs`. Polled, because better-auth
 * may answer before its sender has run — and a code from an earlier sign-in
 * to the same address is no longer the one that works.
 */
const codeSentTo = async (auth: AuthServer, email: string, sinceMs: number): Promise<string> => {
  for (let attempt = 0; attempt < 25; attempt++) {
    const response = await fetch(`${auth.base}/dev/outbox?to=${encodeURIComponent(email)}`);
    if (response.ok) {
      const sent = (await response.json()) as { code: string | null; atMs: number; };
      if (sent.code !== null && sent.atMs >= sinceMs) return sent.code;
    }
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  throw new Error(`no code reached the outbox for ${email}`);
};

/**
 * Sends a code to `email`, types it, and trades the session for a token. The
 * first time makes the account, with `role`; later times sign it in again, and
 * pick up a role changed since.
 */
export const signInWithCode = async (
  auth: AuthServer,
  email: string,
  role?: "rider" | "driver",
): Promise<string> => {
  const sinceMs = Date.now();
  await post(auth, "/api/auth/email-otp/send-verification-otp", { email, type: "sign-in" });
  const otp = await codeSentTo(auth, email, sinceMs);

  const signedIn = await post(
    auth,
    "/api/auth/sign-in/email-otp",
    role === undefined ? { email, otp } : { email, otp, role },
  );
  // Only the name=value part; a full Set-Cookie carries attributes the Cookie
  // header must not.
  const cookie = signedIn.headers.getSetCookie().map((entry) => entry.split(";")[0]).join("; ");

  const response = await fetch(`${auth.base}/api/auth/token`, {
    headers: { cookie, origin: auth.origin },
  });
  const body = (await response.json()) as { token?: unknown; };
  if (typeof body.token !== "string") {
    throw new Error(`no token in response: ${JSON.stringify(body)}`);
  }
  return body.token;
};
