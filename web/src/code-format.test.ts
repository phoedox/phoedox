import { describe, expect, test } from "bun:test";
import { formatCode, langOf } from "./code-format";

describe("code formatting", () => {
  test("languages by file name", () => {
    expect(langOf("index.html")).toBe("html");
    expect(langOf("app.tsx")).toBe("tsx");
    expect(langOf("slides.md")).toBe("md");
    expect(langOf("data.json")).toBe("json");
    expect(langOf("style.css")).toBe("css");
    expect(langOf("notes.txt")).toBe("text");
    expect(langOf("json")).toBe("json");
  });

  test("formats html, css, js, ts, json and markdown", async () => {
    const page = await formatCode("<!doctype html><html><head><title>T</title><style>a{color:red}</style></head><body><main><p>Hi</p></main><script>const a={b:1}</script></body></html>", "html");
    expect(page).toContain("\n  <head>\n");
    expect(page).toContain("a {\n        color: red;\n      }");
    expect(page).toContain("const a = { b: 1 };");
    expect(await formatCode("a{color:red}", "css")).toBe("a {\n  color: red;\n}\n");
    expect(await formatCode("const a={b:1}", "js")).toBe("const a = { b: 1 };\n");
    expect(await formatCode("const a:number=1", "ts")).toBe("const a: number = 1;\n");
    expect(await formatCode("export default ()=> <div className='x'>Hi</div>", "tsx")).toContain('<div className="x">Hi</div>');
    expect(await formatCode('{"a":1,"b":[1,2]}', "json")).toBe('{ "a": 1, "b": [1, 2] }\n');
    expect(await formatCode("#  Title\n*  one\n*  two", "md")).toBe("# Title\n\n- one\n- two\n");
  });

  test("syntax errors throw, plain text stays", async () => {
    await expect(formatCode("const = ;", "js")).rejects.toThrow();
    expect(await formatCode("as  is", "text")).toBe("as  is");
  });
});
