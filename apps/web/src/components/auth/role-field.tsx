import { cn } from "@/lib/utils.js";
import type { SignUpRole } from "@surge/common/iam/auth-schemas";
import { Option } from "effect";
import { Car, Check, MapPin } from "lucide-react";
import { motion } from "motion/react";
import * as React from "react";

const ROLES: ReadonlyArray<
  {
    readonly value: SignUpRole;
    readonly label: string;
    readonly description: string;
    readonly icon: typeof Car;
  }
> = [
  { value: "rider", label: "Ride", description: "Book trips across Amsterdam.", icon: MapPin },
  { value: "driver", label: "Drive", description: "Take trips while on shift.", icon: Car },
];

/**
 * The rider/driver choice, as one effect-form field: two tiles like the
 * rider's class tiles, the chosen one yellow, each with a radio mark that pops
 * a check when chosen.
 *
 * A radio group validated as a unit, per `knowledge/rules/form-validation-message.md`:
 * one message for the group, none on the individual options. It always has a
 * value — the form starts on `rider` — so in practice it never shows one; the
 * branch exists so the shape stays honest if that default ever goes away.
 *
 * Real radio inputs inside labels, so the whole tile is the click target and
 * keyboard selection works without a line of handling here.
 */
export const roleField = (options: { readonly legend: string; }) =>
(props: {
  readonly field: {
    readonly value: SignUpRole;
    readonly onChange: (value: SignUpRole) => void;
    readonly onBlur: () => void;
    readonly error: Option.Option<string>;
    readonly isDirty: boolean;
  };
  readonly props: { readonly submitted?: boolean; };
}) => {
  const name = React.useId();
  const errorId = `${name}-error`;
  const reveal = props.field.isDirty || props.props.submitted === true;
  const error = reveal ? props.field.error : Option.none();

  return (
    <fieldset
      className="flex flex-col gap-2"
      aria-invalid={Option.isSome(error)}
      aria-describedby={Option.isSome(error) ? errorId : undefined}
    >
      <legend className="mb-2 text-[13px] font-semibold text-muted-foreground">
        {options.legend}
      </legend>
      <div className="grid grid-cols-2 gap-2">
        {ROLES.map((role) => {
          const checked = props.field.value === role.value;
          const Icon = role.icon;
          return (
            <label
              key={role.value}
              className={cn(
                "flex cursor-pointer flex-col gap-2.5 rounded-2xl p-3.5 transition-[background-color,transform] duration-150 ease-out active:scale-[0.98] has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-foreground",
                checked ? "bg-primary text-primary-foreground" : "bg-tile hover:bg-tile-hover",
              )}
            >
              <input
                type="radio"
                name={name}
                value={role.value}
                checked={checked}
                onChange={() => props.field.onChange(role.value)}
                onBlur={props.field.onBlur}
                className="sr-only"
              />
              <span className="flex items-center justify-between">
                <span aria-hidden className="grid size-9 place-items-center rounded-full bg-card">
                  <Icon className="size-4" />
                </span>
                <span
                  aria-hidden
                  className={cn(
                    "grid size-5 place-items-center rounded-full transition-colors duration-150",
                    checked ? "bg-foreground text-primary" : "ring-2 ring-foreground/20 ring-inset",
                  )}
                >
                  {checked && (
                    <motion.span
                      initial={{ scale: 0.4, opacity: 0 }}
                      animate={{ scale: 1, opacity: 1 }}
                      transition={{ type: "spring", stiffness: 600, damping: 28 }}
                    >
                      <Check className="size-3" strokeWidth={3.5} />
                    </motion.span>
                  )}
                </span>
              </span>
              <span className="flex flex-col gap-0.5">
                <span className="text-base font-semibold">{role.label}</span>
                <span className="text-[13px] leading-snug opacity-70">{role.description}</span>
              </span>
            </label>
          );
        })}
      </div>
      {Option.isSome(error) && (
        <p id={errorId} className="text-[13px] font-medium text-destructive">{error.value}</p>
      )}
    </fieldset>
  );
};
