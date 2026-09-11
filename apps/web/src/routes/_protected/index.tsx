import { sessionAtom } from "@/atom/session-atoms.js";
import { QueryError } from "@/components/app/query-error.js";
import { hasOnboarded, Onboarding } from "@/routes/_protected/-components/onboarding.js";
import { useAtomValue } from "@effect/atom-react";
import { createFileRoute, Navigate, useRouteContext } from "@tanstack/react-router";
import { AsyncResult } from "effect/unstable/reactivity";
import * as React from "react";

/**
 * Where a signed-in person lands. The first time, that is onboarding; after
 * it is finished or skipped, straight to their surface — the rider's booking,
 * the driver's shift, the operator's console.
 */
const Home = () => {
  const { user } = useRouteContext({ from: "/_protected" });
  const session = useAtomValue(sessionAtom);
  const [done, setDone] = React.useState(() => hasOnboarded(user.id));

  if (AsyncResult.isInitial(session)) return <Notice>Loading…</Notice>;
  if (AsyncResult.isFailure(session)) {
    return (
      <Notice>
        <QueryError result={session} subject="your session" />
      </Notice>
    );
  }

  const { role } = session.value;
  if (role === "ops") return <Navigate to="/console" />;
  if (done) return <Navigate to={role === "rider" ? "/ride" : "/drive"} />;

  return (
    <Onboarding
      setup={role}
      userId={user.id}
      onSkip={() => {
        setDone(true);
      }}
    />
  );
};

/** The page is full-bleed, so the states without a map bring their own margin. */
const Notice = (props: { readonly children: React.ReactNode; }) => (
  <div className="flex-1 px-4 pt-24 text-[15px] text-muted-foreground md:px-8">
    {props.children}
  </div>
);

export const Route = createFileRoute("/_protected/")({
  // Client-only: onboarding draws a WebGL map and remembers itself in the
  // browser's storage, neither of which the server has.
  ssr: false,
  staticData: { crumb: "Home", fullBleed: true },
  component: Home,
});
