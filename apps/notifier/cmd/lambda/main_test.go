package main

import (
	"encoding/json"
	"testing"

	"github.com/emerson/emerbot/apps/notifier/internal/notifier"
)

// TestScheduledRunDecodesWhatTheSchedulerSends closes the last gap in the chain
// that decides which of the day's two runs this invocation is.
//
// The Terraform literals are tied to the RunKind constants by a test in the
// notifier package, and ParseRunKind is tested against those constants. Neither
// touches the step in between: the `json:"run"` tag on this struct. Rename it
// and both of those stay green while every afternoon invocation decodes to the
// zero value — which ParseRunKind reads as the morning digest, so the pharmacy
// would get the day's summary twice and never the reminder.
//
// The payloads below are written out as raw JSON on purpose. Building them from
// the struct would test the struct against itself.
func TestScheduledRunDecodesWhatTheSchedulerSends(t *testing.T) {
	tests := []struct {
		name string
		body string
		want notifier.RunKind
	}{
		{
			name: "morning schedule",
			body: `{"run":"digest"}`,
			want: notifier.RunDigest,
		},
		{
			name: "afternoon schedule",
			body: `{"run":"saidas_tarde"}`,
			want: notifier.RunOpenBills,
		},
		{
			// EventBridge Scheduler sends its own envelope when a target has no
			// input configured. Unknown fields are ignored and `run` stays empty,
			// which ParseRunKind resolves to the digest — so a schedule created
			// before the field existed keeps working.
			name: "no input configured",
			body: `{"version":"0","id":"a1","detail-type":"Scheduled Event","detail":{}}`,
			want: notifier.RunDigest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var event scheduledRun
			if err := json.Unmarshal([]byte(tc.body), &event); err != nil {
				t.Fatalf("unmarshal %s: %v", tc.body, err)
			}
			got, err := notifier.ParseRunKind(event.Run)
			if err != nil {
				t.Fatalf("ParseRunKind(%q): %v", event.Run, err)
			}
			if got != tc.want {
				t.Errorf("%s decoded to %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}

// TestScheduledRunRejectsAnUnknownKind: a typo in the scheduler's input fails
// the invocation rather than falling back to the digest. One run erroring
// loudly is recoverable; the wrong message in front of a real person is not.
func TestScheduledRunRejectsAnUnknownKind(t *testing.T) {
	var event scheduledRun
	if err := json.Unmarshal([]byte(`{"run":"saidas-tarde"}`), &event); err != nil {
		t.Fatal(err)
	}
	if _, err := notifier.ParseRunKind(event.Run); err == nil {
		t.Error("a hyphen instead of an underscore must not silently become the digest")
	}
}
