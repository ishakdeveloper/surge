import { manipulateAsync, SaveFormat } from "expo-image-manipulator";
import * as ImagePicker from "expo-image-picker";

/** The side the server stores; sending more is only bytes over a phone's connection. */
export const AVATAR_SIDE = 512;

export type Picked =
  | {
    readonly _tag: "Chosen";
    /** The JPEG, base64, as the API takes it. */
    readonly base64: string;
    /** A local file of the same JPEG, to show while it uploads. */
    readonly preview: string;
  }
  | { readonly _tag: "Cancelled"; }
  | { readonly _tag: "Denied"; };

/**
 * A photo as it is sent: the square the person framed in the system picker's
 * own crop, scaled to 512 and re-encoded as a JPEG — which bakes the
 * orientation into the pixels and leaves every piece of metadata behind, the
 * place it was taken included. The server crops and re-encodes again; it
 * never trusts that this ran.
 *
 * The library needs no permission: the system picker hands over only the
 * photo chosen. The camera asks.
 */
export const pickPhoto = async (source: "library" | "camera"): Promise<Picked> => {
  if (source === "camera") {
    const permission = await ImagePicker.requestCameraPermissionsAsync();
    if (!permission.granted) return { _tag: "Denied" };
  }

  const options: ImagePicker.ImagePickerOptions = {
    mediaTypes: ["images"],
    allowsEditing: true,
    aspect: [1, 1],
    quality: 1,
    exif: false,
  };
  const result = source === "camera"
    ? await ImagePicker.launchCameraAsync({ ...options, cameraType: ImagePicker.CameraType.front })
    : await ImagePicker.launchImageLibraryAsync(options);
  const asset = result.canceled ? undefined : result.assets[0];
  if (asset === undefined) return { _tag: "Cancelled" };

  const side = Math.min(asset.width, asset.height, AVATAR_SIDE);
  const photo = await manipulateAsync(asset.uri, [{ resize: { width: side } }], {
    compress: 0.9,
    format: SaveFormat.JPEG,
    base64: true,
  });
  if (photo.base64 === undefined) throw new Error("the photo could not be encoded");
  return { _tag: "Chosen", base64: photo.base64, preview: photo.uri };
};
