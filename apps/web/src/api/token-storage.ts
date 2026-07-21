export interface AuthTokens {
  accessToken: string;
  refreshToken?: string;
  idToken?: string;
}

const ACCESS_KEY = "access_token";
const REFRESH_KEY = "refresh_token";
const ID_KEY = "id_token";

export const tokenStorage = {
  getTokens(): AuthTokens | null {
    const access = localStorage.getItem(ACCESS_KEY);
    if (!access) return null;
    return {
      accessToken: access,
      refreshToken: localStorage.getItem(REFRESH_KEY) ?? undefined,
      idToken: localStorage.getItem(ID_KEY) ?? undefined,
    };
  },

  setTokens(tokens: AuthTokens) {
    localStorage.setItem(ACCESS_KEY, tokens.accessToken);
    // The refresh flow returns a new access + ID token but no refresh token,
    // so only overwrite the ones we were actually handed.
    if (tokens.refreshToken) localStorage.setItem(REFRESH_KEY, tokens.refreshToken);
    if (tokens.idToken) localStorage.setItem(ID_KEY, tokens.idToken);
  },

  clear() {
    localStorage.removeItem(ACCESS_KEY);
    localStorage.removeItem(REFRESH_KEY);
    localStorage.removeItem(ID_KEY);
  },
};
