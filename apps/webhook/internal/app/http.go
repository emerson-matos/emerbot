package app

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/aws/aws-lambda-go/events"
)

type WebhookHTTPRequest struct {
	Method string
	Query  map[string]string
	Header map[string]string
	Body   []byte
}

type WebhookHTTPResponse struct {
	StatusCode int
	Headers    map[string]string
	Body       string
}

func (a *App) HandleWebhookHTTP(ctx context.Context, req WebhookHTTPRequest) (WebhookHTTPResponse, error) {
	switch req.Method {
	case http.MethodGet:
		resp := a.HandleVerification(req.Query["hub.mode"], req.Query["hub.verify_token"], req.Query["hub.challenge"])
		return WebhookHTTPResponse{
			StatusCode: resp.StatusCode,
			Headers:    resp.Headers,
			Body:       resp.Body,
		}, nil
	case http.MethodPost:
		if !validSignature(req.Body, headerValue(req.Header, "X-Hub-Signature-256"), a.secret) {
			log.Printf("rejecting webhook with invalid signature")
			return httpJSONResponse(http.StatusUnauthorized, map[string]string{"error": "invalid signature"})
		}

		messages, err := FromWAWebhook(req.Body)
		if err != nil {
			return httpJSONResponse(http.StatusBadRequest, map[string]string{"error": "invalid json"})
		}

		// Process every batched message, but always answer Meta with a single
		// 200 — a non-200 makes Meta retry the whole batch for up to 7 days.
		for i := range messages {
			if _, _, herr := a.Handle(ctx, messages[i]); herr != nil {
				log.Printf("handling webhook message %s: %v", messages[i].MessageID, herr)
			}
		}
		return httpJSONResponse(http.StatusOK, map[string]bool{"ok": true})
	default:
		return httpJSONResponse(http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (a *App) HandleLambda(ctx context.Context, event events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	body := []byte(event.Body)
	if event.IsBase64Encoded {
		decoded, err := decodeBase64Body(event.Body)
		if err != nil {
			return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid base64 body"})
		}
		body = decoded
	}

	resp, err := a.HandleWebhookHTTP(ctx, WebhookHTTPRequest{
		Method: event.RequestContext.HTTP.Method,
		Query:  event.QueryStringParameters,
		Header: event.Headers,
		Body:   body,
	})
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Headers,
		Body:       resp.Body,
	}, nil
}

func httpJSONResponse(statusCode int, payload any) (WebhookHTTPResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return WebhookHTTPResponse{}, err
	}

	return WebhookHTTPResponse{
		StatusCode: statusCode,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: string(body),
	}, nil
}

func headerValue(headers map[string]string, key string) string {
	for k, v := range headers {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}
