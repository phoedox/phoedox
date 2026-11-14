import { describe, expect, test } from "bun:test";
import { contrast, projectColors } from "./color";
import { markdownHTML, withoutTitle } from "./md";
import { parseRequest, splitVars } from "./request";
import { creationOptions, requestOptions } from "./webauthn";

describe("mentions", () => {
  test("links @slug and @project/slug, but not e-mail addresses", () => {
    const out = markdownHTML("Der @router-flur und @homelab/pve-01, Mail an a@b.de", "lindenstrasse");
    expect(out).toContain('<a href="/p/lindenstrasse/e/router-flur" title="mention">@router-flur</a>');
    expect(out).toContain('<a href="/p/homelab/e/pve-01" title="mention">@homelab/pve-01</a>');
    expect(out).toContain("a@b.de");
    expect(out).not.toContain("/e/b");
  });
  test("a leading heading equal to the title is hidden", () => {
    expect(withoutTitle("# Fahrplan und Stand\n\nText", "Fahrplan und Stand")).toBe("Text");
    expect(withoutTitle("# Anders\n\nText", "Fahrplan")).toBe("# Anders\n\nText");
    expect(withoutTitle("Text\n# Fahrplan", "Fahrplan")).toBe("Text\n# Fahrplan");
  });
  test("code stays literal", () => {
    const out = markdownHTML("Am **@router**: `ssh @nas`\n\n```sh\necho @server\n```\n\n| a | b |\n|---|---|\n| @switch | `@x` |", "h");
    expect(out).toContain('<strong><a href="/p/h/e/router" title="mention">@router</a></strong>');
    expect(out).toContain("<code>ssh @nas</code>");
    expect(out).toContain("echo @server");
    expect(out).toContain('<td><a href="/p/h/e/switch" title="mention">@switch</a></td>');
    expect(out).toContain("<code>@x</code>");
    expect(out.match(/title="mention"/g)?.length).toBe(2);
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

describe("page building blocks (ADR-0031)", () => {
  test("render to their elements", () => {
    const out = markdownHTML([
      "> [!warning]", "> Achtung **fett**", "",
      "> [!callout icon=User color=orange]", "> Wichtig ist die Änderung", "",
      "Text [rot]{color=red} und [markiert]{bg=yellow} und [beides]{color=blue bg=pink}, ++unter++.",
      "Formel $a^2+b^2$ aber $5 und $10 bleiben.",
      'Am <time datetime="2026-12-24" data-kind="countdown">24.12.2026</time>.',
      "", "```math", "\\int_0^1 x\\,dx", "```", "", "```embed", "https://youtu.be/dQw4w9WgXcQ", "```", "", "```board", "umbau", "```",
      "", "```mermaid", "flowchart LR", "  A --> B", "```",
    ].join("\n"), "haus");
    expect(out).toContain('class="callout cb-orange" data-kind="warning"');
    expect(out).toContain("<strong>fett</strong>");
    expect(out).toContain('class="callout cb-orange" data-kind="callout"');
    expect(out).toContain('<span class="tc-red">rot</span>');
    expect(out).toContain('<span class="tb-yellow">markiert</span>');
    expect(out).toContain('<span class="tc-blue tb-pink">beides</span>');
    expect(out).toContain("<u>unter</u>");
    expect(out).toContain('<phx-math data-tex="a^2+b^2">');
    expect(out).toContain("$5 und $10 bleiben");
    expect(out).toContain('<phx-time datetime="2026-12-24" data-kind="countdown">24.12.2026</phx-time>');
    expect(out).toContain('data-display="1"');
    expect(out).toContain('<phx-embed data-url="https://youtu.be/dQw4w9WgXcQ" data-kind="embed">');
    expect(out).toContain('<phx-board-embed data-board="umbau" data-project="haus">');
    expect(out).toContain("<phx-mermaid><pre>flowchart LR\n  A --&gt; B</pre></phx-mermaid>");
  });
});
