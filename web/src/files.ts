import { ApiError } from "./api";

export interface FileInfo {
  id: string; projectId: string; sha256: string; name: string; mime: string; size: number;
  documentId?: string; entityId?: string; createdBy: string; createdAt: string;
  version: number; hasPreview: boolean; textStatus: "" | "done" | "none" | "failed";
}
export interface FileVersion { version: number; sha256: string; mime: string; size: number; createdBy: string; createdAt: string }
export interface Storage { usedBytes: number; limitBytes: number; projectMb: number; maxMb: number }

/** Preview image (JPEG/PNG) made by the server, if hasPreview. */
export const previewURL = (f: Pick<FileInfo, "id" | "sha256">) => `/api/v1/files/${f.id}?preview=1&v=${f.sha256.slice(0, 8)}`;
/** An older version of a file. */
export const versionURL = (f: Pick<FileInfo, "id" | "name">, version: number, download = false) =>
  `/api/v1/files/${f.id}/${encodeURIComponent(f.name)}?version=${version}${download ? "&download=1" : ""}`;

/** Address of a file; the name is only for readable links. */
export const fileURL = (f: Pick<FileInfo, "id" | "name">, download = false) =>
  `/api/v1/files/${f.id}/${encodeURIComponent(f.name)}${download ? "?download=1" : ""}`;

export const isImage = (mime: string) => /^image\/(png|jpeg|gif|webp|avif|svg\+xml)/.test(mime);

export function fileIcon(mime: string): string {
  if (isImage(mime)) return "Image";
  if (mime.startsWith("text/") || mime === "application/pdf") return "FileText";
  return "Paperclip";
}

/** 1536 -> "1,5 KB" (locale-aware). */
export function formatSize(n: number, locale = "de"): string {
  const units = ["B", "KB", "MB", "GB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${v.toLocaleString(locale === "de" ? "de-DE" : "en-GB", { maximumFractionDigits: i === 0 ? 0 : 1 })} ${units[i]}`;
}

export interface UploadTarget { document?: string; entity?: string; /** new version of this file */ file?: string }

/** Files above this size are sent in resumable chunks (tus). */
export const RESUMABLE_FROM = 8 << 20;
const CHUNK = 4 << 20;

/** Uploads one file with progress: small files in one request, large ones resumable. */
export function uploadFile(project: string, file: File, attach: UploadTarget, onProgress?: (fraction: number) => void): Promise<FileInfo> {
  return file.size >= RESUMABLE_FROM ? uploadResumable(project, file, attach, onProgress) : uploadSimple(project, file, attach, onProgress);
}

function errorFrom(xhr: XMLHttpRequest): ApiError {
  let data: any = {};
  try { data = JSON.parse(xhr.responseText); } catch { /* not JSON */ }
  return new ApiError(xhr.status, data.code ?? "unknown", data.key ?? "error.unknown", data.params ?? {});
}

function send(method: string, url: string, body: Blob | null, headers: Record<string, string>, onProgress?: (loaded: number) => void): Promise<XMLHttpRequest> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open(method, url);
    xhr.setRequestHeader("X-Phoedox", "1");
    for (const [k, v] of Object.entries(headers)) xhr.setRequestHeader(k, v);
    if (onProgress) xhr.upload.onprogress = (e) => onProgress(e.loaded);
    xhr.onload = () => resolve(xhr);
    xhr.onerror = () => reject(new ApiError(0, "network", "error.network"));
    xhr.send(body);
  });
}

async function uploadSimple(project: string, file: File, attach: UploadTarget, onProgress?: (fraction: number) => void): Promise<FileInfo> {
  const url = attach.file
    ? `/api/v1/projects/${project}/files/${attach.file}/versions`
    : `/api/v1/projects/${project}/files?${new URLSearchParams({ name: file.name || "file", ...(attach.document ? { document: attach.document } : {}), ...(attach.entity ? { entity: attach.entity } : {}) })}`;
  const xhr = await send("POST", url, file, { "Content-Type": "application/octet-stream" }, (n) => onProgress?.(n / Math.max(file.size, 1)));
  if (xhr.status >= 200 && xhr.status < 300) return JSON.parse(xhr.responseText) as FileInfo;
  throw errorFrom(xhr);
}

const b64 = (s: string) => btoa(String.fromCharCode(...new TextEncoder().encode(s)));
const resumeKey = (project: string, f: File, attach: UploadTarget) => `phx-upload:${project}:${attach.file ?? ""}:${f.name}:${f.size}:${f.lastModified}`;
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));
const storage = () => { try { return window.localStorage; } catch { return undefined; } };

/** tus 1.0 client: creates the upload, sends chunks, resumes after network errors or a reload. */
async function uploadResumable(project: string, file: File, attach: UploadTarget, onProgress?: (fraction: number) => void): Promise<FileInfo> {
  const key = resumeKey(project, file, attach);
  const tus = { "Tus-Resumable": "1.0.0" };
  let location = storage()?.getItem(key) ?? "";
  let offset = 0;
  if (location) {
    const head = await send("HEAD", location, null, tus).catch(() => undefined);
    if (head?.status === 200) offset = Number(head.getResponseHeader("Upload-Offset") ?? 0);
    else location = "";
  }
  if (!location) {
    const meta = [`name ${b64(file.name || "file")}`];
    if (attach.document) meta.push(`document ${b64(attach.document)}`);
    if (attach.entity) meta.push(`entity ${b64(attach.entity)}`);
    if (attach.file) meta.push(`file ${b64(attach.file)}`);
    const created = await send("POST", `/api/v1/projects/${project}/uploads`, null, { ...tus, "Upload-Length": String(file.size), "Upload-Metadata": meta.join(",") });
    if (created.status !== 201) throw errorFrom(created);
    location = created.getResponseHeader("Location") ?? "";
    storage()?.setItem(key, location);
  }
  let failures = 0;
  for (;;) {
    const chunk = file.slice(offset, offset + CHUNK);
    let xhr: XMLHttpRequest;
    try {
      xhr = await send("PATCH", location, chunk, { ...tus, "Content-Type": "application/offset+octet-stream", "Upload-Offset": String(offset) },
        (n) => onProgress?.((offset + n) / file.size));
    } catch (e) {
      // Network gone: wait, ask the server how much arrived, continue.
      if (++failures > 6) throw e;
      await sleep(Math.min(30000, 1000 * 2 ** failures));
      const head = await send("HEAD", location, null, tus).catch(() => undefined);
      if (head?.status === 200) offset = Number(head.getResponseHeader("Upload-Offset") ?? offset);
      continue;
    }
    if (xhr.status === 409) {
      const head = await send("HEAD", location, null, tus);
      offset = Number(head.getResponseHeader("Upload-Offset") ?? offset);
      continue;
    }
    if (xhr.status !== 204) {
      storage()?.removeItem(key);
      throw errorFrom(xhr);
    }
    failures = 0;
    offset = Number(xhr.getResponseHeader("Upload-Offset") ?? offset + chunk.size);
    onProgress?.(offset / file.size);
    const id = xhr.getResponseHeader("Phoedox-File-Id");
    if (id) {
      storage()?.removeItem(key);
      const files = await fetch(`/api/v1/projects/${project}/files`, { credentials: "same-origin" }).then((r) => r.json() as Promise<FileInfo[]>);
      const f = files.find((x) => x.id === id);
      if (!f) throw new ApiError(404, "not_found", "error.not_found");
      return f;
    }
    if (offset >= file.size) throw new ApiError(500, "internal", "error.unknown");
  }
}
