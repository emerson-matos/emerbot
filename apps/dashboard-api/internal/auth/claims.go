package auth

// Claims are the authenticated identity attached to a request context, once
// a JWT (Cognito-issued, verified either by API Gateway or by
// NewLocalCognitoMiddleware) has been established.
type Claims struct {
	UserID string
	Email  string
	Name   string
}
