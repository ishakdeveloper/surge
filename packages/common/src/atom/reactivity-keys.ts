/**
 * Invalidation keys shared by every atom module.
 *
 * Each module builds its own atoms, but all of them come from the same default
 * factory, and that factory memoises a single `Reactivity` service per registry
 * — so a key invalidated by a mutation in one module refreshes the queries
 * registered in another. Reads subscribe with `Atom.withReactivity`, writes
 * announce with `reactivityKeys`.
 *
 * These cover state the *browser* fetches over HTTP. Live fleet movement does
 * not belong here: it arrives over the WebSocket many times a second, and
 * routing that through cache invalidation would be a refetch storm rather than
 * a subscription.
 */
export const Keys = {
  /** The signed-in identity. Signing in, out, or verifying email moves it. */
  session: "session",
  /** Trip history and the current trip. */
  trips: "trips",
  /** Vehicles and driver profile. */
  drivers: "drivers",
  /** Simulator configuration — the knob, not its output. */
  sim: "sim",
  /** A rider's card and holds; a driver's payout account, balance and earnings. */
  payments: "payments",
  /**
   * Lists of conversations: their unread counts, and support's queue. A live
   * conversation is not keyed here — `Chat.live` keeps itself current.
   */
  chat: "chat",
  /** Names and photos: the caller's own, and everyone's the caller has seen. */
  profiles: "profiles",
  /** A driver's standing, their cars and papers, and the review queue. */
  fleet: "fleet",
} as const;
