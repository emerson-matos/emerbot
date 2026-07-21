// Minimal JWT payload decode for reading display claims (name/email/phone) out
// of the Cognito ID token. No signature verification — that is the API
// Gateway authorizer's job; the browser only trusts these for display.
export interface IdTokenClaims {
  name?: string;
  email?: string;
  phone_number?: string;
  [claim: string]: unknown;
}

export function decodeIdToken(idToken: string): IdTokenClaims | null {
  try {
    const payload = idToken.split(".")[1];
    const base64 = payload.replace(/-/g, "+").replace(/_/g, "/");
    const padded = base64.padEnd(
      base64.length + ((4 - (base64.length % 4)) % 4),
      "=",
    );
    const json = new TextDecoder().decode(
      Uint8Array.from(atob(padded), (c) => c.charCodeAt(0)),
    );
    return JSON.parse(json) as IdTokenClaims;
  } catch {
    return null;
  }
}
