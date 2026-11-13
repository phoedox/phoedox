// WebAuthn helpers: the server (go-webauthn) sends options as JSON with
// base64url strings; the browser API wants ArrayBuffers and returns them.

const b64urlToBuf = (s: string): ArrayBuffer => {
  const pad = "=".repeat((4 - (s.length % 4)) % 4);
  const bin = atob((s + pad).replace(/-/g, "+").replace(/_/g, "/"));
  return Uint8Array.from(bin, (c) => c.charCodeAt(0)).buffer;
};

const bufToB64url = (b: ArrayBuffer | null | undefined): string | undefined => {
  if (!b) return undefined;
  let s = "";
  for (const byte of new Uint8Array(b)) s += String.fromCharCode(byte);
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
};

type Json = Record<string, any>;

export const passkeysSupported = () => typeof window !== "undefined" && "PublicKeyCredential" in window && !!navigator.credentials;

/** Decodes creation options from the server for navigator.credentials.create. */
export function creationOptions(server: Json): CredentialCreationOptions {
  const pk = { ...server.publicKey };
  pk.challenge = b64urlToBuf(pk.challenge);
  pk.user = { ...pk.user, id: b64urlToBuf(pk.user.id) };
  pk.excludeCredentials = (pk.excludeCredentials ?? []).map((c: Json) => ({ ...c, id: b64urlToBuf(c.id) }));
  return { publicKey: pk };
}

/** Decodes request options from the server for navigator.credentials.get. */
export function requestOptions(server: Json): CredentialRequestOptions {
  const pk = { ...server.publicKey };
  pk.challenge = b64urlToBuf(pk.challenge);
  pk.allowCredentials = (pk.allowCredentials ?? []).map((c: Json) => ({ ...c, id: b64urlToBuf(c.id) }));
  return { publicKey: pk, mediation: server.mediation };
}

/** Encodes a credential from create() or get() as the server expects it. */
export function credentialJSON(c: PublicKeyCredential): Json {
  const response: Json = { clientDataJSON: bufToB64url(c.response.clientDataJSON) };
  if ("attestationObject" in c.response) {
    const r = c.response as AuthenticatorAttestationResponse;
    response.attestationObject = bufToB64url(r.attestationObject);
    response.transports = r.getTransports?.() ?? [];
  } else {
    const r = c.response as AuthenticatorAssertionResponse;
    response.authenticatorData = bufToB64url(r.authenticatorData);
    response.signature = bufToB64url(r.signature);
    response.userHandle = bufToB64url(r.userHandle);
  }
  return {
    id: c.id, rawId: bufToB64url(c.rawId), type: c.type, response,
    authenticatorAttachment: c.authenticatorAttachment ?? undefined,
    clientExtensionResults: c.getClientExtensionResults(),
  };
}

/** True when the user closed the browser dialog (not an error to report). */
export const cancelled = (e: unknown) => e instanceof DOMException && (e.name === "NotAllowedError" || e.name === "AbortError");
