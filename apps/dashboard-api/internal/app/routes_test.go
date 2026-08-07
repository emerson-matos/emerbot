package app

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The gateway enumerates every dashboard route by hand, and nothing links that
// list to the mux in app.go: a handler registered without its route 404s at the
// gateway before the Lambda is ever called — and without CORS headers, since the
// API only attaches them to routes that exist — while a route left behind after
// a handler is deleted wakes the Lambda to answer 405.
//
// The .tf file says in a comment that the two must be kept in sync. This is that
// comment, enforced. It reads both sides as text, which is unusual for a Go test
// and is the point: the drift it catches is exactly the drift a reader has to
// notice by eye today.
const gatewayTF = "../../../../infra/modules/api_gateway_lambda/main.tf"

// gatewayOnly are gateway routes with no matching mux pattern. There is no
// exception list in the other direction: every pattern the mux serves needs a
// route, public (GET /health) or protected, or it is unreachable in production.
var gatewayOnly = map[string]string{
	// Answered by the withCORS wrapper rather than by a mux pattern — see the
	// note on dashboard_public_routes.
	"OPTIONS /{proxy+}": "handled by the CORS wrapper, not by a mux pattern",
}

var (
	muxRouteRE     = regexp.MustCompile(`mux\.Handle(?:Func)?\("([A-Z]+ [^"]+)"`)
	tfRouteRE      = regexp.MustCompile(`"([A-Z]+ /[^"]*)"`)
	tfRouteListsRE = regexp.MustCompile(`(?s)dashboard_(?:protected|public)_routes = toset\(\[(.*?)\]\)`)
)

func TestGatewayRoutesMatchTheMux(t *testing.T) {
	t.Parallel()

	mux := readRoutes(t, "app.go", muxRouteRE, func(s string) string { return s })
	gateway := readRoutes(t, gatewayTF, tfRouteRE, func(s string) string {
		// Only the two dashboard lists; the webhook's routes in the same file
		// belong to a different Lambda and a different mux.
		var b strings.Builder
		for _, block := range tfRouteListsRE.FindAllStringSubmatch(s, -1) {
			b.WriteString(block[1])
		}
		if b.Len() == 0 {
			t.Fatalf("no dashboard route lists found in %s — did the locals block move?", gatewayTF)
		}
		return b.String()
	})

	for route := range mux {
		if _, ok := gateway[route]; ok {
			continue
		}
		t.Errorf("%s is registered in app.go but has no route in %s — it would 404 at the gateway, "+
			"with no CORS headers, before the Lambda is called", route, gatewayTF)
	}

	for route := range gateway {
		if _, ok := mux[route]; ok {
			continue
		}
		if why, expected := gatewayOnly[route]; expected {
			t.Logf("%s: not in the mux on purpose (%s)", route, why)
			continue
		}
		t.Errorf("%s is routed by %s but no handler is registered for it in app.go — "+
			"the gateway would wake the Lambda to answer 405", route, gatewayTF)
	}
}

func readRoutes(t *testing.T, path string, re *regexp.Regexp, scope func(string) string) map[string]bool {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	routes := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(scope(string(body)), -1) {
		routes[m[1]] = true
	}
	if len(routes) == 0 {
		t.Fatalf("no routes found in %s — the test's own parsing is broken", path)
	}
	return routes
}
