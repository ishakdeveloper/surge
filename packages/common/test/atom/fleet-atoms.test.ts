import { addVehicle } from "@/atom/fleet-atoms.js";
import { platformAtom } from "@/atom/runtime.js";
import { fakePlatform, type FakeRequest } from "@/testing/fake-platform.js";
import { AsyncResult, type Atom, AtomRegistry } from "effect/unstable/reactivity";
import { describe, expect, it } from "vitest";

/**
 * Adding a car, over the shared atoms and the fake platform both apps' tests
 * use. Six characters and a class is the whole request: everything else about
 * the car comes back from the vehicle register.
 */
const settled = <A, E>(
  registry: AtomRegistry.AtomRegistry,
  atom: Atom.Atom<AsyncResult.AsyncResult<A, E>>,
) =>
  new Promise<AsyncResult.AsyncResult<A, E>>((resolve) => {
    registry.subscribe(atom, (result) => {
      if (!AsyncResult.isInitial(result) && !result.waiting) resolve(result);
    }, { immediate: true });
  });

const registered = {
  vehicle: {
    id: "veh-1",
    driverId: "drv-1",
    plate: "02JLT3",
    make: "Toyota",
    model: "Toyota Prius",
    colour: "Wit",
    seats: 5,
    packageSlug: "sedan",
    status: "VEHICLE_STATUS_PENDING",
    rejectedReason: "",
    apkExpiresAt: "2027-10-11T00:00:00Z",
    taxiRegistered: true,
    insured: true,
    firstRegisteredAt: "2009-07-06T00:00:00Z",
    registerCheckedAt: "2026-09-12T12:00:00Z",
    createdAt: "2026-09-12T12:00:00Z",
    updatedAt: "2026-09-12T12:00:00Z",
  },
};

describe("adding a car", () => {
  it("sends the plate and the class, and gets the register's answer back", async () => {
    const requests: Array<FakeRequest> = [];
    const registry = AtomRegistry.make({
      initialValues: [[
        platformAtom,
        fakePlatform({
          routes: [{ method: "POST", path: "/v1/fleet/vehicles", body: registered }],
          onRequest: (request) => requests.push(request),
        }),
      ]],
    });

    const outcome = settled(registry, addVehicle);
    registry.set(addVehicle, { plate: "02-JLT-3", packageSlug: "sedan" });

    const result = await outcome;
    expect(AsyncResult.isSuccess(result) && result.value.make).toBe("Toyota");
    // The plate travels as the driver typed it; the server holds the one
    // spelling the register uses.
    expect(requests).toEqual([{
      method: "POST",
      path: "/v1/fleet/vehicles",
      authorization: "Bearer fake-token",
      body: { plate: "02-JLT-3", packageSlug: "sedan" },
    }]);
    registry.dispose();
  });
});
