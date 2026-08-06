package agentprompt

import (
	"strings"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/shared"
)

// The failure this pins: 23h06 in Brazil is already tomorrow in UTC, and the
// message timestamp the prompt is dated with arrives in UTC. The model was told
// "Hoje é 06/08/2026" at eleven at night on the 5th, went and asked for the
// 6th's target, got a day that had not begun, and answered "o faturamento de
// hoje é R$ 0,00" — about a day that had traded all afternoon.
func TestFinanceDatesThePromptInThePharmacysDay(t *testing.T) {
	t.Setenv("APP_TIMEZONE", shared.DefaultTimezone)

	// 2026-08-06T02:06:00Z is 05/08 23h06 in São Paulo — a Wednesday.
	late := time.Date(2026, 8, 6, 2, 6, 0, 0, time.UTC)
	got := Finance(late)

	if !strings.Contains(got, "Hoje é quarta-feira, 05/08/2026, e agora são 23:06") {
		t.Errorf("prompt does not date itself in the pharmacy's day:\n%s", firstLines(got, 8))
	}
	if strings.Contains(got, "06/08/2026") {
		t.Errorf("prompt still carries the UTC date:\n%s", firstLines(got, 8))
	}
	if !strings.Contains(got, "Fuso horário: "+shared.DefaultTimezone) {
		t.Errorf("prompt does not name the timezone it rendered in:\n%s", firstLines(got, 8))
	}
}

// The zone line is printed from the same Location the date is rendered in, so a
// deploy whose zone fell back to UTC says so instead of claiming São Paulo over
// a UTC date — which is exactly how the bug above stayed invisible.
func TestFinanceNamesTheZoneItActuallyUsed(t *testing.T) {
	t.Setenv("APP_TIMEZONE", "UTC")

	got := Finance(time.Date(2026, 8, 6, 2, 6, 0, 0, time.UTC))

	if !strings.Contains(got, "Fuso horário: UTC") {
		t.Errorf("prompt claims a zone it did not render in:\n%s", firstLines(got, 8))
	}
	if !strings.Contains(got, "06/08/2026") {
		t.Errorf("prompt should render the UTC date when UTC is the configured zone:\n%s", firstLines(got, 8))
	}
}

func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
