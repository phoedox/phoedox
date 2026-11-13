// Profile pictures and project images: resized in the browser, small on the server.

import { html } from "lit";
import { apiRaw, type Project, type User } from "./api";

/** Scales an image file to a square of size px (cover) as WebP, or PNG where WebP is not available. */
export async function squareImage(file: File, size = 256): Promise<Blob> {
  const bmp = await createImageBitmap(file);
  const s = Math.min(bmp.width, bmp.height);
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = size;
  canvas.getContext("2d")!.drawImage(bmp, (bmp.width - s) / 2, (bmp.height - s) / 2, s, s, 0, 0, size, size);
  bmp.close();
  const blob = await new Promise<Blob | null>((r) => canvas.toBlob(r, "image/webp", 0.86));
  if (blob && blob.type === "image/webp") return blob;
  return new Promise<Blob>((r, e) => canvas.toBlob((b) => (b ? r(b) : e(new Error("encode"))), "image/png"));
}

export const userImageURL = (id: string, v: string) => `/api/v1/images/user/${id}?v=${v}`;
export const projectImageURL = (slug: string, v: string) => `/api/v1/projects/${slug}/image?v=${v}`;

export async function uploadAvatar(file: File) {
  return apiRaw<{ version: string }>("PUT", "/me/avatar", await squareImage(file));
}

export async function uploadProjectImage(slug: string, file: File) {
  return apiRaw<{ version: string }>("PUT", `/projects/${slug}/image`, await squareImage(file));
}

const initials = (name: string) => name.trim().split(/\s+/).map((w) => w[0]).join("").slice(0, 2).toUpperCase() || "?";

/** A round profile picture, or initials. */
export function userAvatar(u: Pick<User, "id" | "name" | "avatar"> | { id: string; name: string; avatar?: string }, size = 24) {
  const st = `width:${size}px;height:${size}px;font-size:${Math.round(size * 0.4)}px`;
  return u.avatar
    ? html`<img class="avatar" src=${userImageURL(u.id, u.avatar)} alt="" width=${size} height=${size} style=${st} loading="lazy">`
    : html`<span class="avatar ini" style=${st} aria-hidden="true">${initials(u.name)}</span>`;
}

/** A project's image, or its short name on the project color. */
export function projectBadge(p: Pick<Project, "slug" | "short" | "primaryColor" | "image">, size = 24) {
  const st = `width:${size}px;height:${size}px;border-radius:${Math.round(size / 4)}px`;
  return p.image
    ? html`<img class="pimg" src=${projectImageURL(p.slug, p.image)} alt="" width=${size} height=${size} style=${st + ";object-fit:cover;flex:none"}>`
    : html`<span class="pi" style=${st + `;background:${p.primaryColor}`}>${p.short}</span>`;
}
