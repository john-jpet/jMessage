/** resizeImage downscales an image file so its longest side is at most
 *  maxSide, returning a JPEG blob (or the original file when it's
 *  already small enough and not resizable). Runs entirely client-side
 *  so avatar uploads stay tiny. */
export async function resizeImage(file: File, maxSide = 256): Promise<Blob> {
  const bitmap = await createImageBitmap(file).catch(() => null);
  if (!bitmap) return file; // not decodable here; let the server judge

  const scale = Math.min(1, maxSide / Math.max(bitmap.width, bitmap.height));
  if (scale === 1 && file.type === "image/jpeg") {
    bitmap.close();
    return file;
  }
  const w = Math.max(1, Math.round(bitmap.width * scale));
  const h = Math.max(1, Math.round(bitmap.height * scale));
  const canvas = document.createElement("canvas");
  canvas.width = w;
  canvas.height = h;
  const ctx = canvas.getContext("2d")!;
  ctx.drawImage(bitmap, 0, 0, w, h);
  bitmap.close();

  const blob = await new Promise<Blob | null>((resolve) =>
    canvas.toBlob(resolve, "image/jpeg", 0.88),
  );
  return blob ?? file;
}
