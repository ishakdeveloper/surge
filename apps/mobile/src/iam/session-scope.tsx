import { mobilePlatform } from "@/atom/platform.js";
import { RegistryProvider } from "@effect/atom-react";
import { platformAtom } from "@surge/common/atom/runtime";
import * as React from "react";

/**
 * One atom registry per signed-in person.
 *
 * `Atom.runtime` memoises its layers per registry, so a registry is also one
 * `AuthToken` cache and one WebSocket. The web app starts over with a page
 * load when the person changes; an app does not reload, and without this a
 * rider who signed out and a driver who signed in on the same phone would
 * share a socket the gateway had bound to the rider — pings refused, offers
 * routed to somebody else.
 *
 * So signing in or out restarts the scope: a fresh registry, whose runtime
 * mints its own token and opens its own socket, while the old one is disposed
 * and its finalizers close what it held.
 */
const RestartContext = React.createContext<() => void>(() => {});

export const SessionScope = (props: { readonly children: React.ReactNode; }) => {
  const [generation, setGeneration] = React.useState(0);
  const restart = React.useCallback(() => {
    setGeneration((current) => current + 1);
  }, []);

  return (
    <RestartContext.Provider value={restart}>
      <RegistryProvider key={generation} initialValues={[[platformAtom, mobilePlatform]]}>
        {props.children}
      </RegistryProvider>
    </RestartContext.Provider>
  );
};

/** Start over as whoever the session now says. Call once a sign-in or sign-out has landed. */
export const useRestartSession = () => React.useContext(RestartContext);
