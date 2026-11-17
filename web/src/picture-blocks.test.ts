import { expect, test } from "bun:test";
import { blockDef } from "./blocks";
import { parseAnnotated } from "./blocks/annotate";
import { parseBeforeAfter } from "./blocks/beforeafter";

test("before/after and annotated images", () => {
  const ba = parseBeforeAfter("title: Rack\nbefore: /api/v1/files/01M7ND6Y5KRMY1R09EK7Q7NMXS Vorher\nafter: 01M7ND6Y5KRMY1R09EK7Q7NMXT Nachher\nstart: 30");
  expect(ba.model).toMatchObject({ before: "/api/v1/files/01M7ND6Y5KRMY1R09EK7Q7NMXS", after: "/api/v1/files/01M7ND6Y5KRMY1R09EK7Q7NMXT", beforeLabel: "Vorher", start: 30 });
  expect(parseBeforeAfter("![a](/api/v1/files/AB12)\n![b](https://example.org/b.jpg)").model).toMatchObject({ beforeLabel: "a", after: "https://example.org/b.jpg" });
  expect(parseBeforeAfter("before: javascript:alert(1)").problems[0].key).toBe("image.err_src");
  expect(parseBeforeAfter("title: x\nbefore:\nafter:").model?.before).toBeUndefined();
  const an = parseAnnotated("image: /api/v1/files/AB12 Kasten\n- 22, 30: F1 Küche und @herd\n- 74,5% ; 64 blue: FI");
  expect(an.problems).toEqual([]);
  expect(an.model!.markers[0]).toMatchObject({ x: 22, y: 30, color: "red", entities: ["herd"] });
  expect(an.model!.markers[1]).toMatchObject({ x: 74.5, y: 64, color: "blue" });
  expect(parseAnnotated("image: /api/v1/files/AB12\n- 120, 3: x").problems[0].key).toBe("annotate.err_pos");
  expect(parseAnnotated("image: /api/v1/files/AB12\n- 1, 3 teal: x").problems[0].key).toBe("annotate.err_color");
  for (const k of ["beforeafter", "annotate"]) {
    const d = blockDef(k)!;
    const { model } = d.parse(d.example);
    expect(model).toBeTruthy();
    expect(d.svg(model, { width: 500, locale: "de" })).toStartWith("<svg");
  }
});
