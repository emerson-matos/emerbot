package app

import (
	"bytes"
	"context"
	"net/http"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
)

// responseRecorder captures the response written by the http.Handler so we
// can translate it back into an API Gateway V2 response.
type responseRecorder struct {
	headers    http.Header
	body       bytes.Buffer
	statusCode int
}

func (r *responseRecorder) Header() http.Header         { return r.headers }
func (r *responseRecorder) WriteHeader(code int)        { r.statusCode = code }
func (r *responseRecorder) Write(b []byte) (int, error) { return r.body.Write(b) }

func (r *responseRecorder) toAPIGWResponse() events.APIGatewayV2HTTPResponse {
	headers := make(map[string]string, len(r.headers))
	for k, vals := range r.headers {
		headers[k] = strings.Join(vals, ", ")
	}
	return events.APIGatewayV2HTTPResponse{
		StatusCode: r.statusCode,
		Headers:    headers,
		Body:       r.body.String(),
	}
}

// apiGWEventToHTTPRequest converts an API Gateway V2 HTTP event into a
// standard http.Request so the existing ServeMux can handle it.
func apiGWEventToHTTPRequest(ctx context.Context, event events.APIGatewayV2HTTPRequest) (*http.Request, error) {
	body := event.Body
	req, err := http.NewRequestWithContext(
		ctx,
		event.RequestContext.HTTP.Method,
		event.RequestContext.HTTP.Path,
		strings.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	for k, v := range event.Headers {
		req.Header.Set(k, v)
	}
	if claims := gatewayClaims(event); claims.UserID != "" {
		req = req.WithContext(apiauth.WithClaims(req.Context(), claims))
	}
	if q := event.RawQueryString; q != "" {
		req.URL.RawQuery = q
	}
	return req, nil
}

func gatewayClaims(event events.APIGatewayV2HTTPRequest) apiauth.Claims {
	if event.RequestContext.Authorizer == nil || event.RequestContext.Authorizer.JWT == nil {
		return apiauth.Claims{}
	}
	claims := event.RequestContext.Authorizer.JWT.Claims
	return apiauth.Claims{UserID: claims["sub"], Email: claims["email"], Name: claims["username"]}
}
