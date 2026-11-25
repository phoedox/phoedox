// Zoom steps of <phx-zoom-pane>.

export const ZOOM_MIN = 0.25, ZOOM_MAX = 6;

/** The next step of the toolbar buttons (25 % … 600 %). */
export function zoomStep(z: number, dir: 1 | -1): number {
  const steps = [0.25, 0.33, 0.5, 0.67, 0.8, 1, 1.25, 1.5, 2, 2.5, 3, 4, 5, 6];
  if (dir > 0) return steps.find((s) => s > z + 0.001) ?? ZOOM_MAX;
  return [...steps].reverse().find((s) => s < z - 0.001) ?? ZOOM_MIN;
}
