import { sessionAtom } from "@/atom/session-atoms.js";
import { useAtomValue } from "@effect/atom-react";
import { AsyncResult } from "effect/unstable/reactivity";
import { Redirect } from "expo-router";

/** Where a signed-in person lands: their role's surface. */
const Home = () => {
  const role = useAtomValue(
    sessionAtom,
    (session) => AsyncResult.isSuccess(session) ? session.value.role : undefined,
  );

  return <Redirect href={role === "driver" ? "/drive" : role === "rider" ? "/ride" : "/account"} />;
};

export default Home;
