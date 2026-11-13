/// <reference types="vite/client" />
// Islands for published sites (ADR-0006): formulas, embeds, dates and the
// icons of callouts. Pages are fully readable without this script.
import "../styles/tokens.css";
import "./pub.css";
import "../components/blocks";
import { iconSVG } from "../icon";

for (const el of document.querySelectorAll<HTMLElement>(".callout .ci[data-icon]")) el.innerHTML = iconSVG(el.dataset.icon ?? "Info", 18);

// <time datetime data-kind> shows in the reader's language; countdowns tick.
for (const t of document.querySelectorAll<HTMLTimeElement>("time[datetime]")) {
  const el = document.createElement("phx-time");
  el.setAttribute("datetime", t.dateTime);
  el.dataset.kind = t.dataset.kind ?? "date";
  el.textContent = t.textContent;
  t.replaceWith(el);
}

// Mobile menu.
document.querySelector(".menu-toggle")?.addEventListener("click", () => document.body.classList.toggle("nav-open"));
