import { nav } from "@/components/app/top-bar.js";
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command.js";
import type { LinkProps } from "@tanstack/react-router";
import { useNavigate } from "@tanstack/react-router";
import * as React from "react";

/**
 * ⌘K, over destinations and records.
 *
 * The "go to" group is built from the same array that drives the sidebar,
 * exported for exactly this reason — a hand-typed second list is how a page ends
 * up reachable from one and not the other.
 *
 * On `RULES.md`'s rule that navigation must use real links: a `CommandItem` is not
 * an anchor, so these call `navigate`. That is acceptable here because the palette
 * is a keyboard accelerator over destinations which all remain real `<Link>`s in
 * the sidebar — it is not the only route to any of them.
 *
 * Mounted inside the signed-in shell, not the root: a palette that searched a
 * rider's trips from the sign-in page would be a bug, not a feature.
 */
export const CommandPalette = () => {
  const [open, setOpen] = React.useState(false);
  const navigate = useNavigate();

  React.useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      // Ctrl as well as Cmd, so it works away from macOS.
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setOpen((current) => !current);
      }
    };

    globalThis.addEventListener("keydown", onKeyDown);

    return () => {
      globalThis.removeEventListener("keydown", onKeyDown);
    };
  }, []);

  const go = (to: NonNullable<LinkProps["to"]>) => {
    setOpen(false);
    void navigate({ to });
  };

  return (
    <CommandDialog open={open} onOpenChange={setOpen}>
      {
        /* Base UI's CommandDialog is only the dialog — unlike the Radix one it
          does not wrap its children in a Command root, so cmdk's context has to
          be supplied here or every child throws. */
      }
      <Command>
        <CommandInput placeholder="Search pages…" />
        <CommandList>
          <CommandEmpty>Nothing matches that.</CommandEmpty>

          <CommandGroup heading="Go to">
            {nav.map(({ icon: Icon, label, to }) => (
              <CommandItem
                key={to}
                value={`go ${label}`}
                onSelect={() => {
                  go(to);
                }}
              >
                <Icon className="size-4" aria-hidden />
                {label}
              </CommandItem>
            ))}
          </CommandGroup>
        </CommandList>
      </Command>
    </CommandDialog>
  );
};
