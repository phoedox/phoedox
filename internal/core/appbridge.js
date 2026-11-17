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
  const call = (op, args) => new Promise((resolve, reject) => {
    const id = ++n;
    pending.set(id, { resolve, reject });
    post({ id, op, args });
  });
  const listeners = { theme: [] };
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
    const p = pending.get(m.id);
    if (!p) return;
    pending.delete(m.id);
    if (m.error) p.reject(Object.assign(new Error(m.error), { code: m.code }));
    else p.resolve(m.result);
  });
  const opts = (o) => ({ personal: !!(o && o.personal) });
  window.phoedox = Object.freeze({
    /** What the app may do here: { level, granted, sources, ai, project, user }. */
    info: () => call("info", {}),
    /** Asks the person for the level the app needs (shows Phoedox's dialog). */
    requestAccess: () => call("grant", {}),
    /** Reads a source of the app's manifest by its id (level 1). */
    read: (source) => call("read", { source }),
    /** The app's own storage (level 2): shared by default, or per person. */
    storage: Object.freeze({
      get: (key, o) => call("storage.get", Object.assign({ key }, opts(o))),
      set: (key, value, o) => call("storage.set", Object.assign({ key, value }, opts(o))),
      delete: (key, o) => call("storage.delete", Object.assign({ key }, opts(o))),
      list: (o) => call("storage.list", opts(o)),
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
})();
