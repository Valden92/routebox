import jsQR from "jsqr";

/** Куда подставить строку из QR в форме добавления подписки. */
export type QrImportTarget = "url" | "uri" | "text";

export type QrImportResult = {
  target: QrImportTarget;
  value: string;
};

const SHARE_URI_RE = /^(vless|ss|ssr|trojan|hysteria2?|hy2|tuic|wireguard):\/\//i;

/**
 * Классифицирует содержимое QR: http(s) → URL, одна share-ссылка → URI, иначе текст
 * (список URI / base64 / Clash YAML).
 */
export function classifyQrPayload(raw: string): QrImportResult | null {
  const text = raw.replace(/^\uFEFF/, "").trim();
  if (!text) return null;
  if (/^https?:\/\//i.test(text)) {
    return { target: "url", value: text };
  }
  const lines = text
    .split(/\r?\n/)
    .map((l) => l.trim())
    .filter(Boolean);
  if (lines.length === 1 && SHARE_URI_RE.test(lines[0])) {
    return { target: "uri", value: lines[0] };
  }
  return { target: "text", value: text };
}

/** Декод QR из ImageData (jsQR). */
export function decodeQrFromImageData(imageData: ImageData): string | null {
  const code = jsQR(imageData.data, imageData.width, imageData.height, {
    inversionAttempts: "attemptBoth",
  });
  const data = code?.data?.trim();
  return data || null;
}

/** Декод QR из файла/blob (скриншот, PNG/JPEG/WebP). */
export async function decodeQrFromBlob(blob: Blob): Promise<string | null> {
  if (blob.type && !blob.type.startsWith("image/") && blob.type !== "application/octet-stream") {
    return null;
  }
  const bitmap = await createImageBitmap(blob);
  try {
    const canvas = document.createElement("canvas");
    canvas.width = bitmap.width;
    canvas.height = bitmap.height;
    const ctx = canvas.getContext("2d", { willReadFrequently: true });
    if (!ctx) return null;
    ctx.drawImage(bitmap, 0, 0);
    return decodeQrFromImageData(ctx.getImageData(0, 0, canvas.width, canvas.height));
  } finally {
    bitmap.close();
  }
}

/** Clipboard API (если разрешён) → image blob. */
export async function readImageFromClipboard(): Promise<Blob | null> {
  if (!navigator.clipboard || !("read" in navigator.clipboard)) {
    return null;
  }
  try {
    const items = await navigator.clipboard.read();
    for (const item of items) {
      const imageType = item.types.find((t) => t.startsWith("image/"));
      if (imageType) {
        return await item.getType(imageType);
      }
    }
  } catch {
    return null;
  }
  return null;
}

/** Первый image/* из paste DataTransfer. */
export function imageBlobFromPaste(data: DataTransfer | null): Blob | null {
  if (!data) return null;
  for (const item of Array.from(data.items)) {
    if (item.kind === "file" && item.type.startsWith("image/")) {
      return item.getAsFile();
    }
  }
  for (const file of Array.from(data.files)) {
    if (file.type.startsWith("image/")) return file;
  }
  return null;
}

/** Краткий превью для тоста / статуса. */
export function previewQrValue(value: string, max = 72): string {
  const one = value.replace(/\s+/g, " ").trim();
  if (one.length <= max) return one;
  return `${one.slice(0, max - 1)}…`;
}
