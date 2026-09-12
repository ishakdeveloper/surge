/**
 * How a person is named on screen, the same on the web and on the phone: by
 * the first name they gave, and by their role until they give one.
 */

/** The letter an avatar without a photo shows, or none for someone without a name. */
export const initialOf = (name: string): string | undefined => {
  const first = [...name.trim()][0];
  return first === undefined ? undefined : first.toLocaleUpperCase("nl-NL");
};

/** A first name if there is one, and the role otherwise: "Sanne", or "Your driver". */
export const nameOr = (
  profile: { readonly displayName: string; } | undefined,
  role: string,
): string => {
  const name = profile?.displayName.trim() ?? "";
  return name === "" ? role : name;
};
