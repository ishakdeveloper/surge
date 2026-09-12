import { sessionAtom } from "@/atom/session-atoms.js";
import { ProfileEditor } from "@/components/profile/profile-editor.js";
import { useAtomValue } from "@effect/atom-react";
import { createFileRoute } from "@tanstack/react-router";
import { AsyncResult } from "effect/unstable/reactivity";

/**
 * Who you are to the people you ride with: your first name and your photo,
 * on the white page card every padded page stands on.
 */
const Account = () => {
  const role = useAtomValue(
    sessionAtom,
    (session) => AsyncResult.isSuccess(session) ? session.value.role : undefined,
  );

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
      <div className="mx-auto flex w-full max-w-md flex-col gap-5 rounded-3xl bg-card p-5 shadow-[0_1px_2px_rgb(0_0_0/0.04),0_12px_32px_-18px_rgb(0_0_0/0.22)]">
        <header className="flex flex-col gap-1 px-1">
          <h1 className="text-2xl font-bold">Your profile</h1>
          <p className="text-[15px] text-pretty text-muted-foreground">
            The name and the face the other side of your trips sees.
          </p>
        </header>
        {role === undefined
          ? <p className="py-6 text-sm text-muted-foreground">Loading your profile…</p>
          : <ProfileEditor role={role} />}
      </div>
    </div>
  );
};

export const Route = createFileRoute("/_protected/account")({
  ssr: false,
  staticData: { crumb: "Your profile" },
  component: Account,
});
