export interface AuthTokens {
  accessToken: string;
  refreshToken?: string;
}

const ACCESS_KEY = "access_token";
const REFRESH_KEY = "refresh_token";

export const tokenStorage = {
  getTokens(): AuthTokens | null {
    const access = localStorage.getItem(ACCESS_KEY);
    if (!access) return null;
    return {
      accessToken: access,
      refreshToken: localStorage.getItem(REFRESH_KEY) ?? undefined,
    };
  },

  setTokens(tokens: AuthTokens) {
    localStorage.setItem(ACCESS_KEY, tokens.accessToken);
    if (tokens.refreshToken) {
      localStorage.setItem(REFRESH_KEY, tokens.refreshToken);
    }
  },

  clear() {
    localStorage.removeItem(ACCESS_KEY);
    localStorage.removeItem(REFRESH_KEY);
  },
};
