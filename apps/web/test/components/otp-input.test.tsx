import { OtpInput } from "@/components/ui/otp-input.js";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import * as React from "react";
import { describe, expect, it, vi } from "vitest";

/**
 * The keyboard contract of the code boxes, which is what makes them a code
 * field rather than six text fields: typing moves on, Backspace clears and
 * then steps back, the arrows move, and a paste fills the lot.
 */
const Harness = (props: { readonly onComplete?: (code: string) => void; }) => {
  const [value, setValue] = React.useState("");
  return (
    <OtpInput value={value} onChange={setValue} onComplete={props.onComplete} aria-label="Code" />
  );
};

const boxes = () => screen.getAllByRole("textbox") as Array<HTMLInputElement>;
const values = () => boxes().map((box) => box.value).join("");

describe("OtpInput", () => {
  it("fills a box per keystroke and moves to the next", async () => {
    render(<Harness />);
    await userEvent.click(boxes()[0]!);
    await userEvent.keyboard("123");

    expect(values()).toBe("123");
    expect(boxes()[3]).toHaveFocus();
  });

  it("clears in place on Backspace, then steps back on the next press", async () => {
    render(<Harness />);
    await userEvent.click(boxes()[0]!);
    await userEvent.keyboard("12");
    // Focus is on the empty third box: stepping back clears the "2".
    await userEvent.keyboard("{Backspace}");
    expect(values()).toBe("1");
    expect(boxes()[1]).toHaveFocus();
  });

  it("moves between boxes with the arrows, never past the digits typed", async () => {
    render(<Harness />);
    await userEvent.click(boxes()[0]!);
    await userEvent.keyboard("12");
    await userEvent.keyboard("{ArrowLeft}{ArrowLeft}");
    expect(boxes()[0]).toHaveFocus();
    await userEvent.keyboard("{ArrowRight}{ArrowRight}{ArrowRight}{ArrowRight}");
    // Box 2 is the first empty one; the arrow stops there.
    expect(boxes()[2]).toHaveFocus();
  });

  it("fills every box from a paste and reports the whole code once", async () => {
    const onComplete = vi.fn();
    render(<Harness onComplete={onComplete} />);
    await userEvent.click(boxes()[0]!);
    await userEvent.paste("48 21-97");

    expect(values()).toBe("482197");
    expect(onComplete).toHaveBeenCalledWith("482197");
    expect(onComplete).toHaveBeenCalledTimes(1);
  });

  it("ignores anything that is not a digit", async () => {
    render(<Harness />);
    await userEvent.click(boxes()[0]!);
    await userEvent.keyboard("a1b2");

    expect(values()).toBe("12");
  });
});
