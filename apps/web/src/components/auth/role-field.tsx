import { cn } from "@/lib/utils.js";
import type { SignUpRole } from "@surge/common/iam/auth-schemas";
import { Option } from "effect";
import * as React from "react";

const ROLES: ReadonlyArray<
  { readonly value: SignUpRole; readonly label: string; readonly description: string; }
> = [
  { value: "rider", label: "Rider", description: "Book trips." },
  { value: "driver", label: "Driver", description: "Get offered them." },
];

/**
 * The rider/driver choice, as one effect-form field.
 *
 * A radio group validated as a unit, per `knowledge/rules/form-validation-message.md`:
 * one message for the group, none on the individual options. It always has a
 * value — the form starts on `rider` — so in practice it never shows one; the
 * branch exists so the shape stays honest if that default ever goes away.
 *
 * Real radio inputs inside labels, so the whole card is the click target and
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
  const reveal = props.field.isDirty || props.props.submitted === true;
  const error = reveal ? props.field.error : Option.none();

  return (
    <fieldset className="flex flex-col gap-2" aria-invalid={Option.isSome(error)}>
      <legend className="mb-2 text-sm font-medium">{options.legend}</legend>
      <div className="grid grid-cols-2 gap-2">
        {ROLES.map((role) => (
          <label
            key={role.value}
            className={cn(
              "flex cursor-pointer flex-col gap-0.5 rounded-md border px-3 py-2 text-sm",
              props.field.value === role.value ? "border-primary" : "border-border",
            )}
          >
            <span className="flex items-center gap-2">
              <input
                type="radio"
                name={name}
                value={role.value}
                checked={props.field.value === role.value}
                onChange={() => props.field.onChange(role.value)}
                onBlur={props.field.onBlur}
                className="accent-primary"
              />
              {role.label}
            </span>
            <span className="text-muted-foreground text-xs">{role.description}</span>
          </label>
        ))}
      </div>
      {Option.isSome(error) && <p className="text-destructive text-xs">{error.value}</p>}
    </fieldset>
  );
};
