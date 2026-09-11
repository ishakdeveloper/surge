import { sessionAtom } from "@/atom/session-atoms.js";
import { CommandPalette } from "@/components/app/command-palette.js";
import { TopBar } from "@/components/app/top-bar.js";
import { useAtomRefresh } from "@effect/atom-react";
import { contactOf, describeContact } from "@surge/domain/iam/Contact";
import { useMatches, useNavigate, useRouteContext } from "@tanstack/react-router";
import type * as React from "react";

/**
 * Signed-in chrome: the floating navigation, and the page beneath it.
 *
 * A floating bar rather than a sidebar because the rider's page is designed
 * for a phone first, where a sidebar leaves no room for the map.
 *
 * No session gate of its own. `/_protected` resolves the session on the
 * server and redirects there, so by the time this renders there is a user —
 * which is why it reads one out of route context instead of waiting on an atom.
 *
 * A page that declares `fullBleed` runs under the bar, edge to edge, and keeps
 * its own content clear of it; every other page starts below the bar, padded.
 * No breadcrumb trail: the bar already says where you are, and every page is
 * one step from it.
 */
export const AppShell = (props: { readonly children: React.ReactNode; }) => {
  const { user } = useRouteContext({ from: "/_protected" });
  const refresh = useAtomRefresh(sessionAtom);
  const navigate = useNavigate();
  const fullBleed = useMatches({
    select: (matches) => matches.some((match) => match.staticData.fullBleed === true),
  });

  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      <CommandPalette />
      <TopBar
        contact={describeContact(contactOf(user.email))}
        onSignOut={() => {
          // The RPC client caches the identity for everything else on the page.
          refresh();
          void navigate({ to: "/auth/sign-in" });
        }}
      />
      {fullBleed
        ? <main className="relative flex min-h-0 flex-1">{props.children}</main>
        : (
          <div className="flex min-h-0 flex-1 flex-col overflow-auto">
            <main className="flex min-h-0 flex-1 flex-col gap-4 px-4 pt-22 pb-8 md:px-8">
              {props.children}
            </main>
          </div>
        )}
    </div>
  );
};
