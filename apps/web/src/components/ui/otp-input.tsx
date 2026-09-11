import { cn } from "@/lib/utils";
import { motion } from "motion/react";
import * as React from "react";

/**
 * A one-time code as one box per digit.
 *
 * Each box is a real input, so a screen reader reads "digit 1 of 6", and the
 * keyboard works as people expect of a code field: typing fills the box and
 * moves on, Backspace clears the box in place and then steps back, the arrows
 * move between boxes, and a paste — or a phone's autofill from the text or
 * email that carried the code — fills every box at once. An ink ring slides
 * between boxes with focus.
 *
 * The value is the digits typed so far, with no gaps: focus always lands on
 * the first empty box, so a code is filled in order and a box in the middle
 * cannot be skipped.
 */
export const OtpInput = (props: {
  readonly value: string;
  readonly onChange: (value: string) => void;
  /** Called with the whole code once the last box is filled. */
  readonly onComplete?: ((value: string) => void) | undefined;
  readonly length?: number;
  readonly autoFocus?: boolean;
  readonly disabled?: boolean;
  /** `error` rings the boxes red and shakes them once; `success` rings them green. */
  readonly status?: "idle" | "error" | "success";
  /** What the code is for, as a name or as the id of the visible label. */
  readonly "aria-label"?: string | undefined;
  readonly "aria-labelledby"?: string | undefined;
  readonly "aria-describedby"?: string | undefined;
}) => {
  const length = props.length ?? 6;
  const status = props.status ?? "idle";
  const inputs = React.useRef<Array<HTMLInputElement | null>>([]);
  const [focused, setFocused] = React.useState<number | undefined>(undefined);
  const digits = props.value.replace(/\D/g, "").slice(0, length);
  const nextEmpty = Math.min(digits.length, length - 1);
  const id = React.useId();

  const focus = (index: number) => {
    inputs.current[Math.max(0, Math.min(index, length - 1))]?.focus();
  };

  /**
   * Where focus goes once a change has landed. Moving it in the handler would
   * be too early: the box focused next checks its place against the digits of
   * the render it was made in, which do not yet include the one just typed,
   * and hands focus straight back.
   */
  const pendingFocus = React.useRef<number | undefined>(undefined);
  React.useLayoutEffect(() => {
    if (pendingFocus.current === undefined) return;
    focus(pendingFocus.current);
    pendingFocus.current = undefined;
  });

  /** Commits a value and moves focus to `index` once it shows. */
  const commit = (value: string, index: number) => {
    const clean = value.replace(/\D/g, "").slice(0, length);
    if (clean === digits) {
      focus(index);
      return;
    }
    pendingFocus.current = index;
    props.onChange(clean);
    if (clean.length === length) props.onComplete?.(clean);
  };

  /** Digits arriving in one go — a paste or an autofill — fill from this box on. */
  const fill = (index: number, incoming: string) => {
    const typed = incoming.replace(/\D/g, "");
    if (typed === "") return;
    const next = (digits.slice(0, index) + typed).slice(0, length);
    commit(next, next.length);
  };

  const ring = status === "error"
    ? "var(--destructive)"
    : status === "success"
    ? "var(--success)"
    : "var(--foreground)";

  return (
    <div className="relative" style={{ "--otp-ring": ring } as React.CSSProperties}>
      <div
        role="group"
        aria-label={props["aria-label"]}
        aria-labelledby={props["aria-labelledby"]}
        className="grid gap-2"
        style={{ gridTemplateColumns: `repeat(${length}, minmax(0, 1fr))` }}
      >
        {Array.from({ length }, (_, index) => {
          const digit = digits[index] ?? "";
          const isFocused = focused === index;
          return (
            <span key={index} className="relative">
              {isFocused && (
                <motion.span
                  layoutId={`${id}-caret`}
                  aria-hidden
                  className="pointer-events-none absolute inset-0 rounded-xl shadow-[inset_0_0_0_2px_var(--otp-ring)]"
                  transition={{ type: "spring", stiffness: 600, damping: 40 }}
                />
              )}
              <input
                ref={(element) => {
                  inputs.current[index] = element;
                }}
                type="text"
                inputMode="numeric"
                pattern="[0-9]*"
                // Autofill offers the code on the first box; `fill` spreads it.
                autoComplete={index === 0 ? "one-time-code" : "off"}
                autoFocus={props.autoFocus === true && index === 0}
                disabled={props.disabled}
                aria-label={`Digit ${index + 1} of ${length}`}
                aria-describedby={props["aria-describedby"]}
                aria-invalid={status === "error"}
                value={digit}
                onFocus={(event) => {
                  // A box past the digits typed so far hands focus to the first
                  // empty one, so the code stays contiguous.
                  if (index > nextEmpty) {
                    focus(nextEmpty);
                    return;
                  }
                  setFocused(index);
                  event.target.select();
                }}
                onBlur={() => {
                  setFocused((current) => current === index ? undefined : current);
                }}
                onChange={(event) => {
                  const typed = event.target.value.replace(/\D/g, "");
                  if (typed.length > 1) {
                    fill(index, typed);
                    return;
                  }
                  if (typed === "") {
                    commit(digits.slice(0, index) + digits.slice(index + 1), index);
                    return;
                  }
                  const next = digits.slice(0, index) + typed + digits.slice(index + 1);
                  commit(next, index + 1);
                }}
                onKeyDown={(event) => {
                  if (event.key === "Backspace") {
                    if (digit === "" && index > 0) {
                      event.preventDefault();
                      commit(digits.slice(0, index - 1) + digits.slice(index), index - 1);
                    }
                    // With a digit here the change handler clears it in place.
                  } else if (event.key === "ArrowLeft") {
                    event.preventDefault();
                    focus(index - 1);
                  } else if (event.key === "ArrowRight") {
                    event.preventDefault();
                    focus(Math.min(index + 1, nextEmpty));
                  } else if (event.key === "Home") {
                    event.preventDefault();
                    focus(0);
                  } else if (event.key === "End") {
                    event.preventDefault();
                    focus(nextEmpty);
                  }
                }}
                onPaste={(event) => {
                  event.preventDefault();
                  fill(index, event.clipboardData.getData("text"));
                }}
                className={cn(
                  // The text is transparent: the digit is drawn by the popping
                  // span over it, once.
                  "h-14 w-full rounded-xl bg-tile text-center text-2xl font-bold text-transparent caret-transparent tabular-nums transition-[background-color,box-shadow] duration-150 ease-out outline-none selection:bg-transparent disabled:opacity-50",
                  isFocused && "bg-card",
                  // Every box is ringed while something is wrong or done, and the
                  // focused one always is. The focused box draws its own ring as
                  // well as the sliding one, so it is ringed the moment it has
                  // focus — and after a sixth digit, when the slide can be cut
                  // short by the code being checked, it still is.
                  (status !== "idle" || isFocused) && "shadow-[inset_0_0_0_2px_var(--otp-ring)]",
                  status === "error" && "motion-safe:animate-shake",
                )}
                style={status === "success" ? { transitionDelay: `${index * 40}ms` } : undefined}
              />
              {
                /* A digit pops in as it lands; the input's own text is drawn on
                  top of it, so the pop is the only motion. */
              }
              {digit !== "" && (
                <motion.span
                  key={`${index}-${digit}`}
                  aria-hidden
                  initial={{ scale: 0.6, opacity: 0 }}
                  animate={{ scale: 1, opacity: 1 }}
                  transition={{ type: "spring", stiffness: 600, damping: 30 }}
                  className="pointer-events-none absolute inset-0 grid place-items-center text-2xl font-bold text-foreground tabular-nums"
                >
                  {digit}
                </motion.span>
              )}
              {isFocused && digit === "" && (
                <span
                  aria-hidden
                  className="pointer-events-none absolute top-1/2 left-1/2 h-6 w-0.5 -translate-x-1/2 -translate-y-1/2 rounded-full bg-foreground motion-safe:animate-pulse"
                />
              )}
            </span>
          );
        })}
      </div>
    </div>
  );
};
