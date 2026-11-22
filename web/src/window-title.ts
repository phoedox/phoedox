/** Window title: "Project · Phoedox", with the organization's name instead of Phoedox on its own domain. */
export function windowTitle(project?: string, org?: string): string {
  const base = org || "Phoedox";
  return project ? `${project} · ${base}` : base;
}
