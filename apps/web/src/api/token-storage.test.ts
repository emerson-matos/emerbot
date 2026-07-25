import { beforeEach, describe, expect, it } from "vitest";
import { tokenStorage } from "./token-storage";

beforeEach(() => localStorage.clear());

describe("tokenStorage.getTokens", () => {
  it("returns null when nothing is stored", () => {
    expect(tokenStorage.getTokens()).toBeNull();
  });

  // The access token is what makes a session; without it the other two are
  // meaningless leftovers.
  it("returns null when only the refresh and id tokens survive", () => {
    localStorage.setItem("refresh_token", "r");
    localStorage.setItem("id_token", "i");

    expect(tokenStorage.getTokens()).toBeNull();
  });

  it("reads back a full set", () => {
    tokenStorage.setTokens({
      accessToken: "a",
      refreshToken: "r",
      idToken: "i",
    });

    expect(tokenStorage.getTokens()).toEqual({
      accessToken: "a",
      refreshToken: "r",
      idToken: "i",
    });
  });

  it("reports the optional tokens as undefined when absent", () => {
    tokenStorage.setTokens({ accessToken: "a" });

    expect(tokenStorage.getTokens()).toEqual({
      accessToken: "a",
      refreshToken: undefined,
      idToken: undefined,
    });
  });
});

describe("tokenStorage.setTokens", () => {
  it("persists under the expected keys", () => {
    tokenStorage.setTokens({
      accessToken: "a",
      refreshToken: "r",
      idToken: "i",
    });

    expect(localStorage.getItem("access_token")).toBe("a");
    expect(localStorage.getItem("refresh_token")).toBe("r");
    expect(localStorage.getItem("id_token")).toBe("i");
  });

  // The refresh response returns a new access and id token but no refresh
  // token — overwriting blindly would log the user out at the next refresh.
  it("keeps the existing refresh token when the new set omits it", () => {
    tokenStorage.setTokens({
      accessToken: "a1",
      refreshToken: "r1",
      idToken: "i1",
    });

    tokenStorage.setTokens({ accessToken: "a2", idToken: "i2" });

    expect(tokenStorage.getTokens()).toEqual({
      accessToken: "a2",
      refreshToken: "r1",
      idToken: "i2",
    });
  });

  it("overwrites the refresh token when a new one is handed over", () => {
    tokenStorage.setTokens({ accessToken: "a1", refreshToken: "r1" });
    tokenStorage.setTokens({ accessToken: "a2", refreshToken: "r2" });

    expect(tokenStorage.getTokens()?.refreshToken).toBe("r2");
  });
});

describe("tokenStorage.clear", () => {
  it("removes every token", () => {
    tokenStorage.setTokens({
      accessToken: "a",
      refreshToken: "r",
      idToken: "i",
    });

    tokenStorage.clear();

    expect(tokenStorage.getTokens()).toBeNull();
    expect(localStorage.getItem("refresh_token")).toBeNull();
    expect(localStorage.getItem("id_token")).toBeNull();
  });

  it("is safe to call on an empty store", () => {
    expect(() => tokenStorage.clear()).not.toThrow();
  });
});
