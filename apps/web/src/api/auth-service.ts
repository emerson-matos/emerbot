import { tokenStorage } from "./token-storage";
import type { AuthTokens } from "./token-storage";
import { cognitoInitiateAuth } from "./cognito";

type AuthState = "authenticated" | "unauthenticated";
type Listener = (state: { isAuthenticated: boolean }) => void;

class AuthService {
  #state: AuthState;
  #listeners = new Set<Listener>();

  constructor() {
    this.#state = tokenStorage.getTokens()
      ? "authenticated"
      : "unauthenticated";
  }

  get isAuthenticated() {
    return this.#state === "authenticated";
  }

  getTokens() {
    return tokenStorage.getTokens();
  }

  login(tokens: AuthTokens) {
    tokenStorage.setTokens(tokens);
    this.#state = "authenticated";
    this.#notify();
  }

  logout() {
    tokenStorage.clear();
    this.#state = "unauthenticated";
    this.#notify();
  }

  async refresh(): Promise<boolean> {
    if (refreshInFlight) return refreshInFlight;

    const tokens = tokenStorage.getTokens();
    if (!tokens?.refreshToken) {
      this.logout();
      return false;
    }

    refreshInFlight = (async () => {
      try {
        const result = await cognitoInitiateAuth("REFRESH_TOKEN_AUTH", {
          REFRESH_TOKEN: tokens.refreshToken!,
        });
        this.login({
          accessToken: result.AccessToken,
          refreshToken: result.RefreshToken,
        });
        return true;
      } catch {
        this.logout();
        return false;
      } finally {
        refreshInFlight = null;
      }
    })();

    return refreshInFlight;
  }

  subscribe(fn: Listener) {
    this.#listeners.add(fn);
    return () => this.#listeners.delete(fn);
  }

  #notify() {
    const payload = { isAuthenticated: this.isAuthenticated };
    this.#listeners.forEach((fn) => fn(payload));
  }
}

let refreshInFlight: Promise<boolean> | null = null;

export const authService = new AuthService();
