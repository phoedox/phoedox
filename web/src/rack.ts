// Rack layout (concept "Serverschränke"): devices with mounted_in carry u
// (lowest height unit, 1 = bottom) and height (units) on the relation.

export interface Mounted { slug: string; name: string; kind: string; u?: number; height: number }
export interface Slot { top: number; bottom: number; device?: Mounted; conflict?: boolean }

/** Units from top to bottom with the devices in them; overlaps are marked. */
export function rackSlots(units: number, devices: Mounted[]): { slots: Slot[]; unplaced: Mounted[]; conflicts: string[] } {
  const placed = devices.filter((d) => d.u && d.u >= 1 && d.u + d.height - 1 <= units).sort((a, b) => b.u! - a.u!);
  const unplaced = devices.filter((d) => !placed.includes(d));
  const taken = new Map<number, Mounted>();
  const conflicts = new Set<string>();
  for (const d of placed) {
    for (let u = d.u!; u < d.u! + d.height; u++) {
      const other = taken.get(u);
      if (other) {
        conflicts.add(d.slug);
        conflicts.add(other.slug);
      } else taken.set(u, d);
    }
  }
  const slots: Slot[] = [];
  for (let u = units; u >= 1; ) {
    const d = taken.get(u);
    if (d) {
      const bottom = Math.max(d.u!, u - d.height + 1);
      slots.push({ top: u, bottom, device: d, conflict: conflicts.has(d.slug) });
      u = bottom - 1;
    } else {
      slots.push({ top: u, bottom: u });
      u--;
    }
  }
  return { slots, unplaced, conflicts: [...conflicts] };
}
