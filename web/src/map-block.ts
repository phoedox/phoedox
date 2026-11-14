// Map blocks (```map): markers, tracks and GPX files as plain text, so the
// block stays readable in Markdown, Git and publications.
//
//   marker 52.5200, 13.4050 Berlin office @berlin-office
//   marker @munich-site                (position from the entry's lat/lng)
//   track Fibre route: 52.52,13.40 52.40,13.06 52.10,12.90
//   gpx 01J…FILEID Hike to the summit  (a GPX file of the project)
//   view 52.5, 13.4 zoom 9

export interface MapMarker { lat?: number; lng?: number; label: string; entity?: string }
export interface MapTrack { label: string; points: [number, number][] }
export interface MapGpx { file: string; label: string }
export interface MapSpec { markers: MapMarker[]; tracks: MapTrack[]; gpx: MapGpx[]; view?: { lat: number; lng: number; zoom: number }; errors: number[] }

const NUM = String.raw`(-?\d+(?:\.\d+)?)`;
const PAIR = new RegExp(String.raw`^${NUM}\s*,\s*${NUM}`);
const valid = (lat: number, lng: number) => Math.abs(lat) <= 90 && Math.abs(lng) <= 180;

export function parseMap(src: string): MapSpec {
  const out: MapSpec = { markers: [], tracks: [], gpx: [], errors: [] };
  src.split("\n").forEach((raw, i) => {
    const line = raw.trim();
    if (!line || line.startsWith("#")) return;
    const [cmd, ...rest] = line.split(/\s+/);
    const tail = rest.join(" ");
    switch (cmd.toLowerCase()) {
      case "marker": {
        let text = tail;
        let entity: string | undefined;
        const m = text.match(/(?:^|\s)@([a-z0-9]+(?:-[a-z0-9]+)*)\s*$/);
        if (m) {
          entity = m[1];
          text = text.slice(0, m.index).trim();
        }
        const pos = text.match(PAIR);
        if (pos) {
          const [lat, lng] = [Number(pos[1]), Number(pos[2])];
          if (!valid(lat, lng)) return void out.errors.push(i + 1);
          out.markers.push({ lat, lng, label: text.slice(pos[0].length).trim(), entity });
        } else if (entity) {
          out.markers.push({ label: text, entity });
        } else out.errors.push(i + 1);
        return;
      }
      case "track": {
        const colon = tail.indexOf(":");
        const label = colon >= 0 && !PAIR.test(tail) ? tail.slice(0, colon).trim() : "";
        const coords = colon >= 0 && label ? tail.slice(colon + 1) : tail;
        const points = [...coords.matchAll(new RegExp(String.raw`${NUM}\s*,\s*${NUM}`, "g"))].map((x) => [Number(x[1]), Number(x[2])] as [number, number]);
        if (points.length < 2 || !points.every(([a, b]) => valid(a, b))) return void out.errors.push(i + 1);
        out.tracks.push({ label, points });
        return;
      }
      case "gpx": {
        const [file, ...label] = rest;
        if (!/^[A-Za-z0-9_-]{6,40}$/.test(file ?? "")) return void out.errors.push(i + 1);
        out.gpx.push({ file, label: label.join(" ") });
        return;
      }
      case "view": {
        const m = tail.match(new RegExp(String.raw`^${NUM}\s*,\s*${NUM}(?:\s+zoom\s+(\d{1,2}))?$`));
        if (!m || !valid(Number(m[1]), Number(m[2]))) return void out.errors.push(i + 1);
        out.view = { lat: Number(m[1]), lng: Number(m[2]), zoom: Math.min(19, Number(m[3] ?? 13)) };
        return;
      }
    }
    out.errors.push(i + 1);
  });
  return out;
}

/** Track points of a GPX document (trkpt, rtept, then wpt). */
export function gpxPoints(xml: string): [number, number][] {
  const out: [number, number][] = [];
  for (const tag of ["trkpt", "rtept", "wpt"]) {
    for (const m of xml.matchAll(new RegExp(`<${tag}\\b[^>]*>`, "g"))) {
      const lat = m[0].match(/\blat="(-?[\d.]+)"/), lon = m[0].match(/\blon="(-?[\d.]+)"/);
      if (lat && lon && valid(Number(lat[1]), Number(lon[1]))) out.push([Number(lat[1]), Number(lon[1])]);
    }
    if (out.length) break;
  }
  return out;
}
