/**
 * A chosen photo as it is sent: its middle square, at most 512 pixels, as a
 * JPEG.
 *
 * Drawn from the decoded pixels, the right way up however the phone stored it
 * (`imageOrientation: "from-image"`), so the upload is small, the preview is
 * true, and nothing the file carried besides its pixels rides along. The
 * server crops and re-encodes again; it never trusts that this ran.
 */

/** The side the server stores; sending more is only bytes. */
export const AVATAR_SIDE = 512;

export interface PreparedPhoto {
  /** The JPEG, base64, as the API takes it. */
  readonly base64: string;
  /** The same JPEG as a data URL, to show while it uploads. */
  readonly preview: string;
}

/** A file this browser cannot read as a photo — a HEIC on Chrome, a PDF picked by mistake. */
export class UnreadablePhoto extends Error {}

export const preparePhoto = async (file: File): Promise<PreparedPhoto> => {
  if (!file.type.startsWith("image/")) throw new UnreadablePhoto(`${file.type} is not a photo`);

  let bitmap: ImageBitmap;
  try {
    bitmap = await createImageBitmap(file, { imageOrientation: "from-image" });
  } catch (cause) {
    throw new UnreadablePhoto(`this browser cannot read ${file.type}`, { cause });
  }

  const side = Math.min(bitmap.width, bitmap.height);
  const target = Math.min(side, AVATAR_SIDE);
  const canvas = document.createElement("canvas");
  canvas.width = target;
  canvas.height = target;
  const context = canvas.getContext("2d");
  if (context === null) {
    bitmap.close();
    throw new UnreadablePhoto("this browser has no canvas to draw it on");
  }
  // White under a transparent PNG, as the server does, so the preview matches.
  context.fillStyle = "#ffffff";
  context.fillRect(0, 0, target, target);
  context.imageSmoothingQuality = "high";
  context.drawImage(
    bitmap,
    (bitmap.width - side) / 2,
    (bitmap.height - side) / 2,
    side,
    side,
    0,
    0,
    target,
    target,
  );
  bitmap.close();

  const blob = await new Promise<Blob>((resolve, reject) => {
    canvas.toBlob(
      (encoded) => {
        if (encoded === null) reject(new UnreadablePhoto("the photo could not be encoded"));
        else resolve(encoded);
      },
      "image/jpeg",
      0.9,
    );
  });

  const bytes = new Uint8Array(await blob.arrayBuffer());
  // In chunks: spreading half a megabyte into one call overflows the stack.
  let binary = "";
  for (let at = 0; at < bytes.length; at += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(at, at + 0x8000));
  }
  const base64 = btoa(binary);
  return { base64, preview: `data:image/jpeg;base64,${base64}` };
};
