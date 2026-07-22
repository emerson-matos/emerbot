package auth

// Claims are the authenticated identity attached to a request context, once
// a JWT (Cognito-issued, verified either by API Gateway or by
// NewLocalCognitoMiddleware) has been established.
type Claims struct {
	UserID  string
	Email   string
	Name    string
	Phone   string // Cognito's phone_number attribute, E.164 (e.g. "+5511987654321")
	Subject string // the real, never-overridden Cognito sub — see GatewayMiddleware
}
