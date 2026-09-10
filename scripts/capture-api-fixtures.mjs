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

const email = `fixture-${Date.now()}@surge.test`;
const signup = await fetch(`${auth}/api/auth/sign-up/email`, {
  method: "POST",
  headers: { "content-type": "application/json", origin },
  body: JSON.stringify({
    email,
    password: "correct-horse-battery",
    name: "Fixture",
    role: "rider",
  }),
});
if (!signup.ok) throw new Error(`sign-up failed with ${signup.status}: ${await signup.text()}`);

// Only the name=value part; a full Set-Cookie carries attributes a Cookie
// header must not.
const cookie = signup.headers.getSetCookie().map((entry) => entry.split(";")[0]).join("; ");
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
