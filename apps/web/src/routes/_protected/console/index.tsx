import { sessionAtom } from "@/atom/session-atoms.js";
import { ConsoleView } from "@/routes/_protected/console/-components/console-view.js";
import { useAtomValue } from "@effect/atom-react";
import { createFileRoute } from "@tanstack/react-router";
import { AsyncResult } from "effect/unstable/reactivity";

/**
 * The ops console: the whole fleet on a map, how the city is split between the
 * matchers, how long matching takes, and the simulator's knob.
 *
 * The gateway refuses a non-ops watch and the simulator a non-ops rescale
 * regardless; this only says so before anybody tries.
 */
const Console = () => {
  const role = useAtomValue(
    sessionAtom,
    (session) => AsyncResult.isSuccess(session) ? session.value.role : undefined,
  );

  if (role !== undefined && role !== "ops") {
    return (
      <p className="text-muted-foreground text-sm">
        This page is for ops. <code className="font-mono text-xs">make grant-ops EMAIL=…</code>{" "}
        makes an account ops; sign in again afterwards so the token carries the new role.
      </p>
    );
  }

  return <ConsoleView />;
};

export const Route = createFileRoute("/_protected/console/")({
  // Client-only, like the ride and drive pages: a socket and a WebGL map.
  ssr: false,
  staticData: { crumb: "Console" },
  component: Console,
});
