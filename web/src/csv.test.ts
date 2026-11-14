import { describe, expect, test } from "bun:test";
import { parseCSV, sniffDelimiter, toCSV } from "./csv";

describe("csv", () => {
  test("parses quotes, delimiters and line endings", () => {
    const text = '﻿name;ip;notes\r\nWeb;10.0.0.2;"rack ""A""; top"\r\nNAS;10.0.0.3;"two\nlines"\r\n\r\n';
    expect(sniffDelimiter(text)).toBe(";");
    expect(parseCSV(text)).toEqual([["name", "ip", "notes"], ["Web", "10.0.0.2", 'rack "A"; top'], ["NAS", "10.0.0.3", "two\nlines"]]);
    expect(parseCSV("a,b\n1,")).toEqual([["a", "b"], ["1", ""]]);
    expect(sniffDelimiter("a\tb\tc\n")).toBe("\t");
  });

  test("writes CSV that reads back and defuses formulas", () => {
    const rows = [["name", "note", "n"], ["Web", 'say "hi", ok', 3], ["=HYPERLINK(1)", "-5", "-x"]];
    const out = toCSV(rows);
    expect(parseCSV(out)).toEqual([["name", "note", "n"], ["Web", 'say "hi", ok', "3"], ["'=HYPERLINK(1)", "-5", "'-x"]]);
  });
});
