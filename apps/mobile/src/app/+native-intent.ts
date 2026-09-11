/**
 * Links into the app that are not screens.
 *
 * `surge://stripe-redirect` is where a bank's app sends the rider back after a
 * redirect-based check. Stripe's provider completes the step from that URL (see
 * `app/_layout.tsx`); the router only has to not treat it as a route, and
 * returns to where the rider is.
 */
export const redirectSystemPath = (event: { readonly path: string; readonly initial: boolean; }) =>
  event.path.includes("stripe-redirect") ? "/" : event.path;
