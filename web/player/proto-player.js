/*
 * Phoedox prototype player: plays a prototype drawn on a whiteboard
 * (screens, components, interactions, variables, data).
 *
 * One plain script, shared by the web app (play mode), share links, the
 * Flutter app (WebView) and exported HTML files. It defines
 * globalThis.PhxProto.mount(root, model, hooks). It renders only the board's
 * data: texts are always set as text, never as HTML, and nothing from the
 * board runs as code.
 */
(() => {
  "use strict";

  const ICONS = {"house":"<path d=\"M15 21v-8a1 1 0 0 0-1-1h-4a1 1 0 0 0-1 1v8\"/><path d=\"M3 10a2 2 0 0 1 .709-1.528l7-6a2 2 0 0 1 2.582 0l7 6A2 2 0 0 1 21 10v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z\"/>","search":"<path d=\"m21 21-4.34-4.34\"/><circle cx=\"11\" cy=\"11\" r=\"8\"/>","user":"<path d=\"M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2\"/><circle cx=\"12\" cy=\"7\" r=\"4\"/>","users":"<path d=\"M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2\"/><path d=\"M16 3.128a4 4 0 0 1 0 7.744\"/><path d=\"M22 21v-2a4 4 0 0 0-3-3.87\"/><circle cx=\"9\" cy=\"7\" r=\"4\"/>","settings":"<path d=\"M9.671 4.136a2.34 2.34 0 0 1 4.659 0 2.34 2.34 0 0 0 3.319 1.915 2.34 2.34 0 0 1 2.33 4.033 2.34 2.34 0 0 0 0 3.831 2.34 2.34 0 0 1-2.33 4.033 2.34 2.34 0 0 0-3.319 1.915 2.34 2.34 0 0 1-4.659 0 2.34 2.34 0 0 0-3.32-1.915 2.34 2.34 0 0 1-2.33-4.033 2.34 2.34 0 0 0 0-3.831A2.34 2.34 0 0 1 6.35 6.051a2.34 2.34 0 0 0 3.319-1.915\"/><circle cx=\"12\" cy=\"12\" r=\"3\"/>","bell":"<path d=\"M10.268 21a2 2 0 0 0 3.464 0\"/><path d=\"M3.262 15.326A1 1 0 0 0 4 17h16a1 1 0 0 0 .74-1.673C19.41 13.956 18 12.499 18 8A6 6 0 0 0 6 8c0 4.499-1.411 5.956-2.738 7.326\"/>","heart":"<path d=\"M2 9.5a5.5 5.5 0 0 1 9.591-3.676.56.56 0 0 0 .818 0A5.49 5.49 0 0 1 22 9.5c0 2.29-1.5 4-3 5.5l-5.492 5.313a2 2 0 0 1-3 .019L5 15c-1.5-1.5-3-3.2-3-5.5\"/>","star":"<path d=\"M11.525 2.295a.53.53 0 0 1 .95 0l2.31 4.679a2.123 2.123 0 0 0 1.595 1.16l5.166.756a.53.53 0 0 1 .294.904l-3.736 3.638a2.123 2.123 0 0 0-.611 1.878l.882 5.14a.53.53 0 0 1-.771.56l-4.618-2.428a2.122 2.122 0 0 0-1.973 0L6.396 21.01a.53.53 0 0 1-.77-.56l.881-5.139a2.122 2.122 0 0 0-.611-1.879L2.16 9.795a.53.53 0 0 1 .294-.906l5.165-.755a2.122 2.122 0 0 0 1.597-1.16z\"/>","plus":"<path d=\"M5 12h14\"/><path d=\"M12 5v14\"/>","minus":"<path d=\"M5 12h14\"/>","x":"<path d=\"M18 6 6 18\"/><path d=\"m6 6 12 12\"/>","check":"<path d=\"M20 6 9 17l-5-5\"/>","chevron-left":"<path d=\"m15 18-6-6 6-6\"/>","chevron-right":"<path d=\"m9 18 6-6-6-6\"/>","chevron-down":"<path d=\"m6 9 6 6 6-6\"/>","chevron-up":"<path d=\"m18 15-6-6-6 6\"/>","arrow-left":"<path d=\"m12 19-7-7 7-7\"/><path d=\"M19 12H5\"/>","arrow-right":"<path d=\"M5 12h14\"/><path d=\"m12 5 7 7-7 7\"/>","menu":"<path d=\"M4 5h16\"/><path d=\"M4 12h16\"/><path d=\"M4 19h16\"/>","ellipsis":"<circle cx=\"12\" cy=\"12\" r=\"1\"/><circle cx=\"19\" cy=\"12\" r=\"1\"/><circle cx=\"5\" cy=\"12\" r=\"1\"/>","mail":"<path d=\"m22 7-8.991 5.727a2 2 0 0 1-2.009 0L2 7\"/><rect x=\"2\" y=\"4\" width=\"20\" height=\"16\" rx=\"2\"/>","phone":"<path d=\"M13.832 16.568a1 1 0 0 0 1.213-.303l.355-.465A2 2 0 0 1 17 15h3a2 2 0 0 1 2 2v3a2 2 0 0 1-2 2A18 18 0 0 1 2 4a2 2 0 0 1 2-2h3a2 2 0 0 1 2 2v3a2 2 0 0 1-.8 1.6l-.468.351a1 1 0 0 0-.292 1.233 14 14 0 0 0 6.392 6.384\"/>","calendar":"<path d=\"M8 2v3\"/><path d=\"M16 2v3\"/><rect x=\"3\" y=\"3\" width=\"18\" height=\"18\" rx=\"2\"/><path d=\"M3 9h18\"/>","clock":"<circle cx=\"12\" cy=\"12\" r=\"10\"/><path d=\"M12 6v6l4 2\"/>","camera":"<path d=\"M13.997 4a2 2 0 0 1 1.76 1.05l.486.9A2 2 0 0 0 18.003 7H20a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V9a2 2 0 0 1 2-2h1.997a2 2 0 0 0 1.759-1.048l.489-.904A2 2 0 0 1 10.004 4z\"/><circle cx=\"12\" cy=\"13\" r=\"3\"/>","image":"<rect width=\"18\" height=\"18\" x=\"3\" y=\"3\" rx=\"2\" ry=\"2\"/><circle cx=\"9\" cy=\"9\" r=\"2\"/><path d=\"m21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21\"/>","map-pin":"<path d=\"M20 10c0 4.993-5.539 10.193-7.399 11.799a1 1 0 0 1-1.202 0C9.539 20.193 4 14.993 4 10a8 8 0 0 1 16 0\"/><circle cx=\"12\" cy=\"10\" r=\"3\"/>","shopping-cart":"<path d=\"m2.05 2.05 1.099-.028a1 1 0 0 1 1.008.815l2.69 14.347A1 1 0 0 0 7.83 18H18\"/><path d=\"M4.563 5h16.435a1 1 0 0 1 .981 1.204l-1.026 6.226A2 2 0 0 1 18.962 14H6.25\"/><circle cx=\"18\" cy=\"20\" r=\"2\"/><circle cx=\"8\" cy=\"20\" r=\"2\"/>","trash-2":"<path d=\"M10 11v6\"/><path d=\"M14 11v6\"/><path d=\"M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6\"/><path d=\"M3 6h18\"/><path d=\"M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2\"/>","pencil":"<path d=\"M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z\"/><path d=\"m15 5 4 4\"/>","share-2":"<circle cx=\"18\" cy=\"5\" r=\"3\"/><circle cx=\"6\" cy=\"12\" r=\"3\"/><circle cx=\"18\" cy=\"19\" r=\"3\"/><line x1=\"8.59\" x2=\"15.42\" y1=\"13.51\" y2=\"17.49\"/><line x1=\"15.41\" x2=\"8.59\" y1=\"6.51\" y2=\"10.49\"/>","download":"<path d=\"M12 15V3\"/><path d=\"M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4\"/><path d=\"m7 10 5 5 5-5\"/>","upload":"<path d=\"M12 3v12\"/><path d=\"m17 8-5-5-5 5\"/><path d=\"M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4\"/>","filter":"<path d=\"M10 20a1 1 0 0 0 .553.895l2 1A1 1 0 0 0 14 21v-7a2 2 0 0 1 .517-1.341L21.74 4.67A1 1 0 0 0 21 3H3a1 1 0 0 0-.742 1.67l7.225 7.989A2 2 0 0 1 10 14z\"/>","list":"<path d=\"M3 5h.01\"/><path d=\"M3 12h.01\"/><path d=\"M3 19h.01\"/><path d=\"M8 5h13\"/><path d=\"M8 12h13\"/><path d=\"M8 19h13\"/>","layout-grid":"<rect width=\"7\" height=\"7\" x=\"3\" y=\"3\" rx=\"1\"/><rect width=\"7\" height=\"7\" x=\"14\" y=\"3\" rx=\"1\"/><rect width=\"7\" height=\"7\" x=\"14\" y=\"14\" rx=\"1\"/><rect width=\"7\" height=\"7\" x=\"3\" y=\"14\" rx=\"1\"/>","lock":"<rect width=\"18\" height=\"11\" x=\"3\" y=\"11\" rx=\"2\" ry=\"2\"/><path d=\"M7 11V7a5 5 0 0 1 10 0v4\"/>","log-out":"<path d=\"m16 17 5-5-5-5\"/><path d=\"M21 12H9\"/><path d=\"M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4\"/>","info":"<circle cx=\"12\" cy=\"12\" r=\"10\"/><path d=\"M12 16v-4\"/><path d=\"M12 8h.01\"/>","circle-alert":"<circle cx=\"12\" cy=\"12\" r=\"10\"/><line x1=\"12\" x2=\"12\" y1=\"8\" y2=\"12\"/><line x1=\"12\" x2=\"12.01\" y1=\"16\" y2=\"16\"/>","message-circle":"<path d=\"M2.992 16.342a2 2 0 0 1 .094 1.167l-1.065 3.29a1 1 0 0 0 1.236 1.168l3.413-.998a2 2 0 0 1 1.099.092 10 10 0 1 0-4.777-4.719\"/>","play":"<path d=\"M5 5a2 2 0 0 1 3.008-1.728l11.997 6.998a2 2 0 0 1 .003 3.458l-12 7A2 2 0 0 1 5 19z\"/>","pause":"<rect x=\"14\" y=\"3\" width=\"5\" height=\"18\" rx=\"1\"/><rect x=\"5\" y=\"3\" width=\"5\" height=\"18\" rx=\"1\"/>","refresh-cw":"<path d=\"M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8\"/><path d=\"M21 3v5h-5\"/><path d=\"M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16\"/><path d=\"M8 16H3v5\"/>","sun":"<circle cx=\"12\" cy=\"12\" r=\"4\"/><path d=\"M12 2v2\"/><path d=\"M12 20v2\"/><path d=\"m4.93 4.93 1.41 1.41\"/><path d=\"m17.66 17.66 1.41 1.41\"/><path d=\"M2 12h2\"/><path d=\"M20 12h2\"/><path d=\"m6.34 17.66-1.41 1.41\"/><path d=\"m19.07 4.93-1.41 1.41\"/>","moon":"<path d=\"M20.985 12.486a9 9 0 1 1-9.473-9.472c.405-.022.617.46.402.803a6 6 0 0 0 8.268 8.268c.344-.215.825-.004.803.401\"/>","wifi":"<path d=\"M12 20h.01\"/><path d=\"M2 8.82a15 15 0 0 1 20 0\"/><path d=\"M5 12.859a10 10 0 0 1 14 0\"/><path d=\"M8.5 16.429a5 5 0 0 1 7 0\"/>","cloud":"<path d=\"M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9Z\"/>","bookmark":"<path d=\"M17 3a2 2 0 0 1 2 2v15a1 1 0 0 1-1.496.868l-4.512-2.578a2 2 0 0 0-1.984 0l-4.512 2.578A1 1 0 0 1 5 20V5a2 2 0 0 1 2-2z\"/>","send":"<path d=\"M14.536 21.686a.5.5 0 0 0 .937-.024l6.5-19a.496.496 0 0 0-.635-.635l-19 6.5a.5.5 0 0 0-.024.937l7.93 3.18a2 2 0 0 1 1.112 1.11z\"/><path d=\"m21.854 2.147-10.94 10.939\"/>","credit-card":"<rect width=\"20\" height=\"14\" x=\"2\" y=\"5\" rx=\"2\"/><line x1=\"2\" x2=\"22\" y1=\"10\" y2=\"10\"/><path d=\"M6 14h2\"/>","file-text":"<path d=\"M6 22a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h8a2.4 2.4 0 0 1 1.704.706l3.588 3.588A2.4 2.4 0 0 1 20 8v12a2 2 0 0 1-2 2z\"/><path d=\"M14 2v5a1 1 0 0 0 1 1h5\"/><path d=\"M10 9H8\"/><path d=\"M16 13H8\"/><path d=\"M16 17H8\"/>","folder":"<path d=\"M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z\"/>","tag":"<path d=\"M12.586 2.586A2 2 0 0 0 11.172 2H4a2 2 0 0 0-2 2v7.172a2 2 0 0 0 .586 1.414l8.704 8.704a2.426 2.426 0 0 0 3.42 0l6.58-6.58a2.426 2.426 0 0 0 0-3.42z\"/><circle cx=\"7.5\" cy=\"7.5\" r=\".5\" fill=\"currentColor\"/>","thumbs-up":"<path d=\"M15 5.88 14 10h5.83a2 2 0 0 1 1.92 2.56l-2.33 8A2 2 0 0 1 17.5 22H4a2 2 0 0 1-2-2v-8a2 2 0 0 1 2-2h2.76a2 2 0 0 0 1.79-1.11L12 2a3.13 3.13 0 0 1 3 3.88Z\"/><path d=\"M7 10v12\"/>","eye":"<path d=\"M2.062 12.348a1 1 0 0 1 0-.696 10.75 10.75 0 0 1 19.876 0 1 1 0 0 1 0 .696 10.75 10.75 0 0 1-19.876 0\"/><circle cx=\"12\" cy=\"12\" r=\"3\"/>","circle-check":"<circle cx=\"12\" cy=\"12\" r=\"10\"/><path d=\"m16 9-5.5 5.5L8 12\"/>","zap":"<path d=\"M15.914 4a1.5 1.5 0 00-2.474-1.561l-9 9A1.5 1.5 0 005.5 14h4.002a.5.5 0 01.471.666L8.086 20a1.5 1.5 0 002.475 1.56l9-9A1.5 1.5 0 0018.5 10h-3.997a.5.5 0 01-.472-.667z\"/>","gift":"<path d=\"M12 7v14\"/><path d=\"M20 11v8a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2v-8\"/><path d=\"M7.5 7a1 1 0 0 1 0-5A4.8 8 0 0 1 12 7a4.8 8 0 0 1 4.5-5 1 1 0 0 1 0 5\"/><rect x=\"3\" y=\"7\" width=\"18\" height=\"4\" rx=\"1\"/>","truck":"<path d=\"M14 18V6a2 2 0 0 0-2-2H4a2 2 0 0 0-2 2v11a1 1 0 0 0 1 1h2\"/><path d=\"M15 18H9\"/><path d=\"M19 18h2a1 1 0 0 0 1-1v-3.65a1 1 0 0 0-.22-.624l-3.48-4.35A1 1 0 0 0 17.52 8H14\"/><circle cx=\"17\" cy=\"18\" r=\"2\"/><circle cx=\"7\" cy=\"18\" r=\"2\"/>","package":"<path d=\"M11 21.73a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73z\"/><path d=\"M12 22V12\"/><polyline points=\"3.29 7 12 12 20.71 7\"/><path d=\"m7.5 4.27 9 5.15\"/>","bar-chart-3":"<path d=\"M3 3v16a2 2 0 0 0 2 2h16\"/><path d=\"M18 17V9\"/><path d=\"M13 17V5\"/><path d=\"M8 17v-3\"/>","chart-column":"<path d=\"M3 3v16a2 2 0 0 0 2 2h16\"/><path d=\"M18 17V9\"/><path d=\"M13 17V5\"/><path d=\"M8 17v-3\"/>","activity":"<path d=\"M22 12h-2.48a2 2 0 0 0-1.93 1.46l-2.35 8.36a.25.25 0 0 1-.48 0L9.24 2.18a.25.25 0 0 0-.48 0l-2.35 8.36A2 2 0 0 1 4.49 12H2\"/>","server":"<rect width=\"20\" height=\"8\" x=\"2\" y=\"2\" rx=\"2\" ry=\"2\"/><rect width=\"20\" height=\"8\" x=\"2\" y=\"14\" rx=\"2\" ry=\"2\"/><line x1=\"6\" x2=\"6.01\" y1=\"6\" y2=\"6\"/><line x1=\"6\" x2=\"6.01\" y1=\"18\" y2=\"18\"/>","router":"<rect width=\"20\" height=\"8\" x=\"2\" y=\"14\" rx=\"2\"/><path d=\"M6.01 18H6\"/><path d=\"M10.01 18H10\"/><path d=\"M15 10v4\"/><path d=\"M17.84 7.17a4 4 0 0 0-5.66 0\"/><path d=\"M20.66 4.34a8 8 0 0 0-11.31 0\"/>","monitor":"<rect width=\"20\" height=\"14\" x=\"2\" y=\"3\" rx=\"2\"/><line x1=\"8\" x2=\"16\" y1=\"21\" y2=\"21\"/><line x1=\"12\" x2=\"12\" y1=\"17\" y2=\"21\"/>","smartphone":"<rect width=\"14\" height=\"20\" x=\"5\" y=\"2\" rx=\"2\" ry=\"2\"/><path d=\"M12 18h.01\"/>","lightbulb":"<path d=\"M15 14c.2-1 .7-1.7 1.5-2.5 1-.9 1.5-2.2 1.5-3.5A6 6 0 0 0 6 8c0 1 .2 2.2 1.5 3.5.7.7 1.3 1.5 1.5 2.5\"/><path d=\"M9 18h6\"/><path d=\"M10 22h4\"/>","thermometer":"<path d=\"M14 4v10.54a4 4 0 1 1-4 0V4a2 2 0 0 1 4 0Z\"/>","power":"<path d=\"M12 2v10\"/><path d=\"M18.4 6.6a9 9 0 1 1-12.77.04\"/>","circle-user":"<circle cx=\"12\" cy=\"12\" r=\"10\"/><circle cx=\"12\" cy=\"10\" r=\"3\"/><path d=\"M7 20.662V19a2 2 0 0 1 2-2h6a2 2 0 0 1 2 2v1.662\"/>","log-in":"<path d=\"m10 17 5-5-5-5\"/><path d=\"M15 12H3\"/><path d=\"M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4\"/>","sliders-horizontal":"<path d=\"M10 5H3\"/><path d=\"M12 19H3\"/><path d=\"M14 3v4\"/><path d=\"M16 17v4\"/><path d=\"M21 12h-9\"/><path d=\"M21 19h-5\"/><path d=\"M21 5h-7\"/><path d=\"M8 10v4\"/><path d=\"M8 12H3\"/>","square-pen":"<path d=\"M12 3H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7\"/><path d=\"M18.375 2.625a1 1 0 0 1 3 3l-9.013 9.014a2 2 0 0 1-.853.505l-2.873.84a.5.5 0 0 1-.62-.62l.84-2.873a2 2 0 0 1 .506-.852z\"/>","circle-help":"<circle cx=\"12\" cy=\"12\" r=\"10\"/><path d=\"M9 9.5a3 2.5 0 016 0c0 2-2 2-3 3.5\"/><path d=\"M12 17h.01\"/>"};

  const STR = {
    en: { restart: "Restart", screens: "Screens", comment: "Comment", close: "Close", send: "Send", cancel: "Cancel",
      place: "Click where the comment belongs", saved: "Comment saved", failed: "The comment could not be saved", empty: "This prototype has no screens yet.", comments: "comments" },
    de: { restart: "Neu starten", screens: "Bildschirme", comment: "Kommentieren", close: "Schließen", send: "Senden", cancel: "Abbrechen",
      place: "Klicke dorthin, wo der Kommentar hingehört", saved: "Kommentar gespeichert", failed: "Der Kommentar ließ sich nicht speichern", empty: "Dieser Prototyp hat noch keine Bildschirme.", comments: "Kommentare" },
  };

  /** Palette: stroke and fill colours (the board's tokens, with fallbacks). */
  const PAL = {
    gray: ["#6b6159", "#ebe7e3"], red: ["#b4372f", "#f9dedb"], orange: ["#b84a0c", "#fbe4d4"], yellow: ["#8a6a00", "#f8ecbf"],
    green: ["#2c8a4b", "#dcefe1"], blue: ["#2f5bc0", "#dde6f8"], purple: ["#7a3eb8", "#ebe0f6"], pink: ["#b0306f", "#f7dde9"],
  };
  const tc = (c) => (c && PAL[c] ? `var(--tc-${c}, ${PAL[c][0]})` : "var(--pf)");
  const tb = (c) => (c && PAL[c] ? `var(--tb-${c}, ${PAL[c][1]})` : "transparent");

  const EASE = "cubic-bezier(0.23, 1, 0.32, 1)";
  const DRAWER = "cubic-bezier(0.32, 0.72, 0, 1)";
  const reduced = () => typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;

  const CSS = `
:host { all: initial; display: block; position: relative; width: 100%; height: 100%; contain: strict;
  font-family: var(--font-body, system-ui, -apple-system, "Segoe UI", sans-serif); font-size: 14px; color: var(--fg, #1f1b17);
  --pa: var(--accent, #c2410c); --pas: var(--accent-soft, #fde5d6); --ps: var(--surface, #ffffff); --ps2: var(--surface2, #f4f1ee);
  --pl: var(--line, #e4ded8); --pf: var(--fg, #1f1b17); --pm: var(--muted, #6b6159); --pon: var(--on-color, #ffffff);
  --pbg: var(--canvas, var(--bg, #ece8e4)); --pbad: var(--bad, #b4372f); --pgood: var(--good, #2c8a4b); }
* { box-sizing: border-box; }
.stage { position: absolute; inset: 0; display: flex; flex-direction: column; background: var(--pbg); }
.bar { display: flex; align-items: center; gap: 6px; padding: 8px 10px; flex: none; }
.bar .name { flex: 1; min-width: 0; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.bar button, .bar select { height: 32px; padding: 0 10px; border: 1px solid var(--pl); border-radius: 8px; background: var(--ps); color: var(--pf);
  font: inherit; font-size: 13px; display: inline-flex; align-items: center; gap: 6px; cursor: pointer; }
.bar select { max-width: 40vw; }
.bar button[aria-pressed="true"] { background: var(--pas); color: var(--pa); border-color: var(--pa); }
.bar button:focus-visible, .bar select:focus-visible { outline: 2px solid var(--pa); outline-offset: 1px; }
@media (hover: hover) and (pointer: fine) { .bar button:hover { background: var(--ps2); } }
.bar .lbl { display: inline; } @media (max-width: 520px) { .bar .lbl { display: none; } }
.view { position: relative; flex: 1; min-height: 0; overflow: hidden; }
.hint-line { position: absolute; left: 50%; top: 8px; transform: translateX(-50%); z-index: 5; padding: 6px 12px; border-radius: 99px;
  background: var(--pf); color: var(--ps); font-size: 13px; pointer-events: none; }
.empty { position: absolute; inset: 0; display: grid; place-items: center; color: var(--pm); padding: 24px; text-align: center; }
.device { position: absolute; left: 50%; top: 50%; overflow: hidden; background: var(--ps); color: var(--pf);
  box-shadow: 0 24px 60px -28px rgb(0 0 0 / 0.5), 0 0 0 1px var(--pl); }
.device.phone { border-radius: 44px; } .device.tablet { border-radius: 28px; } .device.desktop { border-radius: 10px; } .device.custom { border-radius: 14px; }
.layer { position: absolute; left: 0; top: 0; overflow: hidden; background: var(--ps); }
.el { position: absolute; }
.txt { width: 100%; height: 100%; display: flex; align-items: center; justify-content: center; text-align: center; white-space: pre-wrap;
  overflow-wrap: anywhere; line-height: 1.25; padding: 4px; overflow: hidden; }
.txt.top { align-items: flex-start; justify-content: flex-start; text-align: left; }
.hot { cursor: pointer; }
.device.hint .hot { box-shadow: 0 0 0 3px color-mix(in srgb, var(--pa) 60%, transparent); }
.device .hot { transition: box-shadow 200ms ease; }
.ov { position: absolute; inset: 0; background: rgb(0 0 0 / 0.38); }
.ov .layer { box-shadow: 0 18px 50px -12px rgb(0 0 0 / 0.5); }
.ov.modal .layer { border-radius: 16px; }
.ov.sheet .layer { border-radius: 16px 16px 0 0; }
.toast { position: absolute; left: 50%; bottom: 28px; transform: translateX(-50%); z-index: 20; max-width: calc(100% - 32px); padding: 10px 16px;
  border-radius: 12px; background: var(--pf); color: var(--ps); font-size: 14px; box-shadow: 0 10px 30px -10px rgb(0 0 0 / 0.5); white-space: pre-wrap; }
/* components */
.ui { width: 100%; height: 100%; font: inherit; color: inherit; }
.btn { border: 1px solid transparent; border-radius: var(--radius-btn, 10px); display: inline-flex; align-items: center; justify-content: center; gap: 8px;
  font-weight: 600; cursor: pointer; padding: 0 12px; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
.btn.primary { background: var(--pa); color: var(--pon); }
.btn.secondary { background: var(--ps); color: var(--pf); border-color: var(--pl); }
.btn.ghost { background: transparent; color: var(--pa); }
.btn.danger { background: var(--pbad); color: var(--pon); }
.btn:active { transform: scale(0.97); } .btn { transition: transform 120ms ease; }
.btn:focus-visible, .field:focus-visible, .row:focus-visible, .tab:focus-visible, a.link:focus-visible { outline: 2px solid var(--pa); outline-offset: 2px; }
a.link { color: var(--pa); text-decoration: underline; display: flex; align-items: center; cursor: pointer; }
.field { border: 1px solid var(--pl); border-radius: 10px; background: var(--ps); padding: 0 12px; font: inherit; color: var(--pf); }
textarea.field { padding: 10px 12px; resize: none; }
.labelled { display: flex; align-items: center; gap: 10px; cursor: pointer; user-select: none; }
.labelled input[type="checkbox"], .labelled input[type="radio"] { width: 20px; height: 20px; accent-color: var(--pa); margin: 0; flex: none; }
.sw { position: relative; flex: none; width: 46px; height: 28px; border-radius: 99px; background: var(--pl); transition: background 160ms ease; }
.sw::after { content: ""; position: absolute; left: 3px; top: 3px; width: 22px; height: 22px; border-radius: 50%; background: #fff; box-shadow: 0 1px 3px rgb(0 0 0 / 0.3); transition: transform 160ms ${EASE}; }
.switch input { position: absolute; opacity: 0; pointer-events: none; }
.switch input:checked + .sw { background: var(--pa); } .switch input:checked + .sw::after { transform: translateX(18px); }
.switch input:focus-visible + .sw { outline: 2px solid var(--pa); outline-offset: 2px; }
.switch span.t { flex: 1; }
.radios { display: grid; gap: 8px; align-content: start; }
.slider { display: grid; gap: 4px; align-content: center; } .slider input { width: 100%; accent-color: var(--pa); }
.slider .v { display: flex; justify-content: space-between; color: var(--pm); font-size: 13px; }
.tabs { display: flex; gap: 4px; padding: 3px; border-radius: 10px; background: var(--ps2); }
.tab { flex: 1; border: 0; border-radius: 8px; background: transparent; color: var(--pm); font: inherit; font-weight: 600; cursor: pointer; display: inline-flex; align-items: center; justify-content: center; gap: 6px; }
.tab[aria-selected="true"] { background: var(--ps); color: var(--pf); box-shadow: 0 1px 3px rgb(0 0 0 / 0.12); }
.nav { display: flex; align-items: stretch; background: var(--ps); border-top: 1px solid var(--pl); }
.nav .tab { flex-direction: column; gap: 2px; font-size: 11px; font-weight: 500; border-radius: 0; }
.nav .tab[aria-selected="true"] { color: var(--pa); background: transparent; box-shadow: none; }
.list { overflow: auto; border: 1px solid var(--pl); border-radius: 12px; background: var(--ps); }
.row { width: 100%; display: flex; align-items: center; gap: 10px; min-height: 52px; padding: 8px 14px; border: 0; border-bottom: 1px solid var(--pl);
  background: transparent; color: var(--pf); font: inherit; text-align: left; }
.row:last-child { border-bottom: 0; } .row .main { flex: 1; min-width: 0; display: grid; gap: 2px; }
.row .t { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } .row .s { color: var(--pm); font-size: 13px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.row.hot { cursor: pointer; } @media (hover: hover) and (pointer: fine) { .row.hot:hover { background: var(--ps2); } }
.card { border: 1px solid var(--pl); border-radius: 14px; background: var(--ps); padding: 14px; display: grid; gap: 6px; align-content: start; overflow: hidden;
  box-shadow: 0 6px 18px -14px rgb(0 0 0 / 0.4); }
.card .t { font-weight: 600; font-size: 16px; display: flex; gap: 8px; align-items: center; } .card .b { color: var(--pm); white-space: pre-wrap; }
.table { overflow: auto; border: 1px solid var(--pl); border-radius: 12px; }
.table table { width: 100%; border-collapse: collapse; font-size: 13px; }
.table th, .table td { padding: 8px 10px; border-bottom: 1px solid var(--pl); text-align: left; white-space: nowrap; }
.table th { background: var(--ps2); color: var(--pm); font-weight: 600; position: sticky; top: 0; }
.toastc { display: flex; align-items: center; gap: 8px; padding: 0 14px; border-radius: 12px; background: var(--pf); color: var(--ps); }
.avatar { border-radius: 50%; background: var(--pas); color: var(--pa); display: grid; place-items: center; font-weight: 700; overflow: hidden; }
.badge { border-radius: 99px; background: var(--pas); color: var(--pa); display: inline-flex; align-items: center; justify-content: center; padding: 0 10px; font-size: 12px; font-weight: 600; }
.progress { display: grid; align-content: center; gap: 4px; } .progress .track { height: 8px; border-radius: 99px; background: var(--ps2); overflow: hidden; }
.progress .fill { height: 100%; border-radius: 99px; background: var(--pa); transition: width 240ms ${EASE}; }
.chart { display: flex; align-items: flex-end; gap: 6px; padding: 8px 4px 0; }
.chart .c { flex: 1; display: grid; gap: 4px; justify-items: center; align-self: stretch; grid-template-rows: 1fr auto; }
.chart .b { width: 100%; align-self: end; border-radius: 6px 6px 0 0; background: var(--pa); min-height: 2px; }
.chart .l { font-size: 11px; color: var(--pm); max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ico { display: inline-grid; place-items: center; flex: none; }
.entity, .embed { border: 1px solid var(--pl); border-radius: 12px; background: var(--ps); padding: 10px 12px; display: grid; gap: 2px; align-content: center; overflow: hidden; }
.entity .k, .embed .k { color: var(--pm); font-size: 12px; } .entity .n, .embed .n { font-weight: 600; overflow: hidden; text-overflow: ellipsis; }
/* sketch look: a wireframe */
.sketch { --pa: var(--pf); --pas: var(--ps2); font-family: var(--font-hand, "Comic Sans MS", "Segoe Print", "Bradley Hand", cursive); }
.sketch .btn.primary { background: var(--pf); color: var(--ps); }
.sketch .field, .sketch .list, .sketch .card, .sketch .table, .sketch .btn.secondary, .sketch .tabs, .sketch .entity, .sketch .embed { border: 2px solid var(--pf); box-shadow: none; }
.sketch .chart .b, .sketch .progress .fill { background: repeating-linear-gradient(45deg, var(--pf) 0 2px, transparent 2px 7px); border: 2px solid var(--pf); }
/* comments */
.pin { position: absolute; z-index: 15; width: 26px; height: 26px; margin: -26px 0 0 0; border-radius: 13px 13px 13px 2px; background: var(--pa); color: var(--pon);
  display: grid; place-items: center; font-size: 12px; font-weight: 700; box-shadow: 0 4px 10px -4px rgb(0 0 0 / 0.5); }
.cform { position: absolute; z-index: 30; width: 260px; padding: 10px; display: grid; gap: 8px; background: var(--ps); border: 1px solid var(--pl); border-radius: 12px;
  box-shadow: 0 14px 34px -14px rgb(0 0 0 / 0.45); }
.cform textarea { width: 100%; min-height: 70px; resize: vertical; font: inherit; color: var(--pf); background: var(--ps); border: 1px solid var(--pl); border-radius: 8px; padding: 8px; }
.cform .acts { display: flex; gap: 6px; justify-content: flex-end; }
.cform button { height: 30px; padding: 0 12px; border-radius: 8px; border: 1px solid var(--pl); background: var(--ps); color: var(--pf); font: inherit; cursor: pointer; }
.cform button.go { background: var(--pa); color: var(--pon); border-color: transparent; }
.commenting .device { cursor: crosshair; }
`;

  // ---- helpers ---------------------------------------------------------------

  function h(tag, cls, kids) {
    const n = document.createElement(tag);
    if (cls) n.className = cls;
    for (const k of kids || []) n.append(k);
    return n;
  }
  function icon(name, size) {
    const s = h("span", "ico");
    const body = ICONS[name];
    if (!body) return s;
    // Our own icon markup (a fixed list), never text from the board.
    s.innerHTML = `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${body}</svg>`;
    return s;
  }
  const num = (v, d = 0) => (typeof v === "number" && isFinite(v) ? v : d);
  const byOrder = (a, b) => (a.order || "").localeCompare(b.order || "") || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0);
  const safeURL = (u) => /^(https?:|mailto:)/i.test(String(u || "").trim());
  const devClass = (d) => (d === "phone" || d === "tablet" || d === "desktop" ? d : "custom");

  function boxOf(e) {
    if (e.points && e.points.length) {
      let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
      for (const [x, y] of e.points) { x0 = Math.min(x0, x); y0 = Math.min(y0, y); x1 = Math.max(x1, x); y1 = Math.max(y1, y); }
      return { x: x0, y: y0, w: x1 - x0, h: y1 - y0 };
    }
    return { x: num(e.x), y: num(e.y), w: num(e.w), h: num(e.h) };
  }

  /** Screens of a prototype and the elements on each (by frame, else by position). */
  function analyse(board, protoId) {
    const els = (board.elements || []).slice().sort(byOrder);
    const byId = new Map(els.map((e) => [e.id, e]));
    const screens = els.filter((e) => e.type === "frame" && e.screen && e.screen.proto === protoId);
    const ids = new Set(screens.map((s) => s.id));
    const on = new Map(screens.map((s) => [s.id, []]));
    for (const e of els) {
      if (e.screen) continue;
      let f = e.frame, n = 0, home = "";
      while (f && n++ < 20) { if (ids.has(f)) { home = f; break; } f = byId.get(f)?.frame; }
      if (!home) {
        const b = boxOf(e), cx = b.x + b.w / 2, cy = b.y + b.h / 2;
        const s = screens.find((s) => cx >= num(s.x) && cx <= num(s.x) + num(s.w) && cy >= num(s.y) && cy <= num(s.y) + num(s.h));
        if (s) home = s.id;
      }
      if (home) on.get(home).push(e);
    }
    const order = screens.slice().sort((a, b) => num(a.y) - num(b.y) || num(a.x) - num(b.x));
    return { screens: order, on, byId };
  }

  /**
   * Draws elements and components. env: state (variables), varType(name),
   * data(), files(), fileURL(id), run(actions, on, ctx), act(action, ctx),
   * refresh() (after a variable changed), active() (the focused element).
   */
  function renderer(env) {
    const state = env.state;
  const fmtVal = (v) => (Array.isArray(v) ? v.join(", ") : v === true ? "✓" : v === false || v === null || v === undefined ? "" : String(v));
  function fmt(s, ctx) {
    return String(s || "").replace(/\{\{\s*([A-Za-z_][\w]*)(?:\.([\w]+))?\s*\}\}/g, (all, a, b) => {
      if (a === "row" && ctx && ctx.row) return fmtVal(b ? ctx.row[b] : "");
      if (a === "item" && ctx && ctx.item !== undefined) return fmtVal(ctx.item);
      if (!(a in state)) return all;
      const v = state[a];
      if (b === "length") return String(Array.isArray(v) ? v.length : String(v ?? "").length);
      return fmtVal(v);
    });
  }
  function truthy(v) { return Array.isArray(v) ? v.length > 0 : !!v && v !== "0"; }
  function holds(cond) {
    if (!cond) return true;
    const not = cond.startsWith("!");
    const c = not ? cond.slice(1) : cond;
    const i = c.indexOf("=");
    const r = i < 0 ? truthy(state[c]) : fmtVal(state[c.slice(0, i)]) === c.slice(i + 1);
    return not ? !r : r;
  }
  function rowsOf(u) {
    if (u.bind && Array.isArray(state[u.bind])) return state[u.bind].map((x) => (x && typeof x === "object" ? x : { title: x }));
    if (u.data && env.data() && Array.isArray(env.data()[u.data])) return env.data()[u.data];
    return (u.options || []).map((o) => ({ title: o }));
  }
  function setVar(name, value, from) {
    if (!name) return;
    state[name] = value;
    env.refresh();
  }
  function parseFor(name, raw) {
    const t = env.varType(name);
    if (t === "number") { const n = Number(raw); return isFinite(n) ? n : 0; }
    if (t === "bool") return raw === true || raw === "true" || raw === "1";
    if (t === "list") return Array.isArray(raw) ? raw : String(raw).split(",").map((x) => x.trim()).filter(Boolean);
    return raw === undefined || raw === null ? "" : String(raw);
  }

  function drawEl(e, sx, sy, ups) {
    const b = boxOf(e);
    const wrap = h("div", "el");
    if (e.opacity !== undefined && e.opacity < 1) wrap.style.opacity = String(e.opacity);
    const place = (x, y, w, hh) => Object.assign(wrap.style, { left: x - sx + "px", top: y - sy + "px", width: w + "px", height: hh + "px" });
    const label = (n, ctx) => ups.push(() => { n.textContent = fmt(e.text, ctx); });
    const t = e.type;
    if (t === "line" || t === "arrow" || t === "pen") {
      place(b.x - 20, b.y - 20, b.w + 40, b.h + 40);
      const pts = e.points.map(([x, y]) => [x - b.x + 20, y - b.y + 20]);
      const NS = "http://www.w3.org/2000/svg";
      const svg = document.createElementNS(NS, "svg");
      svg.setAttribute("width", String(b.w + 40));
      svg.setAttribute("height", String(b.h + 40));
      svg.style.overflow = "visible";
      const path = document.createElementNS(NS, "path");
      let d = `M${pts[0][0]} ${pts[0][1]}`;
      if (t === "pen") for (let i = 1; i < pts.length - 1; i++) { const mx = (pts[i][0] + pts[i + 1][0]) / 2, my = (pts[i][1] + pts[i + 1][1]) / 2; d += ` Q${pts[i][0]} ${pts[i][1]} ${mx} ${my}`; }
      else for (const p of pts.slice(1)) d += ` L${p[0]} ${p[1]}`;
      if (t === "pen" && pts.length > 1) d += ` L${pts[pts.length - 1][0]} ${pts[pts.length - 1][1]}`;
      if (t === "arrow" && pts.length > 1) {
        const [a, z] = [pts[pts.length - 2], pts[pts.length - 1]];
        const ang = Math.atan2(z[1] - a[1], z[0] - a[0]);
        d += ` M${z[0] - 14 * Math.cos(ang - 0.45)} ${z[1] - 14 * Math.sin(ang - 0.45)} L${z[0]} ${z[1]} L${z[0] - 14 * Math.cos(ang + 0.45)} ${z[1] - 14 * Math.sin(ang + 0.45)}`;
      }
      path.setAttribute("d", d);
      path.setAttribute("fill", "none");
      path.setAttribute("stroke", tc(e.stroke));
      path.setAttribute("stroke-width", String(num(e.width, 2)));
      path.setAttribute("stroke-linecap", "round");
      path.setAttribute("stroke-linejoin", "round");
      if (e.dash) path.setAttribute("stroke-dasharray", e.dash === "dotted" ? "2 6" : "10 8");
      svg.append(path);
      wrap.append(svg);
      wrap.style.pointerEvents = "none";
      return wrap;
    }
    place(b.x, b.y, b.w, b.h);
    if (e.ui) drawUI(e, wrap, ups);
    else if (t === "rect" || t === "ellipse" || t === "frame") {
      const sh = h("div", "txt");
      const bw = t === "frame" ? (e.stroke ? 1.5 : 0) : num(e.width, 2);
      Object.assign(sh.style, { border: bw ? `${bw}px ${e.dash === "dotted" ? "dotted" : e.dash ? "dashed" : "solid"} ${tc(e.stroke)}` : "0", background: tb(e.fill),
        borderRadius: t === "ellipse" ? "50%" : t === "frame" ? "12px" : e.round ? "12px" : "4px", fontSize: num(e.fontSize, 18) + "px" });
      if (e.align) sh.style.textAlign = e.align;
      if (t !== "frame") label(sh);
      wrap.append(sh);
    } else if (t === "diamond") {
      const NS = "http://www.w3.org/2000/svg";
      const svg = document.createElementNS(NS, "svg");
      svg.setAttribute("viewBox", `0 0 ${b.w} ${b.h}`);
      Object.assign(svg.style, { position: "absolute", inset: "0", width: "100%", height: "100%" });
      const poly = document.createElementNS(NS, "polygon");
      poly.setAttribute("points", `${b.w / 2},1 ${b.w - 1},${b.h / 2} ${b.w / 2},${b.h - 1} 1,${b.h / 2}`);
      poly.setAttribute("fill", tb(e.fill));
      poly.setAttribute("stroke", tc(e.stroke));
      poly.setAttribute("stroke-width", String(num(e.width, 2)));
      svg.append(poly);
      const sh = h("div", "txt");
      Object.assign(sh.style, { position: "absolute", inset: "18%", width: "auto", height: "auto", fontSize: num(e.fontSize, 18) + "px" });
      label(sh);
      wrap.append(svg, sh);
    } else if (t === "sticky") {
      const sh = h("div", "txt top");
      Object.assign(sh.style, { background: tb(e.fill || "yellow"), padding: "12px", borderRadius: "4px", boxShadow: "0 6px 14px -10px rgb(0 0 0 / .5)",
        color: "#1f1b17", fontFamily: 'var(--font-hand, "Comic Sans MS", cursive)', fontSize: num(e.fontSize, 18) + "px" });
      label(sh);
      wrap.append(sh);
    } else if (t === "text") {
      const sh = h("div", "txt top");
      Object.assign(sh.style, { color: e.stroke && e.stroke !== "gray" ? tc(e.stroke) : "var(--pf)", fontSize: num(e.fontSize, 20) + "px", padding: "0", overflow: "visible" });
      if (e.align) { sh.style.textAlign = e.align; sh.style.justifyContent = e.align === "center" ? "center" : e.align === "right" ? "flex-end" : "flex-start"; }
      label(sh);
      wrap.append(sh);
    } else if (t === "image") {
      const src = (env.files() && env.files()[e.file]) || (env.fileURL ? env.fileURL(e.file) : "");
      if (src) {
        const img = h("img");
        img.src = src;
        img.alt = e.text || "";
        Object.assign(img.style, { width: "100%", height: "100%", objectFit: "cover", display: "block" });
        wrap.append(img);
      }
    } else if (t === "entity") {
      wrap.append(h("div", "entity ui", [h("span", "k", [e.entity || ""]), h("span", "n", [e.text || e.entity || ""])]));
    } else if (t === "embed") {
      const pv = e.preview || {};
      const card = h("div", "embed ui", [h("span", "k", [pv.site || ""]), h("span", "n", [pv.title || e.text || e.url || ""])]);
      if (safeURL(e.url)) { card.classList.add("hot"); card.addEventListener("click", () => env.act({ do: "link", value: e.url })); }
      wrap.append(card);
    } else {
      return null;
    }
    if (e.ui && e.ui.show) ups.push(() => { wrap.style.display = holds(e.ui.show) ? "" : "none"; });
    const acts = e.actions || [];
    if (!e.ui && acts.length) {
      if (acts.some((a) => a.on === "click")) {
        wrap.classList.add("hot");
        wrap.addEventListener("click", (ev) => { ev.stopPropagation(); env.run(acts, "click", {}); });
      }
      if (acts.some((a) => a.on === "hover")) wrap.addEventListener("mouseenter", () => env.run(acts, "hover", {}));
    }
    return wrap;
  }

  function drawUI(e, wrap, ups) {
    const u = e.ui;
    const acts = e.actions || [];
    const clickable = acts.some((a) => a.on === "click");
    const click = (n, ctx) => {
      n.addEventListener("click", (ev) => { ev.stopPropagation(); env.run(acts, "click", ctx || {}); });
    };
    const changed = (ctx) => env.run(acts, "change", ctx || {});
    if (acts.some((a) => a.on === "hover")) wrap.addEventListener("mouseenter", () => env.run(acts, "hover", {}));
    const fs = (px) => { wrap.style.fontSize = num(e.fontSize, px) + "px"; };
    const label = (n) => ups.push(() => { n.textContent = fmt(e.text); });
    const valueOf = () => (u.bind && u.bind in state ? state[u.bind] : u.value);
    let n;
    switch (u.comp) {
      case "button": {
        n = h("button", `ui btn ${["primary", "secondary", "ghost", "danger"].includes(u.variant) ? u.variant : "primary"} hot`);
        n.type = "button";
        if (u.icon) n.append(icon(u.icon, 18));
        const l = h("span");
        label(l);
        n.append(l);
        click(n);
        fs(15);
        break;
      }
      case "link": {
        n = h("a", "ui link hot");
        n.href = "#";
        label(n);
        n.addEventListener("click", (ev) => { ev.preventDefault(); ev.stopPropagation(); env.run(acts, "click", {}); });
        fs(15);
        break;
      }
      case "input":
      case "textarea": {
        n = h(u.comp === "input" ? "input" : "textarea", "ui field");
        if (u.comp === "input") n.type = env.varType(u.bind) === "number" ? "number" : "text";
        n.placeholder = u.placeholder || e.text || "";
        n.setAttribute("aria-label", e.text || u.placeholder || "");
        ups.push(() => { if (env.active() !== n) n.value = fmtVal(valueOf() ?? ""); });
        n.addEventListener("input", () => { if (u.bind) { state[u.bind] = parseFor(u.bind, n.value); env.refresh(); } });
        n.addEventListener("change", () => changed({ item: n.value }));
        n.addEventListener("keydown", (ev) => { if (ev.key === "Enter" && u.comp === "input") { changed({ item: n.value }); env.run(acts, "click", { item: n.value }); } });
        n.addEventListener("click", (ev) => ev.stopPropagation());
        fs(15);
        break;
      }
      case "checkbox":
      case "switch": {
        const box = h("input");
        box.type = "checkbox";
        if (u.comp === "switch") box.setAttribute("role", "switch");
        const t = h("span", "t");
        label(t);
        n = h("label", `ui labelled ${u.comp}`, u.comp === "switch" ? [t, box, h("span", "sw")] : [box, t]);
        ups.push(() => { box.checked = truthy(valueOf()); });
        box.addEventListener("change", () => { if (u.bind) setVar(u.bind, box.checked); changed({ item: box.checked }); });
        n.addEventListener("click", (ev) => ev.stopPropagation());
        fs(15);
        break;
      }
      case "radio": {
        n = h("div", "ui radios");
        n.setAttribute("role", "radiogroup");
        const name = "r" + e.id;
        for (const o of u.options || []) {
          const r = h("input");
          r.type = "radio";
          r.name = name;
          r.value = o;
          ups.push(() => { r.checked = fmtVal(valueOf()) === o; });
          r.addEventListener("change", () => { if (u.bind) setVar(u.bind, parseFor(u.bind, o)); changed({ item: o }); });
          n.append(h("label", "labelled", [r, h("span", "", [o])]));
        }
        n.addEventListener("click", (ev) => ev.stopPropagation());
        fs(15);
        break;
      }
      case "select": {
        n = h("select", "ui field");
        n.setAttribute("aria-label", e.text || "");
        const fill = () => {
          const field = (u.fields || [])[0];
          const opts = u.data ? rowsOf(u).map((r) => fmtVal(field ? r[field] : r.title ?? r.name ?? Object.values(r)[0])) : u.options || [];
          const want = fmtVal(valueOf() ?? "");
          n.textContent = "";
          if (u.placeholder) { const p = h("option", "", [u.placeholder]); p.value = ""; n.append(p); }
          for (const o of opts) { const op = h("option", "", [o]); op.value = o; n.append(op); }
          n.value = want;
        };
        ups.push(fill);
        n.addEventListener("change", () => { if (u.bind) setVar(u.bind, parseFor(u.bind, n.value)); changed({ item: n.value }); });
        n.addEventListener("click", (ev) => ev.stopPropagation());
        fs(15);
        break;
      }
      case "slider": {
        const r = h("input");
        r.type = "range";
        r.min = String(num(u.min, 0));
        r.max = String(num(u.max, 100));
        r.setAttribute("aria-label", e.text || "");
        const lab = h("span"), val = h("span");
        n = h("div", "ui slider", [h("div", "v", [lab, val]), r]);
        ups.push(() => { lab.textContent = fmt(e.text); const v = num(Number(valueOf()), num(u.min, 0)); if (env.active() !== r) r.value = String(v); val.textContent = String(v); });
        r.addEventListener("input", () => { if (u.bind) setVar(u.bind, Number(r.value)); else val.textContent = r.value; });
        r.addEventListener("change", () => changed({ item: Number(r.value) }));
        n.addEventListener("click", (ev) => ev.stopPropagation());
        fs(14);
        break;
      }
      case "tabs":
      case "navbar": {
        n = h("div", `ui ${u.comp === "tabs" ? "tabs" : "nav"}`);
        n.setAttribute("role", "tablist");
        let local = (u.options || [])[0] || "";
        (u.options || []).forEach((o, i) => {
          const b = h("button", "tab hot");
          b.type = "button";
          b.setAttribute("role", "tab");
          const ic = (u.fields || [])[i];
          if (ic) b.append(icon(ic, u.comp === "navbar" ? 22 : 16));
          b.append(h("span", "", [o]));
          ups.push(() => { const cur = u.bind ? fmtVal(state[u.bind]) || local : local; b.setAttribute("aria-selected", String(cur === o)); });
          b.addEventListener("click", (ev) => {
            ev.stopPropagation();
            local = o;
            if (u.bind) setVar(u.bind, parseFor(u.bind, o)); else env.refresh();
            env.run(acts, "click", { item: o });
            changed({ item: o });
          });
          n.append(b);
        });
        fs(u.comp === "tabs" ? 14 : 11);
        break;
      }
      case "list": {
        n = h("div", "ui list");
        const fill = () => {
          n.textContent = "";
          const [tf, sf] = u.fields || [];
          for (const row of rowsOf(u)) {
            const titleV = fmtVal(tf ? row[tf] : row.title ?? row.name ?? Object.values(row)[0]);
            const sub = sf ? fmtVal(row[sf]) : "";
            const r = h(clickable ? "button" : "div", "row" + (clickable ? " hot" : ""));
            if (clickable) r.type = "button";
            if (u.icon) r.append(icon(u.icon, 20));
            r.append(h("span", "main", [h("span", "t", [titleV]), ...(sub ? [h("span", "s", [sub])] : [])]));
            if (clickable) { r.append(icon("chevron-right", 18)); click(r, { row, item: titleV }); }
            n.append(r);
          }
        };
        ups.push(fill);
        fs(15);
        break;
      }
      case "card": {
        const t = h("span");
        label(t);
        const body = h("div", "b");
        ups.push(() => { body.textContent = fmt(u.value || ""); body.style.display = u.value ? "" : "none"; });
        n = h("div", "ui card" + (clickable ? " hot" : ""), [h("div", "t", [...(u.icon ? [icon(u.icon, 18)] : []), t]), body]);
        if (clickable) click(n);
        fs(14);
        break;
      }
      case "table": {
        n = h("div", "ui table");
        const fill = () => {
          const rows = rowsOf(u);
          const cols = u.fields && u.fields.length ? u.fields : rows[0] ? Object.keys(rows[0]).slice(0, 8) : [];
          const tbl = h("table");
          const head = h("tr", "", cols.map((c) => h("th", "", [c])));
          tbl.append(h("thead", "", [head]));
          const tbody = h("tbody");
          for (const row of rows) {
            const tr = h("tr", clickable ? "hot" : "", cols.map((c) => h("td", "", [fmtVal(row[c])])));
            if (clickable) click(tr, { row, item: fmtVal(row[cols[0]]) });
            tbody.append(tr);
          }
          tbl.append(tbody);
          n.textContent = "";
          n.append(tbl);
        };
        ups.push(fill);
        break;
      }
      case "toast": {
        const t = h("span");
        label(t);
        n = h("div", "ui toastc", [...(u.icon ? [icon(u.icon, 18)] : []), t]);
        fs(14);
        break;
      }
      case "icon": {
        n = h("div", "ui" + (clickable ? " hot" : ""));
        Object.assign(n.style, { display: "grid", placeItems: "center", color: e.stroke ? tc(e.stroke) : "var(--pf)" });
        n.append(icon(u.icon || "circle-help", Math.max(12, Math.min(num(e.w, 24), num(e.h, 24)))));
        if (e.text) n.setAttribute("aria-label", e.text);
        if (clickable) click(n);
        break;
      }
      case "avatar": {
        n = h("div", "ui avatar" + (clickable ? " hot" : ""));
        ups.push(() => { n.textContent = fmt(e.text).split(/\s+/).filter(Boolean).slice(0, 2).map((w) => w[0].toUpperCase()).join(""); });
        wrap.style.fontSize = Math.round(Math.min(num(e.w, 40), num(e.h, 40)) * 0.38) + "px";
        if (clickable) click(n);
        break;
      }
      case "badge": {
        n = h("div", "ui badge");
        label(n);
        break;
      }
      case "progress": {
        const fill = h("div", "fill");
        const lab = h("span");
        n = h("div", "ui progress", [lab, h("div", "track", [fill])]);
        ups.push(() => {
          lab.textContent = fmt(e.text);
          lab.style.display = e.text ? "" : "none";
          const max = num(u.max, 100) || 100;
          fill.style.width = Math.max(0, Math.min(100, (num(Number(valueOf()), 0) / max) * 100)) + "%";
        });
        fs(13);
        break;
      }
      case "chart": {
        n = h("div", "ui chart");
        const fill = () => {
          n.textContent = "";
          const [lf, vf] = u.fields || [];
          let rows = rowsOf(u);
          if (!rows.length || !vf) rows = [3, 5, 4, 7, 6].map((v, i) => ({ l: String(i + 1), v }));
          const vals = rows.map((r) => num(Number(vf ? r[vf] : r.v), 0));
          const max = Math.max(1, ...vals);
          rows.slice(0, 24).forEach((r, i) => {
            const bar = h("div", "b");
            bar.style.height = Math.max(2, (vals[i] / max) * 100) + "%";
            bar.title = fmtVal(vals[i]);
            n.append(h("div", "c", [bar, h("span", "l", [fmtVal(lf ? r[lf] : r.l ?? "")])]));
          });
        };
        ups.push(fill);
        break;
      }
      default:
        n = h("div", "ui");
    }
    wrap.append(n);
    if (clickable && !["button", "link", "list", "table", "tabs", "navbar", "card", "icon", "avatar"].includes(u.comp)) {
      wrap.classList.add("hot");
      click(wrap);
    }
  }

    return { drawEl, fmt, fmtVal, truthy, holds, setVar, parseFor };
  }

  // ---- the player ------------------------------------------------------------

  function mount(root, model, hooks) {
    hooks = hooks || {};
    const shadow = root.shadowRoot || root.attachShadow({ mode: "open" });
    shadow.textContent = "";
    const style = h("style");
    style.textContent = CSS;
    shadow.append(style);
    const lang = String(model.lang || (typeof navigator !== "undefined" ? navigator.language : "en")).slice(0, 2);
    const S = Object.assign({}, STR.en, STR[lang] || {}, model.strings || {});

    let m = model;
    let proto = (m.board.prototypes || []).find((p) => p.id === m.proto) || { id: m.proto, name: "", vars: [], data: [] };
    let info = analyse(m.board, proto.id);
    const state = {};
    const resetState = () => {
      for (const k of Object.keys(state)) delete state[k];
      for (const v of proto.vars || []) {
        state[v.name] = v.value !== undefined && v.value !== null ? JSON.parse(JSON.stringify(v.value))
          : v.type === "bool" ? false : v.type === "number" ? 0 : v.type === "list" ? [] : "";
      }
    };
    resetState();
    const varType = (name) => (proto.vars || []).find((v) => v.name === name)?.type;

    const stage = h("div", "stage");
    const bar = h("div", "bar");
    const view = h("div", "view");
    stage.append(bar, view);
    shadow.append(stage);

    const title = h("span", "name");
    const pick = h("select");
    pick.setAttribute("aria-label", S.screens);
    const restartBtn = h("button", "", [icon("refresh-cw", 15), h("span", "lbl", [S.restart])]);
    restartBtn.type = "button";
    const commentBtn = h("button", "", [icon("message-circle", 15), h("span", "lbl", [S.comment])]);
    commentBtn.type = "button";
    commentBtn.setAttribute("aria-pressed", "false");
    const closeBtn = h("button", "", [icon("x", 15), h("span", "lbl", [S.close])]);
    closeBtn.type = "button";
    bar.append(title, pick, restartBtn);
    if (hooks.onComment) bar.append(commentBtn);
    if (hooks.onClose) bar.append(closeBtn);
    if (hooks.bar === false) bar.style.display = "none";

    const device = h("div", "device");
    let base = null; // { id, node, cleanup, refresh }
    let overlays = [];
    const hist = [];
    let commenting = false;
    let hintTimer = 0;

    // -- text and state --
    function refreshAll() {
      base && base.refresh();
      for (const o of overlays) o.refresh();
    }
    const R = renderer({
      state, varType, data: () => m.data, files: () => m.files, fileURL: hooks.fileURL,
      run: (l, on, ctx) => run(l, on, ctx), act: (a, ctx) => act(a, ctx), refresh: refreshAll, active: () => shadow.activeElement,
    });
    const { fmt, truthy, setVar, parseFor } = R;

    // -- actions --
    function run(actions, on, ctx) {
      for (const a of actions || []) {
        if (a.on !== on) continue;
        if (a.item && (!ctx || String(ctx.item ?? "") !== a.item)) continue;
        act(a, ctx);
      }
    }
    function act(a, ctx) {
      const val = fmt(a.value ?? "", ctx);
      switch (a.do) {
        case "goto": return go(a.target, a.anim);
        case "back": return back();
        case "overlay": return openOverlay(a.target);
        case "close": return closeOverlay();
        case "set": return setVar(a.target, parseFor(a.target, val));
        case "toggle": return setVar(a.target, !truthy(state[a.target]));
        case "inc": return setVar(a.target, num(Number(state[a.target]), 0) + (val === "" ? 1 : num(Number(val), 0)));
        case "add": {
          if (val.trim() === "") return;
          const list = Array.isArray(state[a.target]) ? state[a.target].slice() : [];
          list.push(val);
          return setVar(a.target, list);
        }
        case "link":
          if (safeURL(val)) {
            if (hooks.onLink) hooks.onLink(val);
            else window.open(val, "_blank", "noopener,noreferrer");
          }
          return;
        case "toast": return toast(val);
      }
    }
    function toast(text) {
      if (!text) return;
      const t = h("div", "toast", [text]);
      device.append(t);
      const k = reduced() ? [{ opacity: 0 }, { opacity: 1 }] : [{ opacity: 0, transform: "translate(-50%, 8px)" }, { opacity: 1, transform: "translate(-50%, 0)" }];
      t.animate?.(k, { duration: 200, easing: EASE });
      setTimeout(() => {
        const out = t.animate?.([{ opacity: 1 }, { opacity: 0 }], { duration: 180, easing: "ease" });
        if (out) out.onfinish = () => t.remove(); else t.remove();
      }, 2600);
    }

    // -- drawing a screen --
    function buildScreen(s) {
      const node = h("div", "layer");
      node.style.width = num(s.w) + "px";
      node.style.height = num(s.h) + "px";
      if (s.fill) node.style.background = tb(s.fill);
      const ups = [];
      const timers = [];
      const sx = num(s.x), sy = num(s.y);
      for (const e of info.on.get(s.id) || []) {
        const n = R.drawEl(e, sx, sy, ups);
        if (n) node.append(n);
      }
      const refresh = () => { for (const u of ups) u(); };
      refresh();
      const start = () => {
        run(s.actions, "open");
        for (const a of s.actions || []) if (a.on === "delay") timers.push(setTimeout(() => act(a), Math.max(0, num(a.delay, 1000))));
      };
      return { id: s.id, screen: s, node, refresh, start, cleanup: () => timers.forEach(clearTimeout) };
    }

    // -- navigation --
    function fit() {
      if (!base) return;
      const s = base.screen;
      const w = num(s.w, 390), hh = num(s.h, 844);
      const r = view.getBoundingClientRect();
      const k = Math.min((r.width - 24) / w, (r.height - 24) / hh, s.screen?.device === "desktop" ? 1 : 1.25);
      device.style.width = w + "px";
      device.style.height = hh + "px";
      device.style.transform = `translate(-50%, -50%) scale(${Math.max(0.05, k)})`;
      device.dataset.k = String(k);
    }
    function setBase(s, anim, backwards) {
      const next = buildScreen(s);
      const prev = base;
      base = next;
      device.className = `device ${devClass(s.screen?.device)}${proto.look === "sketch" ? " sketch" : ""}`;
      device.prepend(next.node);
      fit();
      pick.value = s.id;
      if (prev) {
        prev.cleanup();
        const mode = !anim ? "" : reduced() ? "fade" : anim;
        if (mode === "slide") {
          const dir = backwards ? -1 : 1;
          next.node.animate([{ transform: `translateX(${dir * 100}%)` }, { transform: "translateX(0)" }], { duration: 260, easing: EASE });
          const out = prev.node.animate([{ transform: "translateX(0)" }, { transform: `translateX(${-dir * 30}%)` }], { duration: 260, easing: EASE });
          device.append(prev.node);
          device.insertBefore(next.node, prev.node.nextSibling);
          out.onfinish = () => prev.node.remove();
        } else if (mode === "fade") {
          device.append(next.node);
          const a = next.node.animate([{ opacity: 0 }, { opacity: 1 }], { duration: reduced() ? 150 : 200, easing: "ease" });
          a.onfinish = () => prev.node.remove();
        } else {
          prev.node.remove();
        }
      }
      next.start();
      paintPins();
    }
    function screenById(id) { return info.screens.find((s) => s.id === id); }
    function go(id, anim) {
      const s = screenById(id);
      if (!s) return;
      if (s.screen?.overlay) return openOverlay(id);
      while (overlays.length) closeOverlay(true);
      if (base) hist.push(base.id);
      setBase(s, anim, false);
    }
    function back() {
      if (overlays.length) return closeOverlay();
      const id = hist.pop();
      const s = id && screenById(id);
      if (s) setBase(s, "slide", true);
    }
    function openOverlay(id) {
      const s = screenById(id);
      if (!s) return;
      const kind = s.screen?.overlay === "sheet" ? "sheet" : "modal";
      const layer = buildScreen(s);
      const ov = h("div", `ov ${kind}`);
      const dw = num(base?.screen.w, 390), dh = num(base?.screen.h, 844);
      const w = Math.min(num(s.w), dw), hh = Math.min(num(s.h), dh);
      Object.assign(layer.node.style, kind === "sheet"
        ? { left: (dw - w) / 2 + "px", top: dh - hh + "px", width: w + "px", height: hh + "px" }
        : { left: (dw - w) / 2 + "px", top: (dh - hh) / 2 + "px", width: w + "px", height: hh + "px" });
      ov.append(layer.node);
      ov.addEventListener("click", (ev) => { if (ev.target === ov && !commenting) closeOverlay(); });
      device.append(ov);
      const o = Object.assign(layer, { ov, kind });
      overlays.push(o);
      ov.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 180, easing: "ease" });
      if (!reduced()) {
        layer.node.animate(kind === "sheet" ? [{ transform: "translateY(100%)" }, { transform: "translateY(0)" }] : [{ transform: "scale(0.96)", opacity: 0 }, { transform: "scale(1)", opacity: 1 }],
          { duration: kind === "sheet" ? 300 : 200, easing: kind === "sheet" ? DRAWER : EASE });
      }
      o.start();
      paintPins();
    }
    function closeOverlay(now) {
      const o = overlays.pop();
      if (!o) return;
      o.cleanup();
      if (now || reduced()) { o.ov.remove(); paintPins(); return; }
      const a = o.ov.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 160, easing: "ease" });
      if (o.kind === "sheet") o.node.animate([{ transform: "translateY(0)" }, { transform: "translateY(100%)" }], { duration: 200, easing: EASE });
      a.onfinish = () => o.ov.remove();
      paintPins();
    }

    function startScreen() {
      const cand = info.screens.filter((s) => !s.screen?.overlay);
      return screenById(hooks.start) || screenById(proto.start) || cand[0] || info.screens[0];
    }
    function restart() {
      resetState();
      hist.length = 0;
      while (overlays.length) closeOverlay(true);
      base?.cleanup();
      base?.node.remove();
      base = null;
      const s = startScreen();
      if (s) setBase(s);
    }

    function fillPicker() {
      pick.textContent = "";
      for (const s of info.screens) {
        if (s.screen?.overlay) continue;
        const o = h("option", "", [s.text || s.id]);
        o.value = s.id;
        pick.append(o);
      }
      pick.style.display = pick.options.length > 1 ? "" : "none";
      title.textContent = proto.name || "";
    }

    // -- comments --
    let pins = [];
    function topLayer() { return overlays.length ? overlays[overlays.length - 1] : base; }
    function paintPins() {
      for (const p of pins) p.remove();
      pins = [];
      if (!commenting) return;
      for (const l of [base, ...overlays]) {
        if (!l) continue;
        const s = l.screen;
        let i = 0;
        for (const c of m.board.comments || []) {
          if (c.element !== s.id || c.resolved) continue;
          const p = h("div", "pin", [String(++i)]);
          p.title = c.text || "";
          Object.assign(p.style, { left: c.x - num(s.x) + "px", top: c.y - num(s.y) + "px" });
          l.node.append(p);
          pins.push(p);
        }
      }
    }
    let form = null;
    function closeForm() { form?.remove(); form = null; }
    function commentAt(ev) {
      const l = topLayer();
      if (!l) return;
      const k = Number(device.dataset.k) || 1;
      const r = l.node.getBoundingClientRect();
      const x = (ev.clientX - r.left) / k, y = (ev.clientY - r.top) / k;
      if (x < 0 || y < 0 || x > num(l.screen.w) || y > num(l.screen.h)) return;
      closeForm();
      const ta = h("textarea");
      ta.setAttribute("aria-label", S.comment);
      const ok = h("button", "go", [S.send]);
      const no = h("button", "", [S.cancel]);
      ok.type = no.type = "button";
      form = h("div", "cform", [ta, h("div", "acts", [no, ok])]);
      const vr = view.getBoundingClientRect();
      Object.assign(form.style, { left: Math.min(ev.clientX - vr.left, vr.width - 270) + "px", top: Math.min(ev.clientY - vr.top, vr.height - 150) + "px" });
      view.append(form);
      ta.focus();
      no.addEventListener("click", closeForm);
      ok.addEventListener("click", async () => {
        const text = ta.value.trim();
        if (!text) return;
        ok.disabled = true;
        try {
          const c = await hooks.onComment({ screen: l.screen.id, x: Math.round(x), y: Math.round(y), text });
          if (c && c.id) (m.board.comments = m.board.comments || []).push(c);
          closeForm();
          paintPins();
          toast(S.saved);
        } catch {
          ok.disabled = false;
          toast(S.failed);
        }
      });
    }
    let hintLine = null;
    function setCommenting(on) {
      commenting = on;
      commentBtn.setAttribute("aria-pressed", String(on));
      stage.classList.toggle("commenting", on);
      closeForm();
      hintLine?.remove();
      hintLine = null;
      if (on) { hintLine = h("div", "hint-line", [S.place]); view.append(hintLine); }
      paintPins();
    }

    // -- events --
    // While commenting, clicks place a comment instead of acting.
    device.addEventListener("click", (ev) => {
      if (!commenting) return;
      ev.stopPropagation();
      ev.preventDefault();
      commentAt(ev);
    }, true);
    device.addEventListener("click", () => {
      // A click on an empty spot shows what can be clicked.
      clearTimeout(hintTimer);
      device.classList.add("hint");
      hintTimer = setTimeout(() => device.classList.remove("hint"), 900);
    });
    pick.addEventListener("change", () => go(pick.value));
    restartBtn.addEventListener("click", restart);
    commentBtn.addEventListener("click", () => setCommenting(!commenting));
    closeBtn.addEventListener("click", () => hooks.onClose && hooks.onClose());
    const onKey = (ev) => {
      if (ev.key === "Escape") {
        if (form) closeForm();
        else if (commenting) setCommenting(false);
        else if (overlays.length) closeOverlay();
        else if (hooks.onClose) hooks.onClose();
      }
    };
    shadow.addEventListener("keydown", onKey);
    const ro = typeof ResizeObserver === "function" ? new ResizeObserver(fit) : null;
    ro?.observe(view);

    function begin() {
      view.textContent = "";
      fillPicker();
      if (!info.screens.length) { view.append(h("div", "empty", [S.empty])); return; }
      view.append(device);
      restart();
    }
    begin();

    return {
      /** New board or data (e.g. the board changed while playing): keeps the state and the screen. */
      update(next) {
        m = next;
        proto = (m.board.prototypes || []).find((p) => p.id === m.proto) || proto;
        info = analyse(m.board, proto.id);
        for (const v of proto.vars || []) if (!(v.name in state)) state[v.name] = v.value ?? (v.type === "bool" ? false : v.type === "number" ? 0 : v.type === "list" ? [] : "");
        fillPicker();
        if (!base || !screenById(base.id)) { begin(); return; }
        const s = screenById(base.id);
        while (overlays.length) closeOverlay(true);
        const old = base;
        base = null;
        old.cleanup();
        old.node.remove();
        setBase(s);
      },
      restart,
      go,
      screen: () => base && base.id,
      destroy() {
        ro?.disconnect();
        clearTimeout(hintTimer);
        base?.cleanup();
        for (const o of overlays) o.cleanup();
        shadow.textContent = "";
      },
    };
  }

  /**
   * Draws one element as it looks while playing, without interactions:
   * the editor shows components with it. opts: look, vars, data, fileURL.
   */
  function preview(host, e, opts) {
    opts = opts || {};
    const shadow = host.shadowRoot || host.attachShadow({ mode: "open" });
    shadow.textContent = "";
    const style = h("style");
    style.textContent = CSS + ":host { contain: layout paint; pointer-events: none; }";
    const vars = opts.vars || [];
    const state = {};
    for (const v of vars) state[v.name] = v.value ?? (v.type === "bool" ? false : v.type === "number" ? 0 : v.type === "list" ? [] : "");
    const R = renderer({
      state, varType: (n) => vars.find((v) => v.name === n)?.type, data: () => opts.data || {}, files: () => ({}), fileURL: opts.fileURL,
      run() {}, act() {}, refresh() {}, active: () => null,
    });
    const ups = [];
    const node = R.drawEl(Object.assign({}, e, { actions: [], ui: e.ui ? Object.assign({}, e.ui, { show: undefined }) : e.ui }), num(e.x), num(e.y), ups);
    for (const u of ups) u();
    const wrap = h("div", opts.look === "sketch" ? "sketch" : "");
    wrap.style.cssText = "position:absolute;inset:0;color:var(--pf)";
    if (node) wrap.append(node);
    shadow.append(style, wrap);
  }

  globalThis.PhxProto = { mount, preview, analyse, icons: Object.keys(ICONS), version: 1 };
})();
