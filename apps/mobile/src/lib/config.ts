// Expo inlines `process.env.EXPO_PUBLIC_*` only when it is written literally,
// so these cannot go through a `ConfigProvider` the way the services' do —
// their result is what the runtime hands to one.
/* oxlint-disable effecttsgo/process-env */
import { serviceUrlsFor } from "@/lib/service-urls.js";
import { Option } from "effect";
import Constants from "expo-constants";

/**
 * The values this build is configured with, and no more.
 *
 * `EXPO_PUBLIC_` because that is the only way Expo puts a value in the bundle,
 * and it replaces `process.env.EXPO_PUBLIC_X` statically — so each is written
 * out rather than looked up, which makes this the list of what the phone knows.
 * Public addresses, never secrets: anything in the bundle can be read by
 * whoever holds the phone.
 */
export const serviceUrls = serviceUrlsFor(
  {
    apiUrl: process.env.EXPO_PUBLIC_SURGE_API_URL,
    wsUrl: process.env.EXPO_PUBLIC_SURGE_WS_URL,
    authUrl: process.env.EXPO_PUBLIC_AUTH_BASE_URL,
  },
  Constants.expoConfig?.hostUri,
);

/**
 * Stripe's publishable key, when payments run against Stripe — the web app's
 * rule: no key means the payments service is running its fake processor, there
 * is no Stripe account for the SDK to talk to, and the app saves the fake's
 * test card with a plain button instead of opening Stripe's sheet.
 */
export const stripePublishableKey: Option.Option<string> = Option.fromNullishOr(
  process.env.EXPO_PUBLIC_STRIPE_PUBLISHABLE_KEY,
).pipe(Option.filter((key) => key !== ""));

export const stripeConfigured = Option.isSome(stripePublishableKey);
