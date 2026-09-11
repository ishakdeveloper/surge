import { Input } from "@/components/ui/input.js";
import { Label } from "@/components/ui/label.js";
import { OtpInput } from "@/components/ui/otp-input.js";
import { Option } from "effect";
import { motion } from "motion/react";
import * as React from "react";

/** What effect-form hands a field renderer. */
interface FieldProps {
  readonly field: {
    readonly value: string;
    readonly onChange: (value: string) => void;
    readonly onBlur: () => void;
    readonly error: Option.Option<string>;
    readonly isDirty: boolean;
  };
  readonly props: { readonly submitted?: boolean; };
}

/**
 * The error to show, if any. Errors stay hidden until the user has actually
 * typed something — blurring a field they never filled in should not accuse
 * them of anything. `submitted` overrides that once a submit has been
 * attempted, so pressing the button on an empty form marks what is missing.
 */
const shownError = (props: FieldProps) =>
  props.field.isDirty || props.props.submitted === true ? props.field.error : Option.none();

/** One inline message under a field, tied to it by `aria-describedby`. */
const FieldError = (props: { readonly id: string; readonly message: string; }) => (
  <motion.p
    id={props.id}
    initial={{ opacity: 0, y: -4 }}
    animate={{ opacity: 1, y: 0 }}
    transition={{ duration: 0.16 }}
    className="text-[13px] font-medium text-destructive"
  >
    {props.message}
  </motion.p>
);

/** Renders one effect-form text field: its label, the grey tile, and its error. */
export const textField = (options: {
  readonly label: string;
  readonly type?: "text" | "email" | "tel";
  readonly autoComplete?: string;
  /** Which keyboard a phone shows — `numeric` for a code. */
  readonly inputMode?: React.HTMLAttributes<HTMLInputElement>["inputMode"];
}) =>
(props: FieldProps) => {
  const id = React.useId();
  const errorId = `${id}-error`;
  const error = shownError(props);

  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>{options.label}</Label>
      <Input
        id={id}
        type={options.type ?? "text"}
        autoComplete={options.autoComplete}
        inputMode={options.inputMode}
        value={props.field.value}
        onChange={(event) => props.field.onChange(event.target.value)}
        onBlur={props.field.onBlur}
        aria-invalid={Option.isSome(error)}
        aria-describedby={Option.isSome(error) ? errorId : undefined}
      />
      {Option.isSome(error) && <FieldError id={errorId} message={error.value} />}
    </div>
  );
};

/**
 * The one-time code, as `OtpInput`'s six boxes. The page passes `onComplete`
 * so the last digit submits the form by itself, and `refused` when the server
 * turned a code down, which rings the boxes red and shakes them once.
 */
export const codeField = (options: { readonly label: string; }) =>
(
  props: FieldProps & {
    readonly props: {
      readonly submitted?: boolean;
      readonly refused?: boolean;
      readonly onComplete?: (code: string) => void;
    };
  },
) => {
  const id = React.useId();
  const errorId = `${id}-error`;
  const error = shownError(props);
  const refused = Option.isSome(error) || props.props.refused === true;

  return (
    <div className="flex flex-col gap-2">
      {/* Six inputs share this one name, so it names the group rather than a control. */}
      <span
        id={`${id}-label`}
        className="text-[13px] leading-none font-semibold text-muted-foreground"
      >
        {options.label}
      </span>
      <OtpInput
        value={props.field.value}
        onChange={props.field.onChange}
        onComplete={props.props.onComplete}
        autoFocus
        status={refused ? "error" : "idle"}
        aria-labelledby={`${id}-label`}
        aria-describedby={Option.isSome(error) ? errorId : undefined}
      />
      {Option.isSome(error) && <FieldError id={errorId} message={error.value} />}
    </div>
  );
};
