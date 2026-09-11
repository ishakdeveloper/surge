#!/usr/bin/env node
/**
 * Captures real gateway responses into
 * `packages/domain/test/api/testdata/responses.json`.
 *
 * `Generation.test.ts` decodes these with the generated schemas, which is the
 * only way to be sure the generation pipeline still produces a client that can
 * read the server. Handwritten fixtures would agree with whatever the schemas
 * happen to say; these came off the wire.
 *
 * Needs the stack up: `make up`, `make dev-auth`, and the gateway and trip
 * services running. Rerun it when the proto changes something a response
 * carries.
 */
import * as fs from "node:fs";
import * as path from "node:path";
import { fileURLToPath } from "node:url";

const auth = process.env["AUTH_BASE_URL"] ?? "http://localhost:3200";
const gateway = process.env["SURGE_API_URL"] ?? "http://localhost:8100";
const origin = process.env["WEB_URL"] ?? "http://localhost:5273";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const target = path.join(root, "packages", "domain", "test", "api", "testdata", "responses.json");

// Signed in the way a person is: a code to the address, read back from the
// auth service's dev outbox rather than an inbox. An open outbox refuses a
// question with no address; a closed one has no such route.
if ((await fetch(`${auth}/dev/outbox`)).status !== 400) {
  throw new Error(
    "The auth service has no dev outbox to read a sign-in code from. Start it with AUTH_DEV_OUTBOX=true.",
  );
}

const post = async (route, body) => {
  const response = await fetch(`${auth}${route}`, {
    method: "POST",
    headers: { "content-type": "application/json", origin },
    body: JSON.stringify(body),
  });
  if (!response.ok) {
    throw new Error(`${route} failed with ${response.status}: ${await response.text()}`);
  }
  return response;
};

const email = `fixture-${Date.now()}@surge.test`;
const sentSince = Date.now();
await post("/api/auth/email-otp/send-verification-otp", { email, type: "sign-in" });

// Polled, because better-auth may answer before its sender has run.
const codeSent = async () => {
  for (let attempt = 0; attempt < 25; attempt++) {
    const response = await fetch(`${auth}/dev/outbox?to=${encodeURIComponent(email)}`);
    if (response.ok) {
      const sent = await response.json();
      if (sent.code !== null && sent.atMs >= sentSince) return sent.code;
    }
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  throw new Error(`no code reached the outbox for ${email}`);
};

const signedIn = await post("/api/auth/sign-in/email-otp", {
  email,
  otp: await codeSent(),
  role: "rider",
});

// Only the name=value part; a full Set-Cookie carries attributes a Cookie
// header must not.
const cookie = signedIn.headers.getSetCookie().map((entry) => entry.split(";")[0]).join("; ");
const { token } = await (await fetch(`${auth}/api/auth/token`, { headers: { cookie, origin } }))
  .json();

const headers = { "content-type": "application/json", authorization: `Bearer ${token}` };
const json = async (input, init) => (await fetch(input, init)).json();

const preview = await json(`${gateway}/v1/trips:preview`, {
  method: "POST",
  headers,
  body: JSON.stringify({
    pickup: { lat: 52.3791, lng: 4.9003 },
    dropoff: { lat: 52.36, lng: 4.8852 },
  }),
});

const created = await json(`${gateway}/v1/trips`, {
  method: "POST",
  headers: { ...headers, "idempotency-key": `fixture-${Date.now()}` },
  body: JSON.stringify({ fareId: preview.fares[0].fareId }),
});

const fixtures = {
  preview,
  created,
  listed: await json(`${gateway}/v1/trips`, { headers }),
  notFound: await json(`${gateway}/v1/trips/does-not-exist`, { headers }),
  // No Authorization header at all, which is the one error a signed-out browser
  // meets before anything else.
  unauth: await json(`${gateway}/v1/trips`, { headers: { "content-type": "application/json" } }),
};

fs.mkdirSync(path.dirname(target), { recursive: true });
fs.writeFileSync(target, `${JSON.stringify(fixtures, null, 2)}\n`);
console.log(path.relative(root, target));
