import { sessionAtom } from "@/atom/session-atoms.js";
import { QueryError } from "@/components/app/query-error.js";
import { useAtomValue } from "@effect/atom-react";
import { createFileRoute, Link } from "@tanstack/react-router";
import { AsyncResult } from "effect/unstable/reactivity";

/**
 * Where a signed-in person lands, and a live check.
 *
 * It points each role at its surface, and it renders the decoded `Identity` —
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

      {session.value.role === "rider" && (
        <Link to="/ride" className="text-primary text-sm underline-offset-4 hover:underline">
          Book a ride →
        </Link>
      )}
      {session.value.role === "driver" && (
        <Link to="/drive" className="text-primary text-sm underline-offset-4 hover:underline">
          Start a shift →
        </Link>
      )}
      {session.value.role === "ops" && (
        <Link to="/console" className="text-primary text-sm underline-offset-4 hover:underline">
          Open the console →
        </Link>
      )}
    </section>
  );
};

export const Route = createFileRoute("/_protected/")({
  staticData: { crumb: "Home" },
  component: Home,
});
