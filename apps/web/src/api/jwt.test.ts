import { describe, expect, it } from "vitest";
import { decodeIdToken } from "./jwt";

/**
 * Builds a token the way a real issuer does: UTF-8 encode the JSON, base64 the
 * bytes, then make it base64url by swapping +/ and dropping the '=' padding.
 * Going straight through btoa() would throw on anything outside Latin-1.
 */
function makeToken(payload: unknown): string {
  const bytes = new TextEncoder().encode(JSON.stringify(payload));
  const binary = Array.from(bytes, (b) => String.fromCharCode(b)).join("");
  const base64url = btoa(binary)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
  return `header.${base64url}.signature`;
}

describe("decodeIdToken", () => {
  it("reads the display claims out of the payload", () => {
    const token = makeToken({
      name: "Emerson",
      email: "e@example.com",
      phone_number: "+5511999999999",
    });

    expect(decodeIdToken(token)).toEqual({
      name: "Emerson",
      email: "e@example.com",
      phone_number: "+5511999999999",
    });
  });

  it("keeps unknown claims", () => {
    const claims = decodeIdToken(makeToken({ sub: "abc", exp: 1893456000 }));

    expect(claims?.sub).toBe("abc");
    expect(claims?.exp).toBe(1893456000);
  });

  // base64url drops the '=' padding, so the decoder has to put it back before
  // atob will accept the segment. Payload lengths hitting each remainder
  // exercise that.
  it("restores stripped base64url padding at every length", () => {
    for (const name of ["a", "ab", "abc", "abcd", "abcde"]) {
      expect(decodeIdToken(makeToken({ name }))?.name).toBe(name);
    }
  });

  it("decodes the base64url alphabet", () => {
    // Padded with a '?' so the payload is long enough to produce '-' and '_'
    // substitutions rather than plain base64.
    const name = "çãõ+/?ü";
    expect(decodeIdToken(makeToken({ name }))?.name).toBe(name);
  });

  it("decodes multi-byte UTF-8 claims", () => {
    expect(decodeIdToken(makeToken({ name: "Farmácia Ção 🏥" }))?.name).toBe(
      "Farmácia Ção 🏥",
    );
  });

  it("returns null for a token with no payload segment", () => {
    expect(decodeIdToken("not-a-token")).toBeNull();
  });

  it("returns null when the payload is not valid base64", () => {
    expect(decodeIdToken("header.!!!not-base64!!!.signature")).toBeNull();
  });

  it("returns null when the payload is not JSON", () => {
    const notJson = btoa("plain text").replace(/=+$/, "");
    expect(decodeIdToken(`header.${notJson}.signature`)).toBeNull();
  });

  it("returns null for an empty string", () => {
    expect(decodeIdToken("")).toBeNull();
  });
});
