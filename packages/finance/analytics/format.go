package analytics

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// weekdayNames is the full Portuguese name for each weekday, indexed by
// time.Weekday (0=Sunday). It is used only where this package is already
// writing Portuguese prose — the WhatsApp digest and the bot payload, both of
// which are read aloud. The structured types carry the weekday as a number and
// no label at all: a name is a rendering, and rendering happens at the edge
// that knows which language it is speaking.
var weekdayNames = [daysInWeek]string{"domingo", "segunda", "terça", "quarta", "quinta", "sexta", "sábado"}

// weekdayArticle is "um"/"uma" for each weekday: segunda through sexta are
// -feira and so feminine, domingo and sábado are not. A single hardcoded "uma"
// read "uma domingo" two days a week.
var weekdayArticle = [daysInWeek]string{"um", "uma", "uma", "uma", "uma", "uma", "um"}

// weekdayWithArticle renders "uma terça" / "um domingo".
func weekdayWithArticle(d time.Weekday) string {
	return weekdayArticle[int(d)] + " " + weekdayNames[int(d)]
}

// monthAbbrev mirrors what pt-BR gives for {month: 'short'} — the trailing dot
// included, because that is what the dashboard labels used to show.
var monthAbbrev = [12]string{
	"jan.", "fev.", "mar.", "abr.", "mai.", "jun.",
	"jul.", "ago.", "set.", "out.", "nov.", "dez.",
}

// formatBRL renders centavos as Brazilian currency ("R$ 1.234,56"). The
// browser used Intl.NumberFormat for this; the strings end up inside insight
// and recommendation copy, so they are built here rather than left to each
// consumer to re-derive.
func formatBRL(centavos int64) string {
	negative := centavos < 0
	if negative {
		centavos = -centavos
	}
	reais := centavos / 100
	cents := centavos % 100

	digits := strconv.FormatInt(reais, 10)
	var b strings.Builder
	if negative {
		b.WriteString("-")
	}
	b.WriteString("R$ ")
	// Group thousands with dots, right to left.
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteString(".")
		}
		b.WriteRune(d)
	}
	fmt.Fprintf(&b, ",%02d", cents)
	return b.String()
}

// There is deliberately no float variant of formatBRL: every amount that
// reaches the copy is already a whole centavo, rounded once where it was
// derived rather than once per consumer that prints it.

// dayMonthLabel renders "12 de jul." — the short day label used for
// highlights.
func dayMonthLabel(t time.Time) string {
	return fmt.Sprintf("%02d de %s", t.Day(), monthAbbrev[int(t.Month())-1])
}

// monthYearLabel renders "jul. de 2026" — the history chart's bar label.
func monthYearLabel(t time.Time) string {
	return fmt.Sprintf("%s de %d", monthAbbrev[int(t.Month())-1], t.Year())
}

// roundToInt64 rounds half away from zero, matching Math.round for the
// positive values these helpers see and staying symmetric for negatives.
func roundToInt64(v float64) int64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return int64(math.Round(v))
}

// roundToInt is roundToInt64 for the percentage fields, which are plain ints.
func roundToInt(v float64) int {
	return int(roundToInt64(v))
}

// pluralDias renders "1 dia" / "N dias".
func pluralDias(n int) string {
	if n == 1 {
		return "1 dia"
	}
	return strconv.Itoa(n) + " dias"
}
