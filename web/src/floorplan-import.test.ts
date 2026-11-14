import { describe, expect, test } from "bun:test";
import { normalizePlan } from "./floorplan";
import { ImportError, addImport, parseDXF, parseIFC, parseImport, parseRoomPlan } from "./floorplan-import";

const dxf = (pairs: [number, string | number][]) => pairs.map(([c, v]) => `  ${c}\n${v}`).join("\n") + "\n";

describe("DXF", () => {
  const file = dxf([
    [0, "SECTION"], [2, "HEADER"], [9, "$INSUNITS"], [70, 6], [0, "ENDSEC"],
    [0, "SECTION"], [2, "ENTITIES"],
    [0, "LINE"], [8, "Walls"], [10, 0], [20, 0], [30, 0], [11, 4], [21, 0], [31, 0],
    [0, "LWPOLYLINE"], [8, "Rooms"], [90, 4], [70, 1], [10, 0], [20, 0], [10, 4], [20, 0], [10, 4], [20, 3], [10, 0], [20, 3],
    [0, "TEXT"], [8, "Labels"], [10, 1], [20, 1], [1, "Küche"],
    [0, "LWPOLYLINE"], [8, "Walls"], [90, 3], [70, 0], [10, 0], [20, 3], [10, 0], [20, 6], [10, 2], [20, 6],
    [0, "POLYLINE"], [8, "Walls"], [10, 0], [20, 0], [70, 0],
    [0, "VERTEX"], [8, "Walls"], [10, 5], [20, 0],
    [0, "VERTEX"], [8, "Walls"], [10, 5], [20, 2],
    [0, "SEQEND"],
    [0, "CIRCLE"], [8, "Walls"], [10, 1], [20, 1], [40, 1],
    [0, "ENDSEC"], [0, "EOF"],
  ]);

  test("lines, polylines, rooms with names, units", () => {
    const r = parseDXF(file);
    const l = r.levels[0];
    expect(r.skipped).toBe(1);
    expect(l.rooms).toHaveLength(1);
    expect(l.rooms[0].name).toBe("Küche");
    expect(l.rooms[0].points[2]).toEqual([4000, -3000]); // metres → mm, y flipped
    // LINE + 2 segments of the open polyline + 1 of the old-style POLYLINE (no dummy point)
    expect(l.walls).toHaveLength(4);
    expect(l.walls[0]).toEqual({ a: [0, -0], b: [4000, -0] });
    expect(l.walls[3]).toEqual({ a: [5000, -0], b: [5000, -2000] });
  });

  test("unitless drawings are guessed from their size", () => {
    const mm = dxf([[0, "SECTION"], [2, "ENTITIES"], [0, "LINE"], [10, 0], [20, 0], [11, 4000], [21, 0], [0, "ENDSEC"]]);
    expect(parseDXF(mm).levels[0].walls[0].b).toEqual([4000, -0]);
  });

  test("rejects other files", () => {
    expect(() => parseDXF("hello\nworld")).toThrow(ImportError);
    expect(() => parseImport("plan.dwg", "")).toThrow("plan.import_dwg");
    expect(() => parseImport("plan.svg", "")).toThrow("plan.import_format");
  });
});

describe("RoomPlan", () => {
  // A wall 4 m long along x centred at (2, 0, 0) and one rotated 90° about y.
  const rot = [0, 0, -1, 0, 0, 1, 0, 0, 1, 0, 0, 0];
  const room = {
    walls: [
      { dimensions: [4, 2.5, 0], transform: [[1, 0, 0, 0], [0, 1, 0, 0], [0, 0, 1, 0], [2, 1.25, 0, 1]] },
      { dimensions: [3, 2.5, 0], transform: [...rot, 4, 1.25, 1.5, 1] },
    ],
    doors: [{ dimensions: [0.9, 2, 0], transform: [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 1, 1, 0, 1] }],
    windows: [{ dimensions: { x: 1.2, y: 1, z: 0 }, transform: [...rot, 4, 1, 2, 1] }],
    objects: [{}, {}],
  };

  test("walls, doors and windows in mm", () => {
    const r = parseRoomPlan(JSON.stringify(room));
    const l = r.levels[0];
    expect(l.walls[0]).toEqual({ a: [0, 0], b: [4000, 0], height: 2500 });
    expect(l.walls[1].a).toEqual([4000, 3000]);
    expect(l.walls[1].b).toEqual([4000, 0]);
    expect(l.openings).toEqual([{ kind: "door", center: [1000, 0], width: 900 }, { kind: "window", center: [4000, 2000], width: 1200 }]);
    expect(r.skipped).toBe(2);
  });

  test("openings land on their wall", () => {
    const res = addImport(normalizePlan(undefined), parseRoomPlan(JSON.stringify(room)).levels[0]);
    expect(res.walls).toBe(2);
    expect(res.openings).toBe(2);
    const door = res.plan.openings.find((o) => o.kind === "door")!;
    const wall = res.plan.walls.find((w) => w.id === door.wall)!;
    expect(wall.a).toEqual([1000, 1000]); // moved next to the origin
    expect(door.offset).toBe(1000);
    expect(res.plan.width).toBeGreaterThanOrEqual(6000);
  });

  test("rejects other JSON", () => {
    expect(() => parseRoomPlan("{}")).toThrow("plan.import_invalid");
  });
});

describe("IFC", () => {
  const ifc = `ISO-10303-21;
HEADER;
FILE_DESCRIPTION((''),'2;1');
ENDSEC;
DATA;
#1=IFCSIUNIT(*,.LENGTHUNIT.,.MILLI.,.METRE.);
#2=IFCCARTESIANPOINT((0.,0.,0.));
#3=IFCAXIS2PLACEMENT3D(#2,$,$);
#4=IFCLOCALPLACEMENT($,#3);
#5=IFCBUILDINGSTOREY('s1',$,'Erdgeschoss',$,$,#4,$,$,.ELEMENT.,0.);
#10=IFCCARTESIANPOINT((1000.,2000.,0.));
#11=IFCAXIS2PLACEMENT3D(#10,$,$);
#12=IFCLOCALPLACEMENT(#4,#11);
#13=IFCCARTESIANPOINT((0.,0.));
#14=IFCCARTESIANPOINT((5000.,0.));
#15=IFCPOLYLINE((#13,#14));
#16=IFCSHAPEREPRESENTATION($,'Axis','Curve2D',(#15));
#17=IFCCARTESIANPOINT((2500.,0.));
#18=IFCAXIS2PLACEMENT2D(#17,$);
#19=IFCRECTANGLEPROFILEDEF(.AREA.,$,#18,5000.,200.);
#20=IFCEXTRUDEDAREASOLID(#19,#3,$,2500.);
#21=IFCSHAPEREPRESENTATION($,'Body','SweptSolid',(#20));
#22=IFCPRODUCTDEFINITIONSHAPE($,$,(#16,#21));
#23=IFCWALLSTANDARDCASE('w1',$,'Wall ''A''',$,$,#12,#22,$);
#30=IFCCARTESIANPOINT((1500.,0.,0.));
#31=IFCAXIS2PLACEMENT3D(#30,$,$);
#32=IFCLOCALPLACEMENT(#12,#31);
#33=IFCDOOR('d1',$,'Door',$,$,#32,$,$,2100.,900.);
#40=IFCCARTESIANPOINT((0.,0.));
#41=IFCCARTESIANPOINT((5000.,0.));
#42=IFCCARTESIANPOINT((5000.,-4000.));
#43=IFCCARTESIANPOINT((0.,-4000.));
#44=IFCPOLYLINE((#40,#41,#42,#43,#40));
#45=IFCSHAPEREPRESENTATION($,'FootPrint','Curve2D',(#44));
#46=IFCPRODUCTDEFINITIONSHAPE($,$,(#45));
#47=IFCSPACE('r1',$,'1.01',$,$,#12,#46,'Wohnzimmer \\X2\\00FC\\X0\\',.ELEMENT.,$,$);
#50=IFCRELCONTAINEDINSPATIALSTRUCTURE('x',$,$,$,(#23,#33),#5);
#51=IFCRELAGGREGATES('y',$,$,$,#5,(#47));
ENDSEC;
END-ISO-10303-21;
`;

  test("walls, doors and spaces per storey", () => {
    const r = parseIFC(ifc);
    expect(r.levels).toHaveLength(1);
    const l = r.levels[0];
    expect(l.name).toBe("Erdgeschoss");
    expect(l.walls).toEqual([{ a: [1000, -2000], b: [6000, -2000], thickness: 200, height: 2500 }]);
    expect(l.openings).toEqual([{ kind: "door", center: [2950, -2000], width: 900 }]);
    expect(l.rooms).toHaveLength(1);
    expect(l.rooms[0].name).toBe("Wohnzimmer ü");
    expect(l.rooms[0].points).toEqual([[1000, -2000], [6000, -2000], [6000, 2000], [1000, 2000]]);
  });

  test("metre models and rectangle walls without axis", () => {
    const m = ifc.replace(".MILLI.", "$").replace("#22=IFCPRODUCTDEFINITIONSHAPE($,$,(#16,#21));", "#22=IFCPRODUCTDEFINITIONSHAPE($,$,(#21));");
    const w = parseIFC(m).levels[0].walls[0];
    // Values are read as metres now: the wall runs from 1000 m to 6000 m.
    expect(w.a).toEqual([1_000_000, -2_000_000]);
    expect(w.b).toEqual([6_000_000, -2_000_000]);
    expect(w.thickness).toBe(200_000);
  });

  test("rejects other files", () => {
    expect(() => parseIFC("hello")).toThrow("plan.import_invalid");
  });
});
