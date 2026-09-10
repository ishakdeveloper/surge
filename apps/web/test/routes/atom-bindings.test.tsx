import { runtime } from "@/atom/runtime.js";
import { RegistryProvider } from "@effect/atom-react";
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { render } from "@testing-library/react";
import { Effect, Layer } from "effect";
import { describe, expect, it } from "vitest";

/**
 * Every page renders without a missing binding.
 *
 * This exists because of a real failure: `ReferenceError: onboardingAtom is not
 * defined`, thrown inside a page. Nothing caught it — `tsc` sees the import, the
 * production bundle resolves it, and no test rendered the page — so the first
 * report came from somebody clicking through to it.
 *
 * The pages are imported dynamically and mounted for real. A module whose imports
 * do not line up with its uses throws on evaluation or on first render, and either
 * way this fails. Data is not stubbed: the atoms cannot reach the network under jsdom,
 * so each page renders its loading or failure branch — which is enough, because
 * what is under test is that the module and its bindings hold together.
 */
const mount = async (load: () => Promise<{ readonly node: React.ReactNode; }>) => {
  const { node } = await load();
  const root = createRootRoute({ component: () => node });
  const router = createRouter({
    routeTree: root,
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });

  await router.load();

  // The runtime's layer is seeded with one that never finishes building. The
  // pages below open a WebSocket and call the gateway the moment their atoms
  // are read, and under jsdom the gateway on this machine is real — so without
  // this a page test would connect to a running system, or retry against one
  // that is not there, for as long as the test process lives. What is under
  // test is that the modules and their bindings hold together, and a page
  // waiting on a runtime that never arrives still renders its loading state.
  const inert = Layer.effectContext(Effect.never);

  return render(
    <RegistryProvider initialValues={[[runtime.layer, inert]]}>
      <RouterProvider router={router as never} />
    </RegistryProvider>,
  );
};

describe("page modules", () => {
  /**
   * Every signed-in page, mounted for real. Adding a route here is cheap and is
   * the only thing that keeps this honest as pages are added.
   */
  const pages = [
    ["home", () => import("@/routes/_protected/index.js")],
    ["ride", () => import("@/routes/_protected/ride/index.js")],
    ["ride/payment", () => import("@/routes/_protected/ride/payment.js")],
    ["drive", () => import("@/routes/_protected/drive/index.js")],
    ["drive/earnings", () => import("@/routes/_protected/drive/earnings.js")],
    ["drive/payouts/refresh", () => import("@/routes/_protected/drive/payouts/refresh.js")],
    ["console", () => import("@/routes/_protected/console/index.js")],
  ] as const;

  for (const [name, load] of pages) {
    it(`renders ${name}`, async () => {
      const { container } = await mount(async () => {
        // The components are not exported — the route is — so the route's own
        // component is what gets mounted, exactly as the router would mount it.
        const module = await load();
        const Component = module.Route.options.component!;

        return { node: <Component /> };
      });

      // The shell resolves no session under jsdom, so it renders its checking
      // state. Reaching that at all means every module in the chain evaluated.
      expect(container).not.toBeEmptyDOMElement();
    });
  }
});
