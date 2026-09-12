import { ActionError } from "@/components/app/action-error.js";
import { QueryError } from "@/components/app/query-error.js";
import { Avatar } from "@/components/profile/avatar.js";
import { actionVariants } from "@/components/sign/sign.js";
import { Input } from "@/components/ui/input.js";
import { Label } from "@/components/ui/label.js";
import { preparePhoto, UnreadablePhoto } from "@/lib/avatar-image.js";
import { cn } from "@/lib/utils.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import {
  myProfileAtom,
  removeAvatar,
  renameProfile,
  uploadAvatar,
} from "@surge/common/atom/profile-atoms";
import type { Role } from "@surge/domain/iam/Identity";
import type { Profile } from "@surge/domain/profile/Profile";
import { AsyncResult } from "effect/unstable/reactivity";
import { Camera, LoaderCircle } from "lucide-react";
import * as React from "react";

/** What the photo is for, in the words of the person it is shown to. */
const why: Record<Role, string> = {
  rider: "Your driver sees your first name and photo, so they know who they are picking up.",
  driver: "Riders look for your face and your name at the kerb. A clear photo of you, not the car.",
  ops: "Riders and drivers see your first name and photo when you answer them from support.",
};

/**
 * The name and the photo someone is seen by, edited where they stand: a large
 * face with a way to change or remove it, and the first name, saved when the
 * field is left or Enter is pressed — so nothing typed is ever lost by
 * leaving, and there is no Save to forget.
 */
export const ProfileEditor = (props: {
  readonly role: Role;
  /** Say what the name and photo are for. Off where the page already has. */
  readonly explain?: boolean;
}) => {
  const profile = useAtomValue(myProfileAtom);
  if (AsyncResult.isInitial(profile)) {
    return <p className="py-6 text-sm text-muted-foreground">Loading your profile…</p>;
  }
  if (AsyncResult.isFailure(profile)) {
    return <QueryError result={profile} subject="your profile" />;
  }
  return (
    <div className="flex flex-col gap-6">
      <PhotoField profile={profile.value} />
      <NameField profile={profile.value} />
      {props.explain !== false && (
        <p className="text-[13px] text-pretty text-muted-foreground">{why[props.role]}</p>
      )}
    </div>
  );
};

const PhotoField = (props: { readonly profile: Profile; }) => {
  const uploading = useAtomValue(uploadAvatar);
  const upload = useAtomSet(uploadAvatar);
  const removing = useAtomValue(removeAvatar);
  const remove = useAtomSet(removeAvatar);
  const input = React.useRef<HTMLInputElement>(null);
  // The photo as chosen, shown while it travels; the server's link replaces it.
  const [preview, setPreview] = React.useState<string | undefined>(undefined);
  const [unreadable, setUnreadable] = React.useState<string | undefined>(undefined);
  const id = React.useId();

  React.useEffect(() => {
    if (!uploading.waiting) setPreview(undefined);
  }, [uploading.waiting]);

  const choose = async (file: File) => {
    setUnreadable(undefined);
    try {
      const photo = await preparePhoto(file);
      setPreview(photo.preview);
      upload(photo.base64);
    } catch (error) {
      setUnreadable(
        error instanceof UnreadablePhoto
          ? "This browser cannot read that file as a photo. Choose a JPEG, PNG or WebP one."
          : "That photo could not be prepared. Try another.",
      );
    }
  };

  const busy = uploading.waiting || removing.waiting;
  const hasPhoto = props.profile.avatarUrl !== "";

  return (
    <div className="flex items-center gap-4">
      <div className="relative">
        <Avatar
          src={preview ?? props.profile.avatarUrl}
          name={props.profile.displayName}
          size="xl"
          className={cn(busy && "opacity-60")}
        />
        {busy && (
          <span className="absolute inset-0 grid place-items-center">
            <LoaderCircle className="size-6 motion-safe:animate-spin" aria-hidden />
          </span>
        )}
      </div>
      <div className="flex min-w-0 flex-1 flex-col items-start gap-2">
        <input
          ref={input}
          id={id}
          type="file"
          accept="image/jpeg,image/png,image/webp,image/heic"
          className="sr-only"
          onChange={(event) => {
            const file = event.target.files?.[0];
            // Cleared, so choosing the same file again still counts as a choice.
            event.target.value = "";
            if (file !== undefined) void choose(file);
          }}
        />
        <button
          type="button"
          disabled={busy}
          onClick={() => input.current?.click()}
          className={actionVariants({ tone: hasPhoto ? "quiet" : "primary", size: "md" })}
        >
          <Camera className="size-4" aria-hidden />
          {uploading.waiting ? "Saving your photo…" : hasPhoto ? "Change photo" : "Add a photo"}
        </button>
        {hasPhoto && !busy && (
          <button
            type="button"
            onClick={() => {
              remove();
            }}
            className="cursor-pointer px-5 text-[13px] font-semibold text-muted-foreground transition-colors hover:text-destructive"
          >
            Remove photo
          </button>
        )}
        <output aria-live="polite" className="w-full">
          {unreadable !== undefined && (
            <p role="alert" className="text-[13px] font-medium text-destructive">{unreadable}</p>
          )}
          {AsyncResult.isFailure(uploading) && !uploading.waiting && (
            <ActionError cause={uploading.cause} />
          )}
          {AsyncResult.isFailure(removing) && !removing.waiting && (
            <ActionError cause={removing.cause} />
          )}
        </output>
      </div>
    </div>
  );
};

const NameField = (props: { readonly profile: Profile; }) => {
  const saving = useAtomValue(renameProfile);
  const rename = useAtomSet(renameProfile);
  const [value, setValue] = React.useState(props.profile.displayName);
  const [empty, setEmpty] = React.useState(false);
  const id = React.useId();
  const errorId = `${id}-error`;
  const statusId = `${id}-status`;
  const saved = props.profile.displayName;

  // A name saved elsewhere — another tab, the onboarding — shows here too,
  // unless it is being edited.
  const editing = React.useRef(false);
  React.useEffect(() => {
    if (!editing.current) setValue(saved);
  }, [saved]);

  const commit = () => {
    editing.current = false;
    const name = value.trim();
    if (name === "") {
      // Only an attempt to clear a name is refused; an untouched empty field is fine.
      setEmpty(saved !== "" || value !== "");
      return;
    }
    setEmpty(false);
    if (name !== saved) rename(name);
  };

  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>First name</Label>
      <Input
        id={id}
        value={value}
        maxLength={40}
        autoComplete="given-name"
        aria-invalid={empty}
        aria-describedby={empty ? errorId : statusId}
        onFocus={() => {
          editing.current = true;
        }}
        onChange={(event) => {
          setValue(event.target.value);
          if (empty && event.target.value.trim() !== "") setEmpty(false);
        }}
        onBlur={commit}
        onKeyDown={(event) => {
          if (event.key === "Enter") {
            event.preventDefault();
            commit();
            event.currentTarget.blur();
          }
        }}
      />
      {empty
        ? (
          <p id={errorId} className="text-[13px] font-medium text-destructive">
            Enter the first name riders and drivers will see.
          </p>
        )
        : (
          <p id={statusId} aria-live="polite" className="text-[13px] text-muted-foreground">
            {saving.waiting
              ? "Saving…"
              : AsyncResult.isSuccess(saving) && saved !== ""
              ? "Saved."
              : "Saved as you leave the field."}
          </p>
        )}
      {AsyncResult.isFailure(saving) && !saving.waiting && <ActionError cause={saving.cause} />}
    </div>
  );
};
