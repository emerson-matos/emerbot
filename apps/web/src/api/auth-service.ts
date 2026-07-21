import { tokenStorage } from "./token-storage";
import type { AuthTokens } from "./token-storage";
import { cognitoInitiateAuth } from "./cognito";
import { decodeIdToken } from "./jwt";
import type { UserProfile } from "./types";

export interface AuthSnapshot {
  isAuthenticated: boolean;
  user: UserProfile | null;
  sessionExpired: boolean;
}

type Listener = (snapshot: AuthSnapshot) => void;

class AuthService {
  #authenticated: boolean;
  #sessionExpired = false;
  #listeners = new Set<Listener>();

  constructor() {
    this.#authenticated = tokenStorage.getTokens() !== null;
  }

  get isAuthenticated() {
    return this.#authenticated;
  }

  get sessionExpired() {
    return this.#sessionExpired;
  }

  getTokens() {
    return tokenStorage.getTokens();
  }

  // Display profile derived from the ID token. Decoded on demand so it always
  // reflects the currently stored token (login and refresh both update it).
  getUser(): UserProfile | null {
    const idToken = tokenStorage.getTokens()?.idToken;
    if (!idToken) return null;
    const claims = decodeIdToken(idToken);
    if (!claims) return null;
    return {
      name: claims.name,
      email: claims.email,
      phone: claims.phone_number,
    };
  }

  snapshot(): AuthSnapshot {
    return {
      isAuthenticated: this.#authenticated,
      user: this.getUser(),
      sessionExpired: this.#sessionExpired,
    };
  }

  login(tokens: AuthTokens) {
    tokenStorage.setTokens(tokens);
    this.#authenticated = true;
    this.#sessionExpired = false;
    this.#notify();
  }

  logout() {
    tokenStorage.clear();
    this.#authenticated = false;
    this.#notify();
  }

  async refresh(): Promise<boolean> {
    if (refreshInFlight) return refreshInFlight;

    const tokens = tokenStorage.getTokens();
    if (!tokens?.refreshToken) {
      this.#expire();
      return false;
    }

    refreshInFlight = (async () => {
      try {
        const result = await cognitoInitiateAuth("REFRESH_TOKEN_AUTH", {
          REFRESH_TOKEN: tokens.refreshToken!,
        });
        this.login({
          accessToken: result.AccessToken,
          idToken: result.IdToken,
          refreshToken: result.RefreshToken,
        });
        return true;
      } catch {
        this.#expire();
        return false;
      } finally {
        refreshInFlight = null;
      }
    })();

    return refreshInFlight;
  }

  subscribe(fn: Listener) {
    this.#listeners.add(fn);
    return () => {
      this.#listeners.delete(fn);
    };
  }

  // The session ended because tokens expired and couldn't be refreshed (as
  // opposed to an explicit logout). Flag it so ProtectedRoute can surface the
  // "session expired" notice; login() clears the flag.
  #expire() {
    this.#sessionExpired = true;
    this.logout();
  }

  #notify() {
    const snapshot = this.snapshot();
    this.#listeners.forEach((fn) => fn(snapshot));
  }
}

let refreshInFlight: Promise<boolean> | null = null;

export const authService = new AuthService();
