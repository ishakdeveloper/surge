import type { BrowserContext } from "@playwright/test";
import { authUrl } from "./accounts.js";

let issued = 0;

/**
 * Gives a browser context its own client address, as far as auth can tell.
 *
 * Deployed, a load balancer stands in front of auth and names every caller in
 * X-Forwarded-For, and auth's rate limits are per caller. The app profile has
 * no load balancer, so every test's browser is the same caller on 127.0.0.1 —
 * and better-auth, finding no forwarded address, puts them all in one shared
 * bucket that a handful of sign-ins in a minute exhausts.
 *
 * This adds the header a load balancer would, on requests to auth only, and
 * keeps every header the browser sent — the session cookie included.
 */
export const asOwnClient = async (context: BrowserContext): Promise<void> => {
  issued += 1;
  const address = `10.${process.pid % 250}.${Math.floor(issued / 250)}.${(issued % 250) + 1}`;

  await context.route(`${authUrl}/**`, async (route) => {
    await route.continue({
      headers: { ...(await route.request().allHeaders()), "x-forwarded-for": address },
    });
  });
};
