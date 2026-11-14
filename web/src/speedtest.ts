// Speed test between this browser and the Phoedox server (ADR-0047). It
// measures the path the browser uses – on Wi-Fi in a room, that is the
// room's Wi-Fi as far as the server.

const H = { "X-Phoedox": "1" };

export interface SpeedResult { down: number; up: number; ping: number }

const mbit = (bytes: number, ms: number) => Math.round(((bytes * 8) / (ms / 1000) / 1e6) * 10) / 10;

/** Median of a list. */
export const median = (xs: number[]) => {
  const s = [...xs].sort((a, b) => a - b);
  return s.length ? (s.length % 2 ? s[(s.length - 1) / 2] : (s[s.length / 2 - 1] + s[s.length / 2]) / 2) : 0;
};

async function ping(): Promise<number> {
  const times: number[] = [];
  for (let i = 0; i < 6; i++) {
    const t0 = performance.now();
    await fetch(`/api/v1/speedtest/ping?n=${i}-${Date.now()}`, { headers: H, credentials: "same-origin", cache: "no-store" });
    if (i) times.push(performance.now() - t0); // the first one warms the connection
  }
  return Math.round(median(times) * 10) / 10;
}

async function download(bytes: number): Promise<number> {
  const t0 = performance.now();
  const res = await fetch(`/api/v1/speedtest/down?bytes=${bytes}&n=${Date.now()}`, { headers: H, credentials: "same-origin", cache: "no-store" });
  if (!res.ok || !res.body) throw new Error("download failed");
  const reader = res.body.getReader();
  let got = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    got += value.byteLength;
  }
  return mbit(got, performance.now() - t0);
}

async function upload(bytes: number): Promise<number> {
  const data = new Uint8Array(bytes);
  crypto.getRandomValues(data.subarray(0, Math.min(bytes, 65536)));
  const t0 = performance.now();
  const res = await fetch(`/api/v1/speedtest/up`, { method: "POST", headers: { ...H, "Content-Type": "application/octet-stream" }, credentials: "same-origin", body: data });
  if (!res.ok) throw new Error("upload failed");
  return mbit(bytes, performance.now() - t0);
}

/** Runs ping, a short then a longer download (sized by the first), and an upload. */
export async function speedTest(onStep?: (step: "ping" | "down" | "up") => void): Promise<SpeedResult> {
  onStep?.("ping");
  const p = await ping();
  onStep?.("down");
  const quick = await download(2 << 20);
  // Aim at about four seconds for the real measurement, between 4 and 100 MB.
  const size = Math.min(100 << 20, Math.max(4 << 20, Math.round((quick * 1e6 / 8) * 4)));
  const down = await download(size);
  onStep?.("up");
  const up = await upload(Math.min(size, 25 << 20));
  return { down, up, ping: p };
}
