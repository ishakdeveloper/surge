/**
 * Where the services are, as the phone sees them.
 *
 * The browser can say `localhost` and mean the laptop. A phone cannot: on a
 * device `localhost` is the phone, and an Android emulator has its own. What
 * every one of them *can* reach is the machine serving the JavaScript bundle,
 * because that is how the app got onto the device in the first place — so an
 * unset address defaults to that machine, on the ports the services use
 * locally, and a phone on the same network needs no configuration at all.
 *
 * Set any of them and it wins. A build that is not served by a dev server has
 * no host to borrow and falls back to `localhost`, which is wrong for any real
 * deployment and meant to look wrong.
 *
 * The keys are the names `packages/client` reads through `Config`, so the
 * result is handed to a `ConfigProvider` as it is — a type rather than an
 * interface for that reason, since only a type satisfies an index signature.
 */
export type ServiceUrls = {
  readonly SURGE_API_URL: string;
  readonly SURGE_WS_URL: string;
  readonly AUTH_BASE_URL: string;
};

/** What the build was given. `undefined` is unset; anything else is used verbatim. */
export interface ConfiguredUrls {
  readonly apiUrl: string | undefined;
  readonly wsUrl: string | undefined;
  readonly authUrl: string | undefined;
}

/**
 * The host part of Expo's `hostUri` — `192.168.1.20:8081` becomes
 * `192.168.1.20`. Split by hand because React Native's `URL` does not implement
 * `hostname`.
 */
export const devHost = (hostUri: string | undefined): string => {
  const host = (hostUri ?? "").split("/")[0]?.replace(/:\d+$/, "") ?? "";
  return host === "" ? "localhost" : host;
};

export const serviceUrlsFor = (
  configured: ConfiguredUrls,
  hostUri: string | undefined,
): ServiceUrls => {
  const host = devHost(hostUri);
  return {
    SURGE_API_URL: configured.apiUrl ?? `http://${host}:8100`,
    SURGE_WS_URL: configured.wsUrl ?? `ws://${host}:8100/ws`,
    AUTH_BASE_URL: configured.authUrl ?? `http://${host}:3200`,
  };
};
