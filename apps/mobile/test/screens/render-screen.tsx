import { RegistryContext } from "@effect/atom-react";
import { afterEach } from "@jest/globals";
import { platformAtom } from "@surge/common/atom/runtime";
import {
  fakePlatform,
  type FakeRequest,
  type FakeRoute,
} from "@surge/common/testing/fake-platform";
import { Identity, UserId } from "@surge/domain/iam/Identity";
import { render } from "@testing-library/react-native";
import { AsyncResult, AtomRegistry } from "effect/unstable/reactivity";
import type * as React from "react";
import { sessionAtom } from "../../src/atom/session-atoms";

/**
 * Registries made by `renderScreen`, disposed after each test.
 *
 * Made here rather than by `RegistryProvider`, which disposes its registry on
 * a timer after unmount — past the end of the test, with the runtime's socket
 * retries and clock ticks still scheduled. Disposing directly closes the
 * runtime's scope, and everything it started with it.
 */
const registries = new Set<AtomRegistry.AtomRegistry>();

afterEach(() => {
  for (const registry of registries) registry.dispose();
  registries.clear();
});

/**
 * Renders a screen the way the app does — inside a registry seeded with a
 * platform — except the platform is the fake one: the atoms, the generated API
 * client and the screen are production code, and only HTTP, the socket, the
 * token and the position are stand-ins.
 *
 * `role` seeds a signed-in session, for screens that read who is signed in;
 * the session atom otherwise asks better-auth, which is its own boundary.
 */
export const renderScreen = (
  element: React.ReactElement,
  options: {
    readonly routes: ReadonlyArray<FakeRoute>;
    readonly requests?: Array<FakeRequest>;
    readonly role?: "rider" | "driver";
  },
) => {
  const platform = fakePlatform({
    routes: options.routes,
    onRequest: (request) => options.requests?.push(request),
  });
  const session = options.role === undefined ? [] : [
    [
      sessionAtom,
      AsyncResult.success(
        new Identity({
          userId: UserId.make("user-1"),
          email: "ada@example.com",
          emailVerified: true,
          role: options.role,
        }),
      ),
    ] as const,
  ];

  const registry = AtomRegistry.make({ initialValues: [[platformAtom, platform], ...session] });
  registries.add(registry);

  return render(<RegistryContext.Provider value={registry}>{element}</RegistryContext.Provider>);
};

/** A trip as the gateway sends it: every field present, numbers as strings where int64. */
export const tripFixture = {
  id: "aa789e96-b9fa-463a-9e65-e193086d8d1a",
  riderId: "user-1",
  driverId: "",
  status: "TRIP_STATUS_REQUESTED",
  pickup: { lat: 52.3791, lng: 4.9003 },
  dropoff: { lat: 52.36, lng: 4.8852 },
  route: { polyline6: "", meters: 6840, seconds: "720" },
  totalCents: "1627",
  createdAt: "2026-09-10T13:15:52Z",
  updatedAt: "2026-09-10T13:15:52Z",
};
