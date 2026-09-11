import { SurgeMap } from "@/components/map/surge-map.js";
import { getSession } from "@/server/session.js";
import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";

/**
 * Auth pages stand where the rider's page does: a white sheet over the live
 * map of Amsterdam — floating at its left from desktop width, and on a phone
 * rising over a band of it. The first thing anyone sees is the city Surge
 * serves, not a form in a void.
 *
 * `ssr: "data-only"` rather than `false`, and the distinction is the whole point
 * of this route. `false` skips the branch on the server entirely — `beforeLoad`
 * included — so an already-signed-in visitor would be sent the sign-in form and
 * bounced only once the bundle had run. `"data-only"` runs the data phase on the
 * server and leaves the rendering to the browser, which is exactly the split
 * these pages want: the map needs WebGL, and effect-form's `Initialize` sets its
 * ready flag in a `useEffect`, which does not run during SSR.
 */
export const Route = createFileRoute("/auth")({
  ssr: "data-only",
  beforeLoad: async () => {
    const user = await getSession();

    // Signed in already: there is nothing to do here.
    if (user !== null) {
      // oxlint-disable-next-line typescript/only-throw-error
      throw redirect({ to: "/" });
    }
  },
  component: () => (
    <div className="relative flex min-h-0 flex-1 flex-col lg:block">
      {
        /* The city, as a backdrop: nothing on it is interactive here, so it is
          hidden from assistive tech. Its credit lifts clear of the phone sheet. */
      }
      <div
        aria-hidden
        className="relative flex h-[30dvh] shrink-0 max-lg:[&_.maplibregl-ctrl-bottom-right]:bottom-9 lg:absolute lg:inset-0 lg:h-auto"
      >
        <SurgeMap markers={[]} route={[]} follow={[]} className="min-h-0" />
      </div>
      {
        /* Wider, the column scrolls as a whole, with no scrollbar inside the
          card. The card hangs from the top rather than centring itself, so a
          step that is taller or shorter than the last moves only the card's
          foot, and the heading stays where the eye left it. */
      }
      <div className="relative z-10 -mt-6 min-h-0 flex-1 overflow-y-auto rounded-t-3xl bg-card px-5 pt-7 pb-10 shadow-[0_-10px_30px_-18px_rgb(0_0_0/0.35)] lg:pointer-events-none lg:absolute lg:inset-y-0 lg:left-0 lg:mt-0 lg:flex lg:w-[520px] lg:flex-col lg:rounded-none lg:bg-transparent lg:p-6 lg:shadow-none">
        <div className="mx-auto w-full max-w-md lg:pointer-events-auto lg:rounded-3xl lg:bg-card lg:p-8 lg:shadow-[0_18px_44px_-18px_rgb(0_0_0/0.35)]">
          <Outlet />
        </div>
      </div>
    </div>
  ),
});
