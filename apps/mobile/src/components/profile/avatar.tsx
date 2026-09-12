import { Text } from "@/components/ui/text.js";
import { colors } from "@/lib/theme.js";
import { cn } from "@/lib/utils.js";
import { useAtomValue } from "@effect/atom-react";
import Ionicons from "@expo/vector-icons/Ionicons";
import { profileAtom } from "@surge/common/atom/profile-atoms";
import { initialOf, nameOr } from "@surge/common/profile/names";
import type { UserId } from "@surge/domain/api/Primitives";
import { AsyncResult } from "effect/unstable/reactivity";
import { Image } from "expo-image";
import * as React from "react";
import { View } from "react-native";

const SIDES = { xs: 28, sm: 36, md: 44, lg: 56, hero: 72, xl: 104 } as const;

export type AvatarSize = keyof typeof SIDES;

/**
 * A face, round, on a grey tile — the web's `Avatar` for the phone: the photo
 * when there is one, fading in as it arrives; the first letter of the name
 * when there is not; a person glyph without either.
 *
 * Decorative: whatever sits beside it says the name, so assistive tech skips
 * it. A photo that fails to load falls back to the letter.
 */
export const Avatar = (props: {
  readonly src?: string | undefined;
  readonly name?: string | undefined;
  readonly size?: AvatarSize;
  readonly className?: string;
}) => {
  const side = SIDES[props.size ?? "md"];
  const [failed, setFailed] = React.useState<string | undefined>(undefined);
  const src = props.src !== undefined && props.src !== "" && props.src !== failed
    ? props.src
    : undefined;
  const initial = initialOf(props.name ?? "");

  return (
    <View
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      className={cn(
        "items-center justify-center overflow-hidden rounded-full bg-tile",
        props.className,
      )}
      style={{ width: side, height: side }}
    >
      {src !== undefined
        ? (
          <Image
            source={{ uri: src }}
            style={{ width: side, height: side }}
            contentFit="cover"
            transition={180}
            cachePolicy="memory-disk"
            onError={() => {
              setFailed(src);
            }}
          />
        )
        : initial !== undefined
        ? (
          <Text className="font-semibold" style={{ fontSize: Math.round(side * 0.4) }}>
            {initial}
          </Text>
        )
        : <Ionicons name="person" size={Math.round(side * 0.48)} color={colors.mutedForeground} />}
    </View>
  );
};

/** Someone's face, read from their profile; the glyph shows while it loads. */
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
