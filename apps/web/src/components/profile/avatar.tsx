import { cn } from "@/lib/utils.js";
import { useAtomValue } from "@effect/atom-react";
import { profileAtom } from "@surge/common/atom/profile-atoms";
import { initialOf, nameOr } from "@surge/common/profile/names";
import type { UserId } from "@surge/domain/api/Primitives";
import { AsyncResult } from "effect/unstable/reactivity";
import { UserRound } from "lucide-react";
import * as React from "react";

const sizes = {
  xs: "size-7 text-xs",
  sm: "size-9 text-sm",
  md: "size-11 text-base",
  lg: "size-14 text-xl",
  xl: "size-24 text-4xl",
} as const;

export type AvatarSize = keyof typeof sizes;

/**
 * A face, round, on a grey tile: the photo when there is one, the first
 * letter of the name when there is not, and a person glyph without either.
 *
 * Decorative — whatever sits beside it says the name — so hidden from
 * assistive tech. A photo that fails to load falls back to the letter rather
 * than a broken image.
 */
export const Avatar = (props: {
  readonly src?: string | undefined;
  readonly name?: string | undefined;
  readonly size?: AvatarSize;
  readonly className?: string;
}) => {
  const [failed, setFailed] = React.useState<string | undefined>(undefined);
  const initial = initialOf(props.name ?? "");
  const src = props.src !== undefined && props.src !== "" && props.src !== failed
    ? props.src
    : undefined;

  return (
    <span
      aria-hidden
      className={cn(
        "relative grid shrink-0 place-items-center overflow-hidden rounded-full bg-tile font-bold text-foreground select-none",
        sizes[props.size ?? "md"],
        props.className,
      )}
    >
      {src !== undefined
        ? (
          <img
            src={src}
            alt=""
            draggable={false}
            onError={() => {
              setFailed(src);
            }}
            className="size-full object-cover"
          />
        )
        : initial ?? <UserRound className="size-1/2 text-muted-foreground" />}
    </span>
  );
};

/** Someone's face, read from their profile. The glyph shows while it loads. */
export const PersonAvatar = (props: {
  readonly userId: UserId;
  readonly size?: AvatarSize;
  readonly className?: string;
}) => {
  const profile = useAtomValue(profileAtom(props.userId));
  const value = AsyncResult.isSuccess(profile) ? profile.value : undefined;
  return (
    <Avatar
      src={value?.avatarUrl}
      name={value?.displayName}
      {...(props.size === undefined ? {} : { size: props.size })}
      {...(props.className === undefined ? {} : { className: props.className })}
    />
  );
};

/** Someone's first name, or their role until they have given one. */
export const usePersonName = (userId: UserId, role: string): string => {
  const profile = useAtomValue(profileAtom(userId));
  return nameOr(AsyncResult.isSuccess(profile) ? profile.value : undefined, role);
};

/** The same, where a component is easier to place than a hook. */
export const PersonName = (props: { readonly userId: UserId; readonly role: string; }) => (
  <>{usePersonName(props.userId, props.role)}</>
);
