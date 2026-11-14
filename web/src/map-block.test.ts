import { describe, expect, test } from "bun:test";
import { gpxPoints, parseMap } from "./map-block";

describe("map block", () => {
  test("parses markers, tracks, gpx and view", () => {
    const m = parseMap(`# sites
marker 52.5200, 13.4050 Berlin office @berlin-office
marker @munich-site
marker 48.1, 11.5
track Fibre route: 52.52,13.40 52.40,13.06 52.10,12.90
track 1,1 2,2
gpx 01JABCDEF123 Summit hike
view 52.5, 13.4 zoom 9
marker 95, 10 bad
nonsense`);
    expect(m.markers).toEqual([
      { lat: 52.52, lng: 13.405, label: "Berlin office", entity: "berlin-office" },
      { label: "", entity: "munich-site" },
      { lat: 48.1, lng: 11.5, label: "", entity: undefined },
    ]);
    expect(m.tracks).toEqual([{ label: "Fibre route", points: [[52.52, 13.4], [52.4, 13.06], [52.1, 12.9]] }, { label: "", points: [[1, 1], [2, 2]] }]);
    expect(m.gpx).toEqual([{ file: "01JABCDEF123", label: "Summit hike" }]);
    expect(m.view).toEqual({ lat: 52.5, lng: 13.4, zoom: 9 });
    expect(m.errors).toEqual([9, 10]);
  });

  test("reads GPX points", () => {
    const xml = `<gpx><trk><trkseg><trkpt lat="47.1" lon="11.2"><ele>900</ele></trkpt><trkpt lon="11.3" lat="47.2"/></trkseg></trk><wpt lat="1" lon="1"/></gpx>`;
    expect(gpxPoints(xml)).toEqual([[47.1, 11.2], [47.2, 11.3]]);
    expect(gpxPoints(`<gpx><wpt lat="1" lon="2"/></gpx>`)).toEqual([[1, 2]]);
  });
});
