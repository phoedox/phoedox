// Phoedox app bridge: the app talks to the Phoedox page around it with
// postMessage; that page checks every request against what the person
// allowed. The app never sees a token or a cookie.
(() => {
  const pending = new Map();
  let n = 0;
  // In the Phoedox app (Flutter WebView) the page is the top window: calls
  // go to the native channel, answers come back as message events.
  const native = window.PhoedoxBridge && parent === window ? window.PhoedoxBridge : null;
  const post = (m) => (native ? native.postMessage(JSON.stringify(Object.assign({ phx: 1 }, m))) : parent.postMessage(Object.assign({ phx: 1 }, m), "*"));
  // The native channel carries text only: files travel as base64.
  const toWire = async (v) => {
    if (!native || !(v instanceof Blob)) return v;
    const buf = new Uint8Array(await v.arrayBuffer());
    let s = "";
    for (let i = 0; i < buf.length; i += 0x8000) s += String.fromCharCode.apply(null, buf.subarray(i, i + 0x8000));
    return { base64: btoa(s), type: v.type, name: v.name || "" };
  };
  const fromWire = (v) => {
    if (v && typeof v === "object" && typeof v.base64 === "string") {
      const bin = atob(v.base64);
      const b = new Uint8Array(bin.length);
      for (let i = 0; i < bin.length; i++) b[i] = bin.charCodeAt(i);
      return new Blob([b], { type: v.type || "application/octet-stream" });
    }
    return v;
  };
  const call = (op, args) => new Promise((resolve, reject) => {
    const id = ++n;
    pending.set(id, { resolve, reject });
    Promise.all(Object.entries(args || {}).map(async ([k, v]) => [k, await toWire(v)])).then((e) => post({ id, op, args: Object.fromEntries(e) }));
  });
  const listeners = { theme: [], db: [], motion: [] };
  addEventListener("message", (e) => {
    if (e.source !== parent) return;
    const m = e.data;
    if (!m || m.phx !== 1) return;
    if (m.event === "theme") {
      const r = document.documentElement;
      r.dataset.theme = m.dark ? "dark" : "light";
      r.style.colorScheme = m.dark ? "dark" : "light";
      for (const [k, v] of Object.entries(m.tokens || {})) r.style.setProperty("--phx-" + k, v);
      listeners.theme.forEach((f) => f(m));
      return;
    }
    if (m.event === "db") return listeners.db.forEach((f) => f(m.change));
    if (m.event === "motion") return listeners.motion.forEach((f) => f(m.motion));
    if (m.event === "capture") {
      capture().then((png) => post({ op: "capture", args: { png } }), () => post({ op: "capture", args: { png: "" } }));
      return;
    }
    const p = pending.get(m.id);
    if (!p) return;
    pending.delete(m.id);
    if (m.error) p.reject(Object.assign(new Error(m.error), { code: m.code }));
    else p.resolve(fromWire(m.result));
  });
  const opts = (o) => ({ personal: !!(o && o.personal) });
  const subscribe = (list, f) => {
    list.push(f);
    return () => { const i = list.indexOf(f); if (i >= 0) list.splice(i, 1); };
  };
  /** A collection of records: a small database of the app (level 2). */
  const collection = (name, o) => {
    const base = Object.assign({ collection: String(name) }, opts(o));
    return Object.freeze({
      /** Adds a record (a JSON object); returns {id, data, by, mine, createdAt, updatedAt}. */
      add: (data) => call("db.add", Object.assign({ data }, base)),
      /** Replaces a record. */
      put: (id, data) => call("db.put", Object.assign({ id, data }, base)),
      /** Changes some fields of a record (null removes a field). */
      update: (id, fields) => call("db.put", Object.assign({ id, data: fields, merge: true }, base)),
      delete: (id) => call("db.delete", Object.assign({ id }, base)),
      /** Records: {where: [[field, op, value]], order, desc, limit, offset, mine}. */
      query: (q) => {
        q = Object.assign({}, q || {});
        if (Array.isArray(q.where)) q.where = q.where.map((c) => (Array.isArray(c) ? { field: c[0], op: c[1], value: c[2] } : c));
        return call("db.query", Object.assign({ query: q }, base));
      },
      /** Calls f(change) when someone (also another person) changes a record. */
      subscribe: (f) => {
        call("db.watch", base).catch(() => undefined);
        return subscribe(listeners.db, (c) => { if (c.collection === base.collection) f(c); });
      },
    });
  };
  window.phoedox = Object.freeze({
    /** What the app may do here: { level, granted, sources, ai, project, devices }. */
    info: () => call("info", {}),
    /** Asks the person for the level the app needs (shows Phoedox's dialog). */
    requestAccess: () => call("grant", {}),
    /** Who is using the app (level 1): { id, name, editor }; the id is stable in this app. */
    me: () => call("me", {}),
    /** Reads a source of the app's manifest by its id (level 1). */
    read: (source) => call("read", { source }),
    /** The app's own storage (level 2): shared by default, or per person. */
    storage: Object.freeze({
      get: (key, o) => call("storage.get", Object.assign({ key }, opts(o))),
      set: (key, value, o) => call("storage.set", Object.assign({ key, value }, opts(o))),
      delete: (key, o) => call("storage.delete", Object.assign({ key }, opts(o))),
      list: (o) => call("storage.list", opts(o)),
    }),
    /** Collections of records with queries and live changes (level 2). */
    db: Object.freeze({ collection }),
    /** Files people add in the app (level 2). */
    files: Object.freeze({
      upload: (file, name) => call("files.upload", { file, name: name || (file && file.name) || "file" }),
      list: () => call("files.list", {}),
      /** The content as a Blob. */
      get: (id) => call("files.get", { id }),
      delete: (id) => call("files.delete", { id }),
    }),
    /** Gives the person a file to save (they confirm): text, a Blob or JSON. */
    download: (name, data, type) => call("download", { name, data: data instanceof Blob ? data : typeof data === "string" ? data : JSON.stringify(data, null, 2), type: type || (data instanceof Blob ? data.type : typeof data === "string" ? "text/plain" : "application/json") }),
    /** Device features the manifest lists; the person allows each one separately. */
    device: Object.freeze({
      /** { camera, microphone, location, screen, motion }: true, false or undefined (not asked). */
      status: () => call("device.status", {}),
      /** Asks for one feature. For camera, microphone and screen the page reloads once allowed; call it again and use the browser API. */
      request: (feature) => call("device.request", { feature }),
      /** The position once: { lat, lng, accuracy } (feature location). */
      location: () => call("device.location", {}),
      /** Motion of the device, about 10 times a second (feature motion). */
      onMotion: (f) => {
        call("device.motion", {}).catch(() => undefined);
        return subscribe(listeners.motion, f);
      },
    }),
    /** Apps with selfEdit: read and save their own files (people who may edit it confirm). */
    self: Object.freeze({
      files: () => call("self.files", {}),
      save: (files) => call("self.save", { files }),
    }),
    /** Proposes a change (level 3); the person confirms it in Phoedox. */
    propose: (change) => call("write", change),
    /** Asks the AI (when the app and the organization allow it). */
    ai: (prompt, o) => call("ai", { prompt, json: !!(o && o.json) }),
    /** Opens a page, entry or card of the project in Phoedox. */
    open: (ref) => call("open", { ref }),
    onTheme: (f) => listeners.theme.push(f),
  });
  const report = (level, text, line, col, file) => post({ op: "console", args: { level, text: String(text).slice(0, 2000), line, col, file } });
  addEventListener("error", (e) => report("error", e.message, e.lineno, e.colno, e.filename));
  addEventListener("unhandledrejection", (e) => report("error", e.reason && e.reason.stack ? e.reason.stack : e.reason));
  for (const lvl of ["error", "warn"]) {
    const orig = console[lvl].bind(console);
    console[lvl] = (...a) => { orig(...a); report(lvl, a.map((x) => (x instanceof Error ? x.stack || x.message : typeof x === "object" ? JSON.stringify(x) : String(x))).join(" ")); };
  }
  // The content's height (not the frame's), so embedded apps fit it.
  const height = () => {
    const b = document.body;
    if (!b) return 0;
    const cs = getComputedStyle(b);
    return Math.ceil(b.getBoundingClientRect().height + parseFloat(cs.marginTop) + parseFloat(cs.marginBottom));
  };
  addEventListener("DOMContentLoaded", () => {
    post({ op: "ready", args: { height: height() } });
    new ResizeObserver(() => post({ op: "resize", args: { height: height() } })).observe(document.body);
  });

  // A picture of the app as it renders now (for previews and the AI): the
  // page with its computed styles drawn into an SVG, then onto a canvas.
  async function capture() {
    const root = document.documentElement;
    const w = Math.max(320, Math.min(innerWidth, 1600));
    const h = Math.max(240, Math.min(innerHeight, 1200));
    const clone = root.cloneNode(true);
    const src = [root, ...root.querySelectorAll("*")];
    const dst = [clone, ...clone.querySelectorAll("*")];
    for (let i = 0; i < src.length && i < 4000; i++) {
      const a = src[i], b = dst[i];
      if (!b) break;
      const tag = a.tagName;
      if (tag === "SCRIPT" || tag === "NOSCRIPT") { b.remove(); continue; }
      const cs = getComputedStyle(a);
      let s = "";
      for (let k = 0; k < cs.length; k++) s += cs[k] + ":" + cs.getPropertyValue(cs[k]) + ";";
      b.setAttribute("style", s);
      try {
        if (tag === "CANVAS") {
          const img = document.createElement("img");
          img.src = a.toDataURL();
          img.setAttribute("style", s);
          b.replaceWith(img);
        } else if (tag === "IMG" && a.complete && a.naturalWidth) {
          const c = document.createElement("canvas");
          c.width = a.naturalWidth;
          c.height = a.naturalHeight;
          c.getContext("2d").drawImage(a, 0, 0);
          b.setAttribute("src", c.toDataURL());
        } else if (tag === "INPUT" || tag === "TEXTAREA") {
          b.setAttribute("value", a.value);
          if (tag === "TEXTAREA") b.textContent = a.value;
          if (a.checked) b.setAttribute("checked", "");
        }
      } catch { /* a picture from elsewhere: left as it is */ }
    }
    clone.querySelectorAll("link, iframe, video, audio, object, embed").forEach((x) => x.remove());
    const xml = new XMLSerializer().serializeToString(clone);
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}"><foreignObject x="0" y="${-scrollY}" width="${w}" height="${h + scrollY}">${xml}</foreignObject></svg>`;
    const img = new Image();
    img.src = "data:image/svg+xml;charset=utf-8," + encodeURIComponent(svg);
    await img.decode();
    const scale = Math.min(1, 1200 / w);
    const c = document.createElement("canvas");
    c.width = Math.round(w * scale);
    c.height = Math.round(h * scale);
    const ctx = c.getContext("2d");
    const bg = getComputedStyle(document.body).backgroundColor;
    ctx.fillStyle = !bg || bg === "transparent" || bg === "rgba(0, 0, 0, 0)" ? (document.documentElement.dataset.theme === "dark" ? "#14110f" : "#fff") : bg;
    ctx.fillRect(0, 0, c.width, c.height);
    ctx.drawImage(img, 0, 0, c.width, c.height);
    return c.toDataURL("image/png");
  }
})();
