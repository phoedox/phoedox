// Signing keys for agent jobs (ADR-0036). The private key is created in the
// browser as non-extractable and stays in this browser's IndexedDB; only the
// public key goes to the server and into agent.yaml (adminKeys).

import { api } from "./api";

export interface LocalKey { id: string; name: string; publicKey: string; privateKey: CryptoKey }
export interface ServerKey { id: string; name: string; publicKey: string; createdAt: string }

const DB = "phoedox-signing";
const STORE = "keys";

function db(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB, 1);
    req.onupgradeneeded = () => req.result.createObjectStore(STORE, { keyPath: "id" });
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

async function tx<T>(mode: IDBTransactionMode, fn: (s: IDBObjectStore) => IDBRequest<T>): Promise<T> {
  const d = await db();
  return new Promise((resolve, reject) => {
    const r = fn(d.transaction(STORE, mode).objectStore(STORE));
    r.onsuccess = () => resolve(r.result);
    r.onerror = () => reject(r.error);
  });
}

const b64 = (buf: ArrayBuffer) => btoa(String.fromCharCode(...new Uint8Array(buf)));

/** Whether this browser can create Ed25519 keys. */
export async function signingSupported(): Promise<boolean> {
  try {
    await crypto.subtle.generateKey({ name: "Ed25519" }, false, ["sign", "verify"]);
    return typeof indexedDB !== "undefined";
  } catch {
    return false;
  }
}

/** Keys stored in this browser. */
export async function localKeys(): Promise<LocalKey[]> {
  try {
    return await tx("readonly", (s) => s.getAll() as IDBRequest<LocalKey[]>);
  } catch {
    return [];
  }
}

/** Creates a key on this device and registers its public half. */
export async function createSigningKey(name: string): Promise<ServerKey> {
  const pair = (await crypto.subtle.generateKey({ name: "Ed25519" }, false, ["sign", "verify"])) as CryptoKeyPair;
  const publicKey = b64(await crypto.subtle.exportKey("raw", pair.publicKey));
  const k = await api<ServerKey>("POST", "/me/signing-keys", { name, publicKey });
  await tx("readwrite", (s) => s.put({ id: k.id, name, publicKey, privateKey: pair.privateKey } satisfies LocalKey));
  return k;
}

/** Forgets a key here and on the server. */
export async function deleteSigningKey(id: string) {
  await api("DELETE", `/me/signing-keys/${id}`).catch(() => undefined);
  await tx("readwrite", (s) => s.delete(id)).catch(() => undefined);
}

/** Signs a job payload with a key of this browser that the server knows. */
export async function signPayload(payload: string): Promise<{ keyId: string; signature: string } | undefined> {
  const server = (await api<ServerKey[]>("GET", "/me/signing-keys").catch(() => [])) ?? [];
  const k = (await localKeys()).find((l) => server.some((s) => s.id === l.id));
  if (!k) return undefined;
  const sig = await crypto.subtle.sign({ name: "Ed25519" }, k.privateKey, new TextEncoder().encode(payload));
  return { keyId: k.id, signature: b64(sig) };
}
