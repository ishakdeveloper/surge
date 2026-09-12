import type { Page } from "@playwright/test";
import pg from "pg";
import { authDatabaseUrl, newEmail, signIn, signOut } from "../support/accounts.js";
import { expect, test } from "../support/test.js";

/**
 * No screen makes an account ops, on purpose. `make grant-ops` is how a person
 * does it, and this is the same statement.
 */
const grantOps = async (email: string): Promise<void> => {
  const client = new pg.Client({ connectionString: authDatabaseUrl });
  await client.connect();
  try {
    await client.query(`update "user" set role = 'ops' where email = $1`, [email]);
  } finally {
    await client.end();
  }
};

/** The fleet's driver count, as the console prints it. */
const fleetDrivers = async (page: Page): Promise<number> => {
  const value = await page
    .getByRole("region", { name: "Fleet" })
    .locator("div")
    .filter({ has: page.locator("dt", { hasText: /^Drivers$/ }) })
    .locator("dd")
    .textContent();
  return Number((value ?? "").replaceAll(/\D/g, ""));
};

test("a rider is turned away from the console", async ({ page }) => {
  await signIn(page, newEmail("rider"), "Rider");

  await page.goto("/console");

  await expect(page.getByText(/This page is for ops/)).toBeVisible();
});

/**
 * The console, through every service: the simulator's knob goes browser →
 * gateway → simd over gRPC, and the drivers it adds come back as the matcher's
 * fleet frames over the WebSocket.
 */
test("ops see the fleet, and turn the simulator's knob", async ({ page }) => {
  test.setTimeout(180_000);

  const email = newEmail("ops");
  await signIn(page, email, "Rider");
  await grantOps(email);
  // The role travels in the session, so it takes a new one to pick it up.
  await signOut(page);
  await signIn(page, email);

  await page.getByRole("link", { name: "Open the console →" }).click();
  await expect(page.getByRole("heading", { name: "Simulator" })).toBeVisible();

  const drivers = page.getByLabel(/^Drivers:/);
  const rescale = page.getByRole("button", { name: "Rescale" });

  await drivers.fill("500");
  await rescale.click();

  await expect.poll(() => fleetDrivers(page), {
    message: "simulated drivers in the fleet the matchers report",
    timeout: 60_000,
  }).toBeGreaterThan(0);
  await expect(page.getByText(/partitions reporting/)).toBeVisible();

  // Back to none, so the next run's trip is offered to its own driver and not
  // to a simulated one that happens to be nearer.
  await drivers.fill("0");
  await rescale.click();
});
