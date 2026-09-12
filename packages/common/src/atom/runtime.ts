import type { AuthToken } from "@surge/client/AuthToken";
import { Chat } from "@surge/client/Chat";
import { Fleet } from "@surge/client/Fleet";
import type { Geolocation } from "@surge/client/Geolocation";
import { Places } from "@surge/client/Places";
import { Realtime } from "@surge/client/Realtime";
import { SurgeApi } from "@surge/client/SurgeApi";
import { Effect, Layer, Schema } from "effect";
import type { HttpClient } from "effect/unstable/http";
import { Atom } from "effect/unstable/reactivity";
import type { Socket } from "effect/unstable/socket";

/**
 * The one runtime every shared atom runs on, in the web app and on the phone.
 *
 * The atoms in this package are the same on both surfaces — the same queries,
 * the same pushes turned into invalidation, the same ping loop. What differs is
 * underneath them: the browser's fetch and WebSocket against React Native's,
 * `navigator.geolocation` against expo-location, a session cookie the browser
 * sends by itself against one the app has to attach. `packages/client` already
 * declares those as requirements; this is where each app satisfies them.
 *
 * So the runtime reads its platform from an atom, and each app seeds that atom
 * when it creates its registry:
 *
 *     <RegistryProvider initialValues={[[platformAtom, webPlatform]]}>
 *
 * A seam in the registry rather than a module each app edits, which is also
 * what makes it the test boundary — a test seeds a platform with a fake HTTP
 * client and runs the production atoms over it, per `RULES.md`.
 *
 * One runtime per registry, and the default factory memoises its layers per
 * registry: one `AuthToken` cache and one WebSocket for a registry's lifetime.
 */

/**
 * What an app provides, configured: the platform's HTTP client and WebSocket
 * constructor, a device position, and the `AuthToken` that trades a session for
 * a JWT — the one service that differs in how it reaches the auth server.
 *
 * Addresses travel inside the layer, as a `ConfigProvider`, so the services
 * built on top read the same configuration the token source was built with.
 */
export type PlatformServices =
  | AuthToken
  | Geolocation
  | HttpClient.HttpClient
  | Socket.WebSocketConstructor;

/** Wrapped, because `Atom.make` and friends read some bare values as Effects. */
export interface Platform {
  readonly layer: Layer.Layer<PlatformServices>;
}

/**
 * The registry was created without a platform.
 *
 * A defect, not an error a screen can act on: it is a wiring mistake in an app
 * or a test, and it should fail the first atom that needs the runtime loudly
 * rather than leave every query loading for ever.
 */
export class PlatformNotProvided
  extends Schema.TaggedError<PlatformNotProvided>()("PlatformNotProvided", {})
{
  override readonly message =
    "No platform was given to the atom registry. Seed `platformAtom` through `RegistryProvider`'s `initialValues`.";
}

const noPlatform: Platform = {
  layer: Layer.effectContext<PlatformServices, never, never>(Effect.die(new PlatformNotProvided())),
};

/**
 * The platform this registry runs on. Kept alive: it is configuration, read by
 * the runtime whenever it builds, and must outlive every atom that reads it.
 */
export const platformAtom: Atom.Atom<Platform> = Atom.keepAlive(Atom.readable(() => noPlatform));

export const runtime = Atom.runtime((get) =>
  // Chat and the fleet are built over the same SurgeApi and Realtime the atoms
  // use, so the registry holds one socket rather than one per service built on
  // it.
  Layer.mergeAll(Chat.layer, Fleet.layer).pipe(
    Layer.provideMerge(Layer.mergeAll(SurgeApi.layer, Realtime.layer, Places.layer)),
    Layer.provideMerge(get(platformAtom).layer),
  )
);
