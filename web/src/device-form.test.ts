import { expect, test } from "bun:test";
import { quickEnum, toolFields } from "./device-form";

test("tool fields from a schema", () => {
  const call = { type: "object", required: ["entity_id", "action"], properties: {
    entity_id: { type: "string" }, action: { type: "string", enum: ["on", "off", "toggle"] } } };
  const f = toolFields(call, "entity_id");
  expect(f).toEqual([{ name: "action", label: "action", type: "enum", options: ["on", "off", "toggle"], required: true }]);
  expect(quickEnum(f)?.name).toBe("action");

  const restart = { type: "object", required: ["entity_id"], properties: {
    entity_id: { type: "string" }, seconds: { type: "integer", minimum: 3, maximum: 30 } } };
  const r = toolFields(restart, "entity_id");
  expect(r).toEqual([{ name: "seconds", label: "seconds", type: "number", min: 3, max: 30, required: false }]);
  expect(quickEnum(r)).toBeUndefined();
  expect(toolFields(null, "x")).toEqual([]);
  expect(toolFields({ properties: { on: { type: "boolean" } } }, "ups")[0].type).toBe("bool");
});
