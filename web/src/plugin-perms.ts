import { t } from "./i18n";

/** Human-readable description of a manifest permission. */
export function permLabel(perm: string): string {
  const [area, action, rest] = perm.split(":", 3);
  if (area === "net" && action === "fetch") return t("perm.net_fetch", { host: rest ?? "" });
  if (area === "secrets") return t("perm.secret", { name: action ?? "" });
  const what = t(`perm.area.${area}` as never);
  if (rest) return t(`perm.${action}_only` as never, { area: what, kinds: rest.split(",").join(", ") });
  return t(`perm.${action}` as never, { area: what });
}
