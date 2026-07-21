const COGNITO_ENDPOINT =
  import.meta.env.VITE_COGNITO_ENDPOINT ?? "http://localhost:9229";
const COGNITO_CLIENT_ID = import.meta.env.VITE_COGNITO_CLIENT_ID ?? "";

export class CognitoAuthError extends Error {
  type: string;
  constructor(type: string, message: string) {
    super(message);
    this.type = type;
  }
}

export interface CognitoAuthResult {
  AccessToken: string;
  IdToken: string;
  RefreshToken?: string;
  ExpiresIn: number;
  TokenType: string;
}

export async function cognitoInitiateAuth(
  authFlow: "USER_PASSWORD_AUTH" | "REFRESH_TOKEN_AUTH",
  authParameters: Record<string, string>,
): Promise<CognitoAuthResult> {
  const res = await fetch(`${COGNITO_ENDPOINT}/`, {
    method: "POST",
    headers: {
      "Content-Type": "application/x-amz-json-1.1",
      "X-Amz-Target": "AWSCognitoIdentityProviderService.InitiateAuth",
    },
    body: JSON.stringify({
      AuthFlow: authFlow,
      ClientId: COGNITO_CLIENT_ID,
      AuthParameters: authParameters,
    }),
  });
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new CognitoAuthError(
      body.__type ?? "UnknownError",
      body.message ?? "Authentication failed",
    );
  }
  return body.AuthenticationResult;
}
