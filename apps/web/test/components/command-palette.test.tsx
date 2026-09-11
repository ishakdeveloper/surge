import { CommandPalette } from "@/components/app/command-palette.js";
import { nav } from "@/components/app/top-bar.js";
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

/**
 * A router is needed for `useNavigate`. What is asserted below is the part that
 * must work with no data at all: the keybinding, and every page the top bar
 * can offer.
 */
const renderPalette = async () => {
  const root = createRootRoute({ component: () => <CommandPalette /> });
  const router = createRouter({
    routeTree: root,
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });

  await router.load();

  return render(<RouterProvider router={router as never} />);
};

const dialog = () => screen.queryByRole("dialog");

describe("CommandPalette", () => {
  it("stays shut until asked for", async () => {
    await renderPalette();

    expect(dialog()).not.toBeInTheDocument();
  });

  it("opens on the meta chord and closes on the same one", async () => {
    await renderPalette();

    await userEvent.keyboard("{Meta>}k{/Meta}");
    expect(dialog()).toBeInTheDocument();

    await userEvent.keyboard("{Meta>}k{/Meta}");
    expect(dialog()).not.toBeInTheDocument();
  });

  /** Ctrl as well as Cmd, so the shortcut is not macOS-only. */
  it("opens on the control chord too", async () => {
    await renderPalette();

    await userEvent.keyboard("{Control>}k{/Control}");

    expect(dialog()).toBeInTheDocument();
  });

  /**
   * Driven by the same `nav` array the top bar renders from rather than by a
   * hand-typed list, so it keeps holding as Phase 4 adds /console, /ride and
   * /drive. Failing means the palette and the navigation have drifted apart.
   */
  it("offers every page the navigation does", async () => {
    await renderPalette();

    await userEvent.keyboard("{Meta>}k{/Meta}");

    for (const { label } of nav) {
      expect(screen.getByRole("option", { name: label })).toBeInTheDocument();
    }
  });

  it("filters to what was typed", async () => {
    await renderPalette();

    await userEvent.keyboard("{Meta>}k{/Meta}");

    const first = nav[0]!;
    expect(screen.getByRole("option", { name: first.label })).toBeInTheDocument();

    // A query nothing matches, so this asserts filtering rather than asserting
    // that one page outranks another — which a single-entry nav cannot show.
    await userEvent.type(screen.getByRole("combobox"), "zzzznotapage");

    expect(screen.queryByRole("option", { name: first.label })).not.toBeInTheDocument();
    expect(screen.getByText("Nothing matches that.")).toBeInTheDocument();
  });
});
