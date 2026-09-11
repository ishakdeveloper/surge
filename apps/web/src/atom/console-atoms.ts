import { Realtime } from "@surge/client/Realtime";
import { SurgeApi } from "@surge/client/SurgeApi";
import { Keys } from "@surge/common/atom/reactivity-keys";
import { runtime } from "@surge/common/atom/runtime";
import type { FleetUpdate, Viewport } from "@surge/domain/realtime/Wire";
import { Effect, Option, Result, Stream } from "effect";
import { Atom } from "effect/unstable/reactivity";

/**
 * The console: the fleet over the socket, the simulator over REST.
 *
 * The fleet is a subscription shaped by what the map is looking at. The gateway
 * sends counts per cell when zoomed out and individual drivers only inside the
 * viewport when zoomed in — ten thousand positions a second to every console
 * would be half a megabyte a second each, which is how a dispatch screen
 * freezes a tab. So the viewport is state, and telling the gateway about it is
 * an atom that reruns whenever it changes.
 */

/** What the map is showing. Wrapped, because `Atom.make` reads an Option as an Effect. */
export interface ConsoleView {
  readonly viewport: Option.Option<Viewport>;
}

export const viewportAtom = Atom.make<ConsoleView>({ viewport: Option.none() });

/**
 * Tells the gateway what the map is showing, again whenever it moves.
 *
 * Only ever a watch, never an unwatch: a rebuild's finalizer runs on a forked
 * fiber, and an unwatch that landed after the next watch would leave the
 * console subscribed to nothing. Stopping belongs to `fleetAtom`, whose
 * lifetime is the page's.
 */
export const fleetWatchAtom = runtime.atom((get) =>
  Effect.gen(function*() {
    const { viewport } = get(viewportAtom);
    if (Option.isNone(viewport)) return;
    const realtime = yield* Realtime;
    yield* realtime.watchFleet(viewport);
  })
);

/** One second of the fleet's numbers, for the latency chart. */
export interface FleetSample {
  readonly atMs: number;
  readonly p50Ms: number;
  readonly p95Ms: number;
  readonly p99Ms: number;
  readonly matchedPerSecond: number;
}

export interface Fleet {
  readonly latest: FleetUpdate;
  /** The last two minutes, oldest first. */
  readonly history: ReadonlyArray<FleetSample>;
}

const HISTORY = 120;

/**
 * The latest update and a short history, from one subscription.
 *
 * The unwatch is here, tied to the stream: leaving the page closes the
 * subscription, and the gateway stops sending the fleet to a console nobody is
 * looking at.
 */
export const fleetAtom = runtime.atom(
  Stream.unwrap(
    Effect.gen(function*() {
      const realtime = yield* Realtime;
      yield* Effect.addFinalizer(() => realtime.watchFleet(Option.none()));
      return realtime.fleet.pipe(
        Stream.scan(
          Option.none<Fleet>(),
          (previous, latest) =>
            Option.some({
              latest,
              history: [
                ...Option.match(previous, { onNone: () => [], onSome: (fleet) => fleet.history })
                  .slice(-(HISTORY - 1)),
                {
                  atMs: latest.atMs,
                  p50Ms: latest.stats.p50Ms,
                  p95Ms: latest.stats.p95Ms,
                  p99Ms: latest.stats.p99Ms,
                  matchedPerSecond: latest.stats.matchedPerSecond,
                },
              ],
            }),
        ),
        Stream.filterMap((fleet) =>
          Option.match(fleet, {
            onNone: () => Result.fail(fleet),
            onSome: (value) => Result.succeed(value),
          })
        ),
      );
    }),
  ),
);

/** The simulator's configuration: the knob, not its output. */
export const simulatorAtom = Atom.withReactivity([Keys.sim])(
  runtime.atom(
    Effect.gen(function*() {
      const api = yield* SurgeApi;
      const { simulator } = yield* api.simulator.get();
      return simulator;
    }),
  ),
);

/** Rescale the fleet. Ops only, which the simulator enforces rather than this page. */
export const configureSimulator = runtime.fn(
  Effect.fnUntraced(function*(settings: {
    readonly drivers: number;
    readonly pingIntervalMs: number;
  }) {
    const api = yield* SurgeApi;
    const { simulator } = yield* api.simulator.configure({ payload: settings });
    return simulator;
  }),
  { reactivityKeys: [Keys.sim] },
);
