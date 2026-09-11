import { sessionAtom, signOut } from "@/atom/session-atoms.js";
import { SurgeMark } from "@/components/app/surge-mark.js";
import { cn } from "@/lib/utils.js";
import { useAtomValue } from "@effect/atom-react";
import type { Role } from "@surge/domain/iam/Identity";
import type { LinkProps } from "@tanstack/react-router";
import { Link, useMatchRoute } from "@tanstack/react-router";
import { Effect } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import { Car, CreditCard, Gauge, LogOut, Navigation, UserRound, Wallet } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import * as React from "react";

/**
 * Every signed-in page's destinations. Exported for the command palette, so
 * the two cannot list different pages.
 */
export const nav: ReadonlyArray<{
  readonly to: NonNullable<LinkProps["to"]>;
  readonly label: string;
  readonly icon: LucideIcon;
  /** Who the page is for. The others see it only in the palette. */
  readonly role: Role;
  readonly exact?: boolean;
}> = [
  // Exact, or Ride would stay lit beside Payment, and Drive beside Earnings,
  // while either child page is open.
  { to: "/ride", label: "Ride", icon: Car, role: "rider", exact: true },
  { to: "/ride/payment", label: "Payment", icon: CreditCard, role: "rider" },
  { to: "/drive", label: "Drive", icon: Navigation, role: "driver", exact: true },
  { to: "/drive/earnings", label: "Earnings", icon: Wallet, role: "driver" },
  { to: "/console", label: "Console", icon: Gauge, role: "ops" },
];

const EASE_OUT = [0.22, 1, 0.36, 1] as const;

/**
 * The navigation, as a black pill floating over the page: black because it is
 * a service rather than a direction, with the page you are on marked by a
 * yellow pill that slides from link to link. It settles into place on arrival.
 * Real links throughout, so middle-click and copy-link behave as expected.
 *
 * It floats rather than spanning the top: on the rider's page the map runs
 * underneath it, edge to edge.
 */
export const TopBar = (props: { readonly contact: string; readonly onSignOut: () => void; }) => {
  const role = useAtomValue(
    sessionAtom,
    (session) => AsyncResult.isSuccess(session) ? session.value.role : undefined,
  );
  const matchRoute = useMatchRoute();
  // Until the session is known every link shows, rather than none.
  const links = nav.filter((item) => role === undefined || item.role === role);
  const current = links.find((item) =>
    matchRoute({ to: item.to, fuzzy: item.exact !== true }) !== false
  )?.to;

  return (
    <div className="pointer-events-none absolute inset-x-0 top-0 z-40 flex justify-center px-3 pt-3">
      <motion.header
        data-surface="dark"
        initial={{ y: -20, opacity: 0 }}
        animate={{ y: 0, opacity: 1 }}
        transition={{ type: "spring", stiffness: 380, damping: 32 }}
        // Full width on a phone; wider, as wide as what it holds, so it reads as
        // a control floating over the page and not an empty black slab.
        className="pointer-events-auto flex h-14 w-full items-center gap-1 rounded-full bg-secondary py-2 pr-2 pl-2.5 text-secondary-foreground shadow-[0_14px_34px_-14px_rgb(0_0_0/0.55)] md:w-auto md:min-w-[26rem]"
      >
        <Link
          to="/"
          className="mr-1 flex shrink-0 items-center gap-2 rounded-full py-1 pr-2.5 pl-1 text-lg font-bold"
        >
          <SurgeMark className="size-8" />
          <span className="max-sm:sr-only">Surge</span>
        </Link>

        <nav aria-label="Main" className="flex min-w-0 items-center gap-0.5">
          {links.map(({ to, label, exact }) => {
            const active = to === current;
            return (
              <Link
                key={to}
                to={to}
                {...(exact === true ? { activeOptions: { exact: true } } : {})}
                className={cn(
                  "relative rounded-full px-3.5 py-2 text-[15px] font-semibold transition-colors duration-150",
                  active ? "text-primary-foreground" : "text-white/70 hover:text-white",
                )}
              >
                {active && (
                  <motion.span
                    layoutId="top-bar-current"
                    aria-hidden
                    className="absolute inset-0 rounded-full bg-primary"
                    transition={{ type: "spring", stiffness: 520, damping: 40 }}
                  />
                )}
                <span className="relative">{label}</span>
              </Link>
            );
          })}
        </nav>

        <AccountMenu contact={props.contact} onSignOut={props.onSignOut} />
      </motion.header>
    </div>
  );
};

/**
 * Who is signed in, and the way out: a round button with the account's
 * initial, opening a small white card. It closes on Escape, on a click
 * anywhere else, and on signing out.
 */
const AccountMenu = (props: { readonly contact: string; readonly onSignOut: () => void; }) => {
  const [open, setOpen] = React.useState(false);
  const root = React.useRef<HTMLDivElement>(null);
  const button = React.useRef<HTMLButtonElement>(null);
  const menuId = React.useId();
  const initial = /[a-z]/i.exec(props.contact)?.[0]?.toUpperCase();

  React.useEffect(() => {
    if (!open) return;
    const outside = (event: PointerEvent) => {
      if (event.target instanceof Node && root.current?.contains(event.target) !== true) {
        setOpen(false);
      }
    };
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setOpen(false);
        button.current?.focus();
      }
    };
    document.addEventListener("pointerdown", outside);
    document.addEventListener("keydown", escape);
    return () => {
      document.removeEventListener("pointerdown", outside);
      document.removeEventListener("keydown", escape);
    };
  }, [open]);

  return (
    <div ref={root} className="relative ml-auto pl-3">
      <button
        ref={button}
        type="button"
        aria-label="Account"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={menuId}
        onClick={() => {
          setOpen((current) => !current);
        }}
        className="grid size-10 cursor-pointer place-items-center rounded-full bg-white/12 text-[15px] font-bold text-white transition-[background-color,transform] duration-150 ease-out hover:bg-white/20 active:scale-95"
      >
        {initial ?? <UserRound className="size-4" aria-hidden />}
      </button>

      <AnimatePresence>
        {open && (
          <motion.div
            id={menuId}
            role="menu"
            data-surface="light"
            initial={{ opacity: 0, y: -6, scale: 0.97 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: -6, scale: 0.97 }}
            transition={{ duration: 0.16, ease: EASE_OUT }}
            className="absolute top-full right-0 mt-2 w-64 origin-top-right rounded-2xl bg-card p-1.5 text-foreground shadow-[0_18px_44px_-16px_rgb(0_0_0/0.4)]"
          >
            <p className="truncate px-3 pt-2 pb-2.5 text-sm text-muted-foreground">
              Signed in as <span className="font-semibold text-foreground">{props.contact}</span>
            </p>
            <button
              type="button"
              role="menuitem"
              onClick={() => {
                setOpen(false);
                void Effect.runPromise(signOut).then(props.onSignOut);
              }}
              className="flex w-full cursor-pointer items-center gap-3 rounded-xl px-3 py-2.5 text-[15px] font-semibold transition-colors duration-100 hover:bg-tile"
            >
              <LogOut className="size-4 text-muted-foreground" aria-hidden />
              Sign out
            </button>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
};
