// Keeps the server's pictures of a board's HTML prototype screens current:
// while someone who may edit the board has it open, missing pictures (new
// or changed screens, per theme) are drawn here and uploaded, so server-side
// screenshots (whiteboard_screenshot) show the screens as they look.
import { api, apiRaw } from "./api";
import type { Whiteboard } from "./whiteboard";
import { darkTheme, screenPictureName, screenPicturePNG } from "./wb-html";

export class ScreenPictures {
  private known?: Set<string>;
  private failed = new Set<string>();
  private timer = 0;
  private busy = false;
  private board?: Whiteboard;

  constructor(private project: string, private doc: string) {}

  /** Checks the board's HTML screens a moment after the last change. */
  schedule(board: Whiteboard) {
    this.board = board;
    if (!board.elements.some((e) => e.screen?.html)) return;
    clearTimeout(this.timer);
    this.timer = window.setTimeout(() => void this.run(), 1200);
  }

  stop() {
    clearTimeout(this.timer);
  }

  private async run() {
    const board = this.board;
    if (!board) return;
    if (this.busy) return this.schedule(board);
    this.busy = true;
    try {
      const base = `/projects/${encodeURIComponent(this.project)}/documents/${encodeURIComponent(this.doc)}`;
      this.known ??= new Set((await api<{ pictures: string[] }>("GET", `${base}/screen-pictures`)).pictures ?? []);
      const dark = darkTheme();
      for (const e of board.elements) {
        if (!e.screen?.html || !e.w || !e.h) continue;
        const name = await screenPictureName(e.id, e.screen.html, e.w, e.h, dark);
        if (this.known.has(name) || this.failed.has(name)) continue;
        const proto = board.prototypes?.find((p) => p.id === e.screen!.proto);
        const state = Object.fromEntries((proto?.vars ?? []).map((v) => [v.name, v.value]));
        try {
          const png = await screenPicturePNG(e.screen.html, e.w, e.h, state);
          await apiRaw("PUT", `${base}/screens/${encodeURIComponent(e.id)}/picture?theme=${dark ? "dark" : "light"}`, png);
          this.known.add(name);
        } catch {
          this.failed.add(name); // not again in this session
        }
      }
    } catch {
      /* offline or no rights: the screenshot shows the text */
    } finally {
      this.busy = false;
    }
  }
}
