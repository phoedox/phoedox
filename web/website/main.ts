// The website's icons come from Lucide (bundled, no CDN) and legal pages
// show only when the operator provided them.
import {
  ArrowRight, BatteryCharging, Braces, Check, ChevronsUpDown, Copy, EthernetPort, FileText, Globe, HardDrive, House, Laptop, Lock, LogIn,
  Network, PlugZap, Router, ScanLine, Server, Tag, Wifi,
} from "lucide";

const icons: Record<string, unknown> = {
  ArrowRight, BatteryCharging, Braces, Check, ChevronsUpDown, Copy, EthernetPort, FileText, Globe, HardDrive, House, Laptop, Lock, LogIn,
  Network, PlugZap, Router, ScanLine, Server, Tag, Wifi,
};
// The page script expects the shape of Lucide's UMD build: [tag, attrs, children].
(window as unknown as { lucide: unknown }).lucide = { icons: Object.fromEntries(Object.entries(icons).map(([k, v]) => [k, ["svg", {}, v]])) };

for (const a of document.querySelectorAll<HTMLAnchorElement>("a[data-legal]")) {
  fetch(a.href, { method: "HEAD" }).then((r) => { if (!r.ok) a.remove(); }, () => a.remove());
}
