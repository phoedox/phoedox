import { describe, expect, test } from "bun:test";
import { openSecret, sealSecret } from "./secret-share";

describe("one-time secrets", () => {
  test("round trip without and with passphrase", async () => {
    const a = await sealSecret("WLAN: 🔑 geheim");
    expect(a.ciphertext).not.toContain("geheim");
    expect(await openSecret(a.ciphertext, a.fragment)).toBe("WLAN: 🔑 geheim");
    const b = await sealSecret("root:toor", "pferd-batterie");
    expect(await openSecret(b.ciphertext, b.fragment, "pferd-batterie")).toBe("root:toor");
    await expect(openSecret(b.ciphertext, b.fragment, "falsch")).rejects.toThrow();
    // The fragment alone is not enough when a passphrase was used, and vice versa.
    await expect(openSecret(b.ciphertext, a.fragment, "pferd-batterie")).rejects.toThrow();
  });
});
