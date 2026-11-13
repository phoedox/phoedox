import { describe, expect, test } from "bun:test";
import { contrast, projectColors } from "./color";
import { linkMentions } from "./md";
import { parseRequest, splitVars } from "./request";
import { creationOptions, requestOptions } from "./webauthn";

describe("mentions", () => {
  test("links @slug and @project/slug, but not e-mail addresses", () => {
    const out = linkMentions("Der @router-flur und @homelab/pve-01, Mail an a@b.de", "lindenstrasse");
    expect(out).toContain("[@router-flur](/p/lindenstrasse/e/router-flur");
    expect(out).toContain("[@homelab/pve-01](/p/homelab/e/pve-01");
    expect(out).toContain("a@b.de");
    expect(out).not.toContain("/e/b");
  });
});

describe("project colours", () => {
  test("light and dark variants reach 4.5:1", () => {
    for (const c of ["#F26A1B", "#FFEB3B", "#0B1A4A", "#7C3AED"]) {
      const { light, dark } = projectColors(c);
      const hx = (h: string) => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16));
      expect(contrast(hx(light), [255, 255, 255])).toBeGreaterThanOrEqual(4.5);
      expect(contrast(hx(dark), [28, 24, 21])).toBeGreaterThanOrEqual(4.5);
    }
  });
});


describe("webauthn", () => {
  test("decodes base64url fields", () => {
    const c = creationOptions({ publicKey: { challenge: "AQID", user: { id: "_-8", name: "a" }, excludeCredentials: [{ id: "AQ", type: "public-key" }] } });
    expect(new Uint8Array(c.publicKey!.challenge as ArrayBuffer)).toEqual(new Uint8Array([1, 2, 3]));
    expect(new Uint8Array(c.publicKey!.user.id as ArrayBuffer)).toEqual(new Uint8Array([255, 239]));
    const r = requestOptions({ publicKey: { challenge: "AQID", allowCredentials: [{ id: "AQ", type: "public-key" }] } });
    expect(new Uint8Array(r.publicKey!.allowCredentials![0].id as ArrayBuffer)).toEqual(new Uint8Array([1]));
  });
});


describe("request blocks", () => {
  test("parse like the server", () => {
    const r = parseRequest("# Status\nPOST {{base}}/items\nContent-Type: application/json\n\n{\"a\": 1}\n\nexpect status == 201\nexpect $.id exists")!;
    expect(r.method).toBe("POST");
    expect(r.headers).toEqual([["Content-Type", "application/json"]]);
    expect(r.body).toBe('{"a": 1}');
    expect(r.expects).toEqual(["status == 201", "$.id exists"]);
    expect(parseRequest("FETCH x")).toBeNull();
    expect(splitVars("{{base}}/x?k={{ key }}")).toEqual([{ text: "base", variable: true }, { text: "/x?k=" }, { text: "key", variable: true }]);
  });
});
