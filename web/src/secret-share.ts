// End-to-end encrypted one-time secrets. The browser encrypts with AES-GCM;
// the key comes from 32 random bytes that only go into the link's #fragment
// (never sent to the server), optionally combined with a passphrase that is
// shared another way. The server stores nothing but the ciphertext.

const b64u = {
  enc: (b: Uint8Array) => btoa(String.fromCharCode(...b)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, ""),
  dec: (s: string): Uint8Array<ArrayBuffer> => Uint8Array.from(atob(s.replace(/-/g, "+").replace(/_/g, "/") + "===".slice((s.length + 3) % 4)), (c) => c.charCodeAt(0)),
};

interface Sealed { v: 1; salt: string; iv: string; ct: string; iter: number }

async function deriveKey(fragment: Uint8Array<ArrayBuffer>, passphrase: string, salt: Uint8Array<ArrayBuffer>, iter: number): Promise<CryptoKey> {
  const material = new Uint8Array([...fragment, ...new TextEncoder().encode(passphrase)]);
  const base = await crypto.subtle.importKey("raw", material, "PBKDF2", false, ["deriveKey"]);
  return crypto.subtle.deriveKey({ name: "PBKDF2", hash: "SHA-256", salt, iterations: iter }, base, { name: "AES-GCM", length: 256 }, false, ["encrypt", "decrypt"]);
}

/** Encrypts a secret: returns the ciphertext for the server and the key for the #fragment. */
export async function sealSecret(text: string, passphrase = ""): Promise<{ ciphertext: string; fragment: string }> {
  const fragment = crypto.getRandomValues(new Uint8Array(32));
  const salt = crypto.getRandomValues(new Uint8Array(16));
  const iv = crypto.getRandomValues(new Uint8Array(12));
  // A passphrase is guessable: make each guess expensive. The random part alone needs no stretching.
  const iter = passphrase ? 310_000 : 1;
  const key = await deriveKey(fragment, passphrase, salt, iter);
  const ct = new Uint8Array(await crypto.subtle.encrypt({ name: "AES-GCM", iv }, key, new TextEncoder().encode(text)));
  const sealed: Sealed = { v: 1, salt: b64u.enc(salt), iv: b64u.enc(iv), ct: b64u.enc(ct), iter };
  return { ciphertext: b64u.enc(new TextEncoder().encode(JSON.stringify(sealed))), fragment: b64u.enc(fragment) };
}

/** Decrypts with the #fragment (and passphrase); throws on a wrong key or passphrase. */
export async function openSecret(ciphertext: string, fragment: string, passphrase = ""): Promise<string> {
  const sealed = JSON.parse(new TextDecoder().decode(b64u.dec(ciphertext))) as Sealed;
  if (sealed.v !== 1) throw new Error("unknown format");
  const key = await deriveKey(b64u.dec(fragment), passphrase, b64u.dec(sealed.salt), sealed.iter);
  const plain = await crypto.subtle.decrypt({ name: "AES-GCM", iv: b64u.dec(sealed.iv) }, key, b64u.dec(sealed.ct));
  return new TextDecoder().decode(plain);
}
