import { defineConfig, devices } from "@playwright/test";

/**
 * The whole stack, through a browser.
 *
 * These cross every boundary at once — the browser, better-auth, the token
 * exchange, the gateway, Kafka, the matcher, trip, payments, and back out over
 * a WebSocket — which is the part of this project no unit test can reach, and
 * the part a mocked gateway would skip.
 *
 * They run against whatever answers on the local ports: the compose `app`
 * profile in CI, or the same profile here. Auth must have AUTH_DEV_OUTBOX=true,
 * because signing in is a code, and the tests read it from the dev outbox.
 *
 * One worker, in file order. The stack is one city with one matcher, and two
 * trips booked at once near the same pickup would each be offered to the
 * other's driver.
 */
const ci = process.env["CI"] !== undefined;

export default defineConfig({
  testDir: "tests",
  globalSetup: "./support/global-setup.ts",
  fullyParallel: false,
  workers: 1,
  // A flaky end-to-end test is a finding, not something to retry past.
  retries: 0,
  forbidOnly: ci,
  timeout: 90_000,
  expect: { timeout: 20_000 },
  reporter: ci ? [["github"], ["html", { open: "never" }]] : [["list"]],
  use: {
    baseURL: process.env["E2E_WEB_URL"] ?? "http://localhost:5273",
    trace: "retain-on-failure",
    video: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
