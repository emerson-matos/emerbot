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

// The caderninho's vocabulary is a rule, not a style note: nothing was promised,
// so nothing is late. A prompt that let "vencido" through would have the model
// dunning a customer over a debt that has no due date (ADR-027 §2).
func TestFinanceForbidsTheLanguageOfLateness(t *testing.T) {
	got := Finance(time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC))

	if !strings.Contains(got, "em aberto há N dias") {
		t.Error("prompt does not give the model the only phrasing fiado has")
	}
	if !strings.Contains(got, `Nunca diga "vencido", "atrasado", "em atraso" ou "inadimplente"`) {
		t.Error("prompt does not forbid the language of lateness")
	}

	// And the words exist nowhere except inside that prohibition. The rest of
	// the prompt is about the ledger, where "vencido" is correct — so what this
	// pins is that the caderninho's section is the only place they appear after
	// it, i.e. nothing tells the model to use them about fiado.
	_, caderninho, found := strings.Cut(got, "Caderninho de fiado")
	if !found {
		t.Fatal("prompt has no caderninho section")
	}
	for _, banned := range []string{"vencido", "atrasado", "inadimplente"} {
		lines := strings.Split(caderninho, "\n")
		for _, line := range lines {
			if strings.Contains(line, banned) && !strings.Contains(line, "Nunca diga") && !strings.Contains(line, "nada está atrasado") {
				t.Errorf("a seção do caderninho usa %q fora da regra que o proíbe: %q", banned, line)
			}
		}
	}
}

// The two systems must not leak into each other: a fiado sale that becomes a
// ledger entry is counted as faturamento, which is the one thing ADR-027 exists
// to prevent.
func TestFinanceKeepsTheCaderninhoApartFromTheLedger(t *testing.T) {
	got := Finance(time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC))

	for _, rule := range []string{
		"FIADO NUNCA VIRA LANÇAMENTO E LANÇAMENTO NUNCA VIRA FIADO",
		"NUNCA registre fiado sem saber de quem é",
		"cliente_novo=true",
	} {
		if !strings.Contains(got, rule) {
			t.Errorf("prompt is missing the rule %q", rule)
		}
	}
}
