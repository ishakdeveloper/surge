import { ActionSheet } from "@/components/app/action-sheet.js";
import { ActionError, QueryError } from "@/components/app/errors.js";
import { PressableScale } from "@/components/motion/pressable-scale.js";
import { Avatar } from "@/components/profile/avatar.js";
import { Input } from "@/components/ui/input.js";
import { Text } from "@/components/ui/text.js";
import { pickPhoto } from "@/lib/avatar-image.js";
import { haptic } from "@/lib/haptics.js";
import { colors } from "@/lib/theme.js";
import { cn } from "@/lib/utils.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import Ionicons from "@expo/vector-icons/Ionicons";
import {
  myProfileAtom,
  removeAvatar,
  renameProfile,
  uploadAvatar,
} from "@surge/common/atom/profile-atoms";
import type { Role } from "@surge/domain/iam/Identity";
import type { Profile } from "@surge/domain/profile/Profile";
import { AsyncResult } from "effect/unstable/reactivity";
import * as React from "react";
import { ActivityIndicator, View } from "react-native";

/** What the photo is for, in the words of the person it is shown to. */
const WHY: Record<Role, string> = {
  rider: "Your driver sees your first name and photo, so they know who they are picking up.",
  driver: "Riders look for your face and your name at the kerb. A clear photo of you, not the car.",
  ops: "Riders and drivers see your first name and photo when you answer them from support.",
};

/**
 * The name and the photo someone is seen by, edited where they stand — the
 * web's `ProfileEditor` for the phone: a large face that opens "take a photo
 * or choose one", and the first name, saved when the field is left or the
 * keyboard's Done is pressed, so there is no Save to forget.
 */
export const ProfileEditor = (props: { readonly role: Role; readonly explain?: boolean; }) => {
  const profile = useAtomValue(myProfileAtom);
  if (AsyncResult.isInitial(profile)) {
    return <Text className="py-6 text-muted-foreground">Loading your profile…</Text>;
  }
  if (AsyncResult.isFailure(profile)) {
    return <QueryError result={profile} subject="your profile" />;
  }
  return (
    <View className="gap-6">
      <PhotoField profile={profile.value} />
      <NameField profile={profile.value} />
      {props.explain !== false && (
        <Text className="text-[13px] leading-5 text-muted-foreground">{WHY[props.role]}</Text>
      )}
    </View>
  );
};

const PhotoField = (props: { readonly profile: Profile; }) => {
  const uploading = useAtomValue(uploadAvatar);
  const upload = useAtomSet(uploadAvatar);
  const removing = useAtomValue(removeAvatar);
  const remove = useAtomSet(removeAvatar);
  const [asking, setAsking] = React.useState(false);
  const [preview, setPreview] = React.useState<string | undefined>(undefined);
  const [problem, setProblem] = React.useState<string | undefined>(undefined);

  React.useEffect(() => {
    if (uploading.waiting) return;
    setPreview(undefined);
    if (AsyncResult.isSuccess(uploading)) haptic.success();
  }, [uploading]);

  const choose = async (source: "library" | "camera") => {
    setProblem(undefined);
    try {
      const picked = await pickPhoto(source);
      if (picked._tag === "Denied") {
        setProblem("Camera access is off for Surge. Allow it in Settings, or choose a photo.");
        return;
      }
      if (picked._tag === "Cancelled") return;
      setPreview(picked.preview);
      upload(picked.base64);
    } catch {
      setProblem("That photo could not be read. Try another.");
    }
  };

  const busy = uploading.waiting || removing.waiting;
  const hasPhoto = props.profile.avatarUrl !== "";

  return (
    <View className="items-center gap-3">
      <PressableScale
        accessibilityLabel={hasPhoto ? "Change your photo" : "Add a photo"}
        disabled={busy}
        scaleTo={0.95}
        onPress={() => {
          setAsking(true);
        }}
      >
        <Avatar
          src={preview ?? props.profile.avatarUrl}
          name={props.profile.displayName}
          size="xl"
          className={cn(busy && "opacity-60")}
        />
        {/* The camera badge on the face's edge says it can be changed. */}
        <View className="absolute right-0 bottom-0 size-9 items-center justify-center rounded-full border-[3px] border-card bg-primary">
          {busy
            ? <ActivityIndicator size="small" color={colors.foreground} />
            : <Ionicons name="camera" size={16} color={colors.foreground} />}
        </View>
      </PressableScale>
      <Text className="text-[13px] font-semibold text-muted-foreground">
        {uploading.waiting
          ? "Saving your photo…"
          : removing.waiting
          ? "Removing…"
          : hasPhoto
          ? "Tap to change"
          : "Add a photo"}
      </Text>
      {problem !== undefined && (
        <Text accessibilityRole="alert" className="text-center text-[13px] text-destructive">
          {problem}
        </Text>
      )}
      {AsyncResult.isFailure(uploading) && !uploading.waiting && (
        <ActionError cause={uploading.cause} />
      )}
      {AsyncResult.isFailure(removing) && !removing.waiting && (
        <ActionError cause={removing.cause} />
      )}

      <ActionSheet
        visible={asking}
        title={hasPhoto ? "Change your photo" : "Add a photo"}
        message="A clear, recent photo of your face."
        onClose={() => {
          setAsking(false);
        }}
        options={[
          { label: "Take a photo", icon: "camera-outline", onPress: () => void choose("camera") },
          {
            label: "Choose from your photos",
            icon: "images-outline",
            onPress: () => void choose("library"),
          },
          ...(hasPhoto
            ? [{
              label: "Remove photo",
              icon: "trash-outline" as const,
              danger: true,
              onPress: () => {
                remove();
              },
            }]
            : []),
        ]}
      />
    </View>
  );
};

const NameField = (props: { readonly profile: Profile; }) => {
  const saving = useAtomValue(renameProfile);
  const rename = useAtomSet(renameProfile);
  const saved = props.profile.displayName;
  const [value, setValue] = React.useState(saved);
  const [empty, setEmpty] = React.useState(false);
  const editing = React.useRef(false);

  // A name saved elsewhere shows here too, unless it is being edited.
  React.useEffect(() => {
    if (!editing.current) setValue(saved);
  }, [saved]);

  // A sheet swiped away with the field still focused never blurs it; what was
  // typed is saved as it goes rather than lost.
  const latest = React.useRef({ value, saved });
  latest.current = { value, saved };
  React.useEffect(() => () => {
    const name = latest.current.value.trim();
    if (name !== "" && name !== latest.current.saved) rename(name);
  }, [rename]);

  const commit = () => {
    editing.current = false;
    const name = value.trim();
    if (name === "") {
      setEmpty(saved !== "" || value !== "");
      return;
    }
    setEmpty(false);
    if (name !== saved) rename(name);
  };

  return (
    <View className="gap-2">
      <Text className="text-[13px] font-semibold text-muted-foreground">First name</Text>
      <Input
        testID="field-first-name"
        accessibilityLabel="First name"
        accessibilityHint={empty ? "Enter the first name riders and drivers will see." : undefined}
        value={value}
        maxLength={40}
        autoComplete="given-name"
        textContentType="givenName"
        autoCapitalize="words"
        returnKeyType="done"
        invalid={empty}
        onFocus={() => {
          editing.current = true;
        }}
        onChangeText={(text) => {
          setValue(text);
          if (empty && text.trim() !== "") setEmpty(false);
        }}
        onBlur={commit}
        onSubmitEditing={commit}
      />
      {empty
        ? (
          <Text accessibilityRole="alert" className="text-[13px] font-medium text-destructive">
            Enter the first name riders and drivers will see.
          </Text>
        )
        : (
          <Text accessibilityLiveRegion="polite" className="text-[13px] text-muted-foreground">
            {saving.waiting
              ? "Saving…"
              : AsyncResult.isSuccess(saving) && saved !== ""
              ? "Saved."
              : "Saved as you leave the field."}
          </Text>
        )}
      {AsyncResult.isFailure(saving) && !saving.waiting && <ActionError cause={saving.cause} />}
    </View>
  );
};
