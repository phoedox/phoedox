import { expect, test } from "bun:test";
import { coverImage, imageMarkdown, insertAt } from "./card-images";

test("cover image", () => {
  expect(coverImage("text ![a](https://evil.example/x.png) ![b](/api/v1/files/01X/b.png)")).toBe("/api/v1/files/01X/b.png");
  expect(coverImage("no images")).toBeUndefined();
  expect(imageMarkdown("shot [1].png", "/api/v1/files/1/x.png")).toBe("![shot 1.png](/api/v1/files/1/x.png)");
});

test("insert on its own line", () => {
  expect(insertAt("abc", 3, 3, "IMG")).toEqual({ value: "abc\nIMG", cursor: 7 });
  expect(insertAt("ab\ncd", 3, 3, "IMG")).toEqual({ value: "ab\nIMG\ncd", cursor: 6 });
  expect(insertAt("", 0, 0, "IMG")).toEqual({ value: "IMG", cursor: 3 });
});
