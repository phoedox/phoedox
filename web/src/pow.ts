// Proof of work for the bot check (internal/botcheck): find a nonce so that
// sha256(prefix + nonce) starts with `bits` zero bits. Runs in a Web Worker
// with a small synchronous SHA-256 (much faster than one crypto.subtle call
// per try).

const workerSource = `
const K = new Uint32Array([0x428a2f98,0x71374491,0xb5c0fbcf,0xe9b5dba5,0x3956c25b,0x59f111f1,0x923f82a4,0xab1c5ed5,0xd807aa98,0x12835b01,0x243185be,0x550c7dc3,0x72be5d74,0x80deb1fe,0x9bdc06a7,0xc19bf174,0xe49b69c1,0xefbe4786,0x0fc19dc6,0x240ca1cc,0x2de92c6f,0x4a7484aa,0x5cb0a9dc,0x76f988da,0x983e5152,0xa831c66d,0xb00327c8,0xbf597fc7,0xc6e00bf3,0xd5a79147,0x06ca6351,0x14292967,0x27b70a85,0x2e1b2138,0x4d2c6dfc,0x53380d13,0x650a7354,0x766a0abb,0x81c2c92e,0x92722c85,0xa2bfe8a1,0xa81a664b,0xc24b8b70,0xc76c51a3,0xd192e819,0xd6990624,0xf40e3585,0x106aa070,0x19a4c116,0x1e376c08,0x2748774c,0x34b0bcb5,0x391c0cb3,0x4ed8aa4a,0x5b9cca4f,0x682e6ff3,0x748f82ee,0x78a5636f,0x84c87814,0x8cc70208,0x90befffa,0xa4506ceb,0xbef9a3f7,0xc67178f2]);
const W = new Uint32Array(64);
function sha256first(bytes) {
  const len = bytes.length, blocks = ((len + 9 + 63) >> 6);
  const buf = new Uint8Array(blocks * 64); buf.set(bytes); buf[len] = 0x80;
  const bits = len * 8; buf[buf.length - 4] = bits >>> 24; buf[buf.length - 3] = bits >>> 16; buf[buf.length - 2] = bits >>> 8; buf[buf.length - 1] = bits;
  let h0=0x6a09e667,h1=0xbb67ae85,h2=0x3c6ef372,h3=0xa54ff53a,h4=0x510e527f,h5=0x9b05688c,h6=0x1f83d9ab,h7=0x5be0cd19;
  for (let o = 0; o < buf.length; o += 64) {
    for (let i = 0; i < 16; i++) W[i] = (buf[o+i*4]<<24)|(buf[o+i*4+1]<<16)|(buf[o+i*4+2]<<8)|buf[o+i*4+3];
    for (let i = 16; i < 64; i++) {
      const a = W[i-15], b = W[i-2];
      const s0 = ((a>>>7)|(a<<25))^((a>>>18)|(a<<14))^(a>>>3), s1 = ((b>>>17)|(b<<15))^((b>>>19)|(b<<13))^(b>>>10);
      W[i] = (W[i-16] + s0 + W[i-7] + s1) | 0;
    }
    let a=h0,b=h1,c=h2,d=h3,e=h4,f=h5,g=h6,h=h7;
    for (let i = 0; i < 64; i++) {
      const S1 = ((e>>>6)|(e<<26))^((e>>>11)|(e<<21))^((e>>>25)|(e<<7)), ch = (e&f)^(~e&g);
      const t1 = (h + S1 + ch + K[i] + W[i]) | 0;
      const S0 = ((a>>>2)|(a<<30))^((a>>>13)|(a<<19))^((a>>>22)|(a<<10)), mj = (a&b)^(a&c)^(b&c);
      const t2 = (S0 + mj) | 0;
      h=g; g=f; f=e; e=(d+t1)|0; d=c; c=b; b=a; a=(t1+t2)|0;
    }
    h0=(h0+a)|0;h1=(h1+b)|0;h2=(h2+c)|0;h3=(h3+d)|0;h4=(h4+e)|0;h5=(h5+f)|0;h6=(h6+g)|0;h7=(h7+h)|0;
  }
  return [h0 >>> 0, h1 >>> 0];
}
onmessage = (ev) => {
  const { prefix, bits } = ev.data;
  const enc = new TextEncoder();
  const pre = enc.encode(prefix);
  for (let n = 0; ; n++) {
    const s = String(n), bytes = new Uint8Array(pre.length + s.length);
    bytes.set(pre); for (let i = 0; i < s.length; i++) bytes[pre.length + i] = s.charCodeAt(i);
    const [w0, w1] = sha256first(bytes);
    const ok = bits <= 32 ? (bits === 0 || (w0 >>> (32 - bits)) === 0) : (w0 === 0 && (w1 >>> (64 - bits)) === 0);
    if (ok) { postMessage(s); return; }
  }
};`;

/** Solves the proof of work in a worker; resolves with the nonce. */
export function solvePow(prefix: string, bits: number): Promise<string> {
  return new Promise((resolve, reject) => {
    let url = "";
    try {
      url = URL.createObjectURL(new Blob([workerSource], { type: "text/javascript" }));
      const w = new Worker(url);
      w.onmessage = (e) => { resolve(String(e.data)); w.terminate(); URL.revokeObjectURL(url); };
      w.onerror = (e) => { reject(e); w.terminate(); URL.revokeObjectURL(url); };
      w.postMessage({ prefix, bits });
    } catch (e) {
      if (url) URL.revokeObjectURL(url);
      reject(e);
    }
  });
}
