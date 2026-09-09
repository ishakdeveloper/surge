import { sessionAtom } from "@/atom/session-atoms.js";
import { QueryError } from "@/components/app/query-error.js";
import { useAtomValue } from "@effect/atom-react";
import { createFileRoute } from "@tanstack/react-router";
import { AsyncResult } from "effect/unstable/reactivity";

/**
 * A placeholder, and a live check.
 *
 * Phase 4 replaces this with the console, the rider app and the driver app.
 * Until then it earns its place by rendering the decoded `Identity` — which is
 * the one thing the whole auth arrangement has to get right, because the same
 * three fields are what Go parses out of the JWT. If the role shown here is
 * wrong, every Go service is about to be wrong in the same way.
 */
const Home = () => {
  const session = useAtomValue(sessionAtom);

  if (AsyncResult.isInitial(session)) {
    return <p className="text-muted-foreground text-sm">Loading…</p>;
  }

  if (AsyncResult.isFailure(session)) {
    return <QueryError result={session} subject="your session" />;
  }

  return (
    <section className="flex flex-col gap-6">
      <div>
        <h1 className="text-lg font-semibold">Surge</h1>
        <p className="text-muted-foreground text-sm">
          Ride-hailing on a geo-sharded matcher. Amsterdam.
        </p>
      </div>

      <dl className="grid max-w-md grid-cols-[8rem_1fr] gap-2 text-sm">
        <dt className="text-muted-foreground">Signed in as</dt>
        <dd className="font-mono">{session.value.email}</dd>
        <dt className="text-muted-foreground">Role</dt>
        <dd className="font-mono">{session.value.role}</dd>
        <dt className="text-muted-foreground">Email verified</dt>
        <dd className="font-mono">{session.value.emailVerified ? "yes" : "no"}</dd>
      </dl>
    </section>
  );
};

export const Route = createFileRoute("/_protected/")({
  staticData: { crumb: "Home" },
  component: Home,
});
