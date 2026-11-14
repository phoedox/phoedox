import { api } from "./api";

export interface AppNotification {
  id: string;
  projectId?: string;
  kind: string;
  subjectId?: string;
  title: string;
  body?: string;
  link?: string;
  actorName?: string;
  readAt: string | null;
  createdAt: string;
}

export interface NotificationSettings {
  email: "instant" | "daily" | "off";
  ntfyUrl: string;
  matrixHomeserver?: string;
  matrixRoom?: string;
  matrixConnected?: boolean;
  muted: string[];
  kinds: string[];
  pushKey?: string;
  subscriptions: string[];
  mailEnabled: boolean;
}

/** VAPID keys come base64url-encoded; PushManager wants bytes. */
export function urlBase64ToBytes(s: string): Uint8Array {
  const pad = "=".repeat((4 - (s.length % 4)) % 4);
  const raw = atob((s + pad).replace(/-/g, "+").replace(/_/g, "/"));
  return Uint8Array.from(raw, (c) => c.charCodeAt(0));
}

export const pushSupported = () => "serviceWorker" in navigator && "PushManager" in window && "Notification" in window;

/** The push subscription of this browser, if any. */
export async function currentSubscription(): Promise<PushSubscription | null> {
  if (!pushSupported()) return null;
  const reg = await navigator.serviceWorker.getRegistration("/");
  return (await reg?.pushManager.getSubscription()) ?? null;
}

/** Asks for permission, subscribes this browser and registers it. */
export async function enablePush(key: string): Promise<void> {
  if ((await Notification.requestPermission()) !== "granted") throw new Error("denied");
  const reg = await navigator.serviceWorker.register("/sw.js", { scope: "/" });
  await navigator.serviceWorker.ready;
  const sub = (await reg.pushManager.getSubscription()) ??
    (await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: urlBase64ToBytes(key) as BufferSource }));
  await api("POST", "/me/push-subscriptions", sub.toJSON());
}

export async function disablePush(): Promise<void> {
  const sub = await currentSubscription();
  if (!sub) return;
  await api("POST", "/me/push-subscriptions/remove", { endpoint: sub.endpoint }).catch(() => {});
  await sub.unsubscribe();
}
