package envelope

import (
	"testing"

	"github.com/emerson/emerbot/packages/payments"
)

func TestReadMetadata(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    payments.Provider
		wantErr bool
	}{
		{"pagbank", `{"provider":"pagbank","date":"2026-07-23","transactional":[]}`, payments.ProviderPagBank, false},
		{"unknown provider", `{"provider":"cielo"}`, "", true},
		{"missing provider", `{"date":"2026-07-23"}`, "", true},
		{"malformed json", `{`, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			meta, err := ReadMetadata([]byte(c.raw))
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", c.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadMetadata: %v", err)
			}
			if meta.Provider != c.want {
				t.Errorf("provider = %q, want %q", meta.Provider, c.want)
			}
		})
	}
}
