import { SurgeMark } from "@/components/app/surge-mark.js";
import { Button } from "@/components/ui/button.js";
import { Link } from "@tanstack/react-router";
import type * as React from "react";

/**
 * The auth sheet's contents: the mark and the city, a heading that says what
 * this step is for, and the step itself.
 */
export const AuthCard = (props: {
  readonly title: string;
  readonly description: string;
  readonly children: React.ReactNode;
}) => (
  <div className="flex flex-col gap-7">
    <div className="flex items-center gap-2.5">
      <SurgeMark className="size-9" />
      <span className="text-xl font-bold">Surge</span>
      <span className="rounded-full bg-tile px-2.5 py-1 text-xs font-semibold text-muted-foreground">
        Amsterdam
      </span>
    </div>
    <header className="flex flex-col gap-2">
      <h1 className="text-[28px] leading-[1.15] font-bold text-balance">{props.title}</h1>
      <p className="text-[15px] text-pretty text-muted-foreground">{props.description}</p>
    </header>
    {props.children}
  </div>
);

/** Navigation between auth pages uses real links, never buttons. */
export const AuthLink = (props: { readonly to: string; readonly children: React.ReactNode; }) => (
  <Link to={props.to} className="font-semibold text-foreground underline underline-offset-4">
    {props.children}
  </Link>
);

/** Google's own mark, as its sign-in guidelines ask a Google button to carry. */
const GoogleMark = () => (
  <svg viewBox="0 0 24 24" className="size-5" aria-hidden>
    <path
      fill="#4285F4"
      d="M23.52 12.27c0-.85-.08-1.67-.22-2.45H12v4.64h6.46a5.52 5.52 0 0 1-2.4 3.62v3h3.88c2.27-2.09 3.58-5.17 3.58-8.81z"
    />
    <path
      fill="#34A853"
      d="M12 24c3.24 0 5.96-1.07 7.94-2.91l-3.88-3c-1.07.72-2.45 1.15-4.06 1.15-3.12 0-5.77-2.11-6.71-4.95H1.28v3.1A12 12 0 0 0 12 24z"
    />
    <path
      fill="#FBBC05"
      d="M5.29 14.29a7.2 7.2 0 0 1 0-4.58v-3.1H1.28a12 12 0 0 0 0 10.78l4.01-3.1z"
    />
    <path
      fill="#EA4335"
      d="M12 4.77c1.76 0 3.34.61 4.59 1.8l3.44-3.44A11.97 11.97 0 0 0 12 0 12 12 0 0 0 1.28 6.61l4.01 3.1C6.23 6.88 8.88 4.77 12 4.77z"
    />
  </svg>
);

export const GoogleButton = (props: { readonly onClick: () => void; }) => (
  <Button type="button" variant="outline" size="lg" className="w-full" onClick={props.onClick}>
    <GoogleMark />
    Continue with Google
  </Button>
);
