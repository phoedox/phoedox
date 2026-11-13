import { ApiError } from "./api";

export interface FileInfo {
  id: string; projectId: string; sha256: string; name: string; mime: string; size: number;
  documentId?: string; entityId?: string; createdBy: string; createdAt: string;
}

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

/** Uploads one file with progress (XMLHttpRequest, as fetch has no upload progress). */
export function uploadFile(project: string, file: File, attach: { document?: string; entity?: string }, onProgress?: (fraction: number) => void): Promise<FileInfo> {
  const q = new URLSearchParams({ name: file.name || "file" });
  if (attach.document) q.set("document", attach.document);
  if (attach.entity) q.set("entity", attach.entity);
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", `/api/v1/projects/${project}/files?${q}`);
    xhr.setRequestHeader("X-Phoedox", "1");
    xhr.setRequestHeader("Content-Type", "application/octet-stream");
    xhr.upload.onprogress = (e) => e.lengthComputable && onProgress?.(e.loaded / e.total);
    xhr.onload = () => {
      let data: any = {};
      try { data = JSON.parse(xhr.responseText); } catch { /* not JSON */ }
      if (xhr.status >= 200 && xhr.status < 300) resolve(data as FileInfo);
      else reject(new ApiError(xhr.status, data.code ?? "unknown", data.key ?? "error.unknown", data.params ?? {}));
    };
    xhr.onerror = () => reject(new ApiError(0, "network", "error.network"));
    xhr.send(file);
  });
}
