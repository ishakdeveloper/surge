import "@/app.css";
import { webPlatform } from "@/atom/platform.js";
import { NotFound } from "@/components/app/not-found.js";
import { RouteCrash } from "@/components/app/route-crash.js";
import { Toaster } from "@/components/ui/sonner.js";
import { publicConfig, publicConfigScript } from "@/lib/public-config.js";
import { RegistryProvider } from "@effect/atom-react";
import { platformAtom } from "@surge/common/atom/runtime";
import { createRootRoute, HeadContent, Outlet, Scripts } from "@tanstack/react-router";
import type * as React from "react";

/**
 * The whole document, not a subtree.
 *
 * Under Start the root route renders `<html>` down, which is why there is no
 * index.html and no main.tsx — `HeadContent` and `Scripts` are what the server
 * fills in and the browser picks up.
 *
 * `errorComponent` is declared here so it covers every page: routes are lazy, so
 * a boundary on each one would miss the failure that stops a route loading at all.
 */
export const Route = createRootRoute({
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { title: "Surge" },
    ],
  }),
  errorComponent: RouteCrash,
  notFoundComponent: NotFound,
  component: () => (
    <RootDocument>
      <Outlet />
    </RootDocument>
  ),
});

const RootDocument = ({ children }: Readonly<{ children: React.ReactNode; }>) => (
  <html lang="en">
    <head>
      {
        /*
        First in the head, so it has run before any module reads `publicConfig`:
        the addresses this server was given at runtime, handed to the browser.
        The browser renders the same string back from what it read, so hydration
        sees no difference.
      */
      }
      <script dangerouslySetInnerHTML={{ __html: publicConfigScript(publicConfig) }} />
      <HeadContent />
    </head>
    <body>
      {
        /*
        Atoms are disposed the moment their last subscriber unmounts, unless the
        registry has an idle window — `AtomRegistry` only schedules a node for a
        timed removal when an idle TTL exists, and deletes it outright otherwise.
        With no window, every navigation threw away the page's data and refetched
        it on the way back, even when nothing had changed.

        Thirty seconds is the window, and it bounds how stale a returning page can
        be. That is proportionate: mutations from this tab invalidate their
        reactivity keys immediately regardless, so the only thing this can hold on
        to is a change somebody else made in the last half-minute.

        The platform is the browser's: the shared atoms in `@surge/common` run on
        whatever the registry is given, and this is where the web app says what
        that is.
      */
      }
      <RegistryProvider defaultIdleTTL={30_000} initialValues={[[platformAtom, webPlatform]]}>
        <div className="h-dvh flex flex-col overflow-hidden">{children}</div>
        <Toaster position="bottom-right" />
      </RegistryProvider>
      <Scripts />
    </body>
  </html>
);
