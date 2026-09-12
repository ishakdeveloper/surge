import { test as base } from "@playwright/test";
import { asOwnClient } from "./client-address.js";

/**
 * Playwright's `test`, with every test's browser a separate caller to auth —
 * see `asOwnClient`. Tests that open extra contexts call it on each.
 */
export const test = base.extend({
  context: async ({ context }, use) => {
    await asOwnClient(context);
    await use(context);
  },
});

export { expect } from "@playwright/test";
