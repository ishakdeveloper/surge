import { type APIRequestContext, expect, type Page } from "@playwright/test";

/** Where the browser reaches auth, and where the dev outbox answers. */
export const authUrl = process.env["E2E_AUTH_URL"] ?? "http://localhost:3200";

/** The gateway, for the readiness check before anything runs. */
export const apiUrl = process.env["E2E_API_URL"] ?? "http://localhost:8100";

/**
 * better-auth's database, for the one thing no screen does on purpose: making
 * an account ops. `make grant-ops` is the way a person does it; this is the
 * same statement.
 */
export const authDatabaseUrl = process.env["E2E_AUTH_DATABASE_URL"]
  ?? "postgresql://surge:surge@localhost:55433/surge_auth";

export type Role = "Rider" | "Driver";

/** A fresh address per call, so no test ever signs in as another test's account. */
export const newEmail = (who: string): string =>
  `e2e-${who}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}@surge.test`;

interface Sent {
  readonly code: string | null;
  readonly atMs: number;
}

/**
 * The code the dev outbox holds for `email`, sent at or after `sinceMs`.
 *
 * Polled, because better-auth can answer before its sender has run — and a code
 * from an earlier sign-in to the same address is no longer the one that works.
 */
const codeFor = async (request: APIRequestContext, email: string, sinceMs: number) => {
  let code = "";
  await expect
    .poll(async () => {
      const response = await request.get(`${authUrl}/dev/outbox`, { params: { to: email } });
      if (!response.ok()) return false;
      const sent = (await response.json()) as Sent;
      if (sent.code === null || sent.atMs < sinceMs) return false;
      code = sent.code;
      return true;
    }, { message: `a sign-in code for ${email} in the dev outbox`, timeout: 15_000 })
    .toBe(true);
  return code;
};

/**
 * Signs in the way a person does: an address, a code, typed back. The first
 * time makes the account with `role`; later times sign the same one in again.
 */
export const signIn = async (page: Page, email: string, role?: Role): Promise<void> => {
  await page.goto("/auth/sign-in");
  await page.getByLabel("Email", { exact: true }).fill(email);
  if (role !== undefined) {
    await page.getByRole("radio", { name: new RegExp(`^${role}`) }).check();
  }

  // Auth and the test read the same clock on this machine; a second of slack
  // covers the gap between reading it here and the send.
  const sinceMs = Date.now() - 1000;
  await page.getByRole("button", { name: "Email me a code" }).click();
  await expect(page.getByText("Enter your code")).toBeVisible();

  await page.getByLabel("Code", { exact: true }).fill(await codeFor(page.request, email, sinceMs));
  await page.getByRole("button", { name: "Continue" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Surge" })).toBeVisible();
};

export const signOut = async (page: Page): Promise<void> => {
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/auth\/sign-in/);
};
