import { describe, expect, test } from "bun:test";
import { wifiPayload } from "./wifi";

describe("guest Wi-Fi card", () => {
  test("escapes special characters", () => {
    expect(wifiPayload("My;Net", 'pa:ss"1', "WPA", false)).toBe(String.raw`WIFI:T:WPA;S:My\;Net;P:pa\:ss\"1;;`);
    expect(wifiPayload("Guest", "", "nopass", true)).toBe("WIFI:T:nopass;S:Guest;H:true;;");
  });
});
