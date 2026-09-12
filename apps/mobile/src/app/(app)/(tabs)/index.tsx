import { sessionAtom } from "@/atom/session-atoms.js";
import { hasOnboarded } from "@/lib/onboarded.js";
import { useAtomValue } from "@effect/atom-react";
import { AsyncResult } from "effect/unstable/reactivity";
import { Redirect } from "expo-router";

/**
 * Where a signed-in person lands: the first time, onboarding; after that,
 * their role's surface.
 */
const Home = () => {
  const session = useAtomValue(
    sessionAtom,
    (result) => AsyncResult.isSuccess(result) ? result.value : undefined,
  );
  const role = session?.role;
  const surface = role === "driver" ? "/drive" : role === "rider" ? "/ride" : "/account";
  const fresh = session !== undefined && (role === "rider" || role === "driver")
    && !hasOnboarded(session.userId);

  return <Redirect href={fresh ? "/onboarding" : surface} />;
};

export default Home;
