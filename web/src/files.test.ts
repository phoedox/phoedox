import { expect, test } from "bun:test";
import { fileIcon, fileURL, formatSize, isImage } from "./files";
import { safeImageSrc } from "./editor/image";

test("file helpers", () => {
  expect(formatSize(512)).toBe("512 B");
  expect(formatSize(1536)).toBe("1,5 KB");
  expect(formatSize(5 * 1024 * 1024, "en")).toBe("5 MB");
  expect(fileURL({ id: "01ABC", name: "Plan Keller.png" })).toBe("/api/v1/files/01ABC/Plan%20Keller.png");
  expect(fileURL({ id: "01ABC", name: "a" }, true)).toEndWith("?download=1");
  expect(isImage("image/svg+xml")).toBe(true);
  expect(isImage("text/html")).toBe(false);
  expect(fileIcon("application/pdf")).toBe("FileText");
});

test("only own files or https images are rendered in the editor", () => {
  expect(safeImageSrc("/api/v1/files/01ABC/Plan.png")).toBe(true);
  expect(safeImageSrc("https://example.org/a.png")).toBe(true);
  expect(safeImageSrc("javascript:alert(1)")).toBe(false);
  expect(safeImageSrc("http://intranet/a.png")).toBe(false);
  expect(safeImageSrc('/api/v1/files/01ABC/x" onerror="alert(1)')).toBe(false);
});
