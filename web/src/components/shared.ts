import { css } from "lit";

/**
 * Shared component styles. Motion follows the animate skill:
 * transform/opacity only, strong ease-out, < 300ms, hover gated to fine
 * pointers, reduced motion keeps fades but drops movement.
 */
export const base = css`
  :host { color: var(--fg); font-family: var(--font-body); }
  *, *::before, *::after { box-sizing: border-box; }
  .avatar { border-radius: 50%; object-fit: cover; flex: none; display: inline-grid; place-items: center; background: var(--accent-soft); color: var(--fg);
    font-family: var(--font-mono); font-weight: 600; vertical-align: middle; }
  a { color: var(--accent); }
  h1, h2, h3 { font-family: var(--font-display); line-height: 1.15; margin: 0; text-wrap: balance; }
  .ico { display: inline-block; vertical-align: -3px; flex: none; }
  .muted { color: var(--muted); }
  .mono { font-family: var(--font-mono); font-size: 0.9em; }

  button, .btn {
    font: inherit; font-weight: 600; font-size: 14px; display: inline-flex; align-items: center; gap: 6px;
    border-radius: 8px; padding: 7px 14px; border: 1px solid transparent; cursor: pointer; text-decoration: none;
    background: var(--accent); color: var(--surface);
    transition: transform var(--dur-press) var(--ease-out), background-color var(--dur-fast) ease, border-color var(--dur-fast) ease;
  }
  button:active, .btn:active { transform: scale(0.97); }
  button.ghost, .btn.ghost { background: var(--surface); color: var(--fg); border-color: var(--line); }
  button.danger { background: var(--surface); color: var(--bad); border-color: var(--line); }
  button:disabled { opacity: 0.5; cursor: not-allowed; transform: none; }
  @media (hover: hover) and (pointer: fine) {
    button.ghost:hover, .btn.ghost:hover, button.danger:hover { border-color: var(--muted); }
  }
  :focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }

  label.field { display: flex; flex-direction: column; gap: 4px; font-size: 13px; color: var(--muted); }
  input, textarea, select {
    font: inherit; font-size: 14px; color: var(--fg); background: var(--surface);
    border: 1px solid var(--line); border-radius: 8px; padding: 8px 10px; width: 100%;
  }
  textarea { resize: vertical; min-height: 120px; font-family: var(--font-mono); font-size: 13px; line-height: 1.55; }
  .card { background: var(--surface); border: 1px solid var(--line); border-radius: 12px; padding: 18px; }
  .chip { display: inline-flex; align-items: center; gap: 4px; font-size: 12px; padding: 2px 9px; border-radius: 999px; background: var(--surface2); color: var(--muted); }
  .error { color: var(--bad); font-size: 13px; }
  .status-accepted { color: var(--good); }
  .status-rejected, .status-deprecated, .status-superseded { color: var(--bad); }
  .row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }

  /* Content swap between documents (tens of times a day): barely-there fade. */
  .enter {
    transition: opacity var(--dur-fast) var(--ease-out), transform var(--dur-fast) var(--ease-out);
    @starting-style { opacity: 0; transform: translateY(4px); }
  }
  @media (prefers-reduced-motion: reduce) {
    .enter { transition: opacity var(--dur-fast) ease; @starting-style { opacity: 0; transform: none; } }
    button:active, .btn:active { transform: none; }
  }
`;
