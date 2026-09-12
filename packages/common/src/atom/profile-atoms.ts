import { SurgeApi } from "@surge/client/SurgeApi";
import type { UserId } from "@surge/domain/api/Primitives";
import { Effect } from "effect";
import { Atom } from "effect/unstable/reactivity";
import { Keys } from "./reactivity-keys.js";
import { runtime } from "./runtime.js";

/**
 * Names and photos, as atoms.
 *
 * A photo's link is signed for the hour and the same for every reader within
 * it, so reading a profile again — on a new screen, or after a change — hands
 * the browser a link it has usually cached already.
 */

/** The caller's own profile, empty until they fill it in. */
export const myProfileAtom = Atom.withReactivity([Keys.profiles])(
  runtime.atom(
    Effect.gen(function*() {
      const api = yield* SurgeApi;
      const { profile } = yield* api.profiles.getMine();
      return profile;
    }),
  ),
);

/**
 * Anyone's profile: the driver of a trip for its rider, the rider for its
 * driver, whoever asked support for support.
 */
export const profileAtom = Atom.family((userId: UserId) =>
  Atom.withReactivity([Keys.profiles])(
    runtime.atom(
      Effect.gen(function*() {
        const api = yield* SurgeApi;
        const { profile } = yield* api.profiles.get({ params: { userId } });
        return profile;
      }),
    ),
  )
);

// The changes below are kept alive: each is made from a sheet or a step that
// may be put away while it is still in flight, and an atom losing its last
// reader would interrupt the request with it.

/** Set the caller's first name. */
export const renameProfile = runtime.fn(
  Effect.fnUntraced(function*(displayName: string) {
    const api = yield* SurgeApi;
    const { profile } = yield* api.profiles.updateMine({ payload: { displayName } });
    return profile;
  }),
  { reactivityKeys: [Keys.profiles] },
).pipe(Atom.keepAlive);

/**
 * Replace the caller's photo, given as base64. The app crops and re-encodes it
 * first, which is platform code and stays in the app; the server does the same
 * again, so nothing but pixels is ever stored.
 */
export const uploadAvatar = runtime.fn(
  Effect.fnUntraced(function*(image: string) {
    const api = yield* SurgeApi;
    const { profile } = yield* api.profiles.uploadAvatar({ payload: { image } });
    return profile;
  }),
  { reactivityKeys: [Keys.profiles] },
).pipe(Atom.keepAlive);

/** Delete the caller's photo. */
export const removeAvatar = runtime.fn(
  Effect.fnUntraced(function*() {
    const api = yield* SurgeApi;
    const { profile } = yield* api.profiles.removeAvatar({ payload: {} });
    return profile;
  }),
  { reactivityKeys: [Keys.profiles] },
).pipe(Atom.keepAlive);
