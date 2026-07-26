package shared

import (
	"log/slog"
	"testing"
	"time"
)

func TestGetenv(t *testing.T) {
	cases := []struct {
		name            string
		set             bool
		value, fallback string
		want            string
	}{
		{"unset falls back", false, "", "padrao", "padrao"},
		{"set wins", true, "definido", "padrao", "definido"},
		// An empty variable is treated as unset: an env var blanked out in a
		// Lambda config must not silently disable a defaulted setting.
		{"empty falls back", true, "", "padrao", "padrao"},
		{"empty fallback is allowed", false, "", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const key = "EMERBOT_TEST_GETENV"
			if tc.set {
				t.Setenv(key, tc.value)
			}
			if got := Getenv(key, tc.fallback); got != tc.want {
				t.Fatalf("Getenv = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGetenvInt(t *testing.T) {
	cases := []struct {
		name     string
		set      bool
		value    string
		fallback int
		want     int
	}{
		{"unset falls back", false, "", 7, 7},
		{"parses a number", true, "42", 7, 42},
		{"parses a negative number", true, "-3", 7, -3},
		{"zero is honoured, not treated as unset", true, "0", 7, 0},
		// A typo'd value must fall back rather than crash or become zero.
		{"non-numeric falls back", true, "muitos", 7, 7},
		{"empty falls back", true, "", 7, 7},
		{"float falls back", true, "4.5", 7, 7},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const key = "EMERBOT_TEST_GETENV_INT"
			if tc.set {
				t.Setenv(key, tc.value)
			}
			if got := GetenvInt(key, tc.fallback); got != tc.want {
				t.Fatalf("GetenvInt = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestInitSlogSetsLevelFromEnv(t *testing.T) {
	t.Run("defaults to info", func(t *testing.T) {
		InitSlog()
		if slog.Default().Enabled(t.Context(), slog.LevelDebug) {
			t.Fatal("debug must be off without LOG_LEVEL=debug")
		}
		if !slog.Default().Enabled(t.Context(), slog.LevelInfo) {
			t.Fatal("info must be on by default")
		}
	})

	t.Run("LOG_LEVEL=debug turns debug on", func(t *testing.T) {
		t.Setenv("LOG_LEVEL", "debug")
		InitSlog()
		if !slog.Default().Enabled(t.Context(), slog.LevelDebug) {
			t.Fatal("LOG_LEVEL=debug must enable debug logging")
		}
	})

	t.Run("an unrecognized level stays at info", func(t *testing.T) {
		t.Setenv("LOG_LEVEL", "verboso")
		InitSlog()
		if slog.Default().Enabled(t.Context(), slog.LevelDebug) {
			t.Fatal("an unknown LOG_LEVEL must not enable debug")
		}
	})
}

func TestPharmacyLocation(t *testing.T) {
	t.Run("defaults to the pharmacy's calendar", func(t *testing.T) {
		if got := PharmacyLocation().String(); got != DefaultTimezone {
			t.Fatalf("location = %q, want %q", got, DefaultTimezone)
		}
	})

	t.Run("APP_TIMEZONE wins", func(t *testing.T) {
		t.Setenv("APP_TIMEZONE", "America/Manaus")
		if got := PharmacyLocation().String(); got != "America/Manaus" {
			t.Fatalf("location = %q, want America/Manaus", got)
		}
	})

	t.Run("NOTIFIER_TIMEZONE is honoured as the older name", func(t *testing.T) {
		// A deploy that has not picked up APP_TIMEZONE yet must keep its
		// configured zone rather than silently jumping to UTC.
		t.Setenv("NOTIFIER_TIMEZONE", "America/Manaus")
		if got := PharmacyLocation().String(); got != "America/Manaus" {
			t.Fatalf("location = %q, want America/Manaus", got)
		}
	})

	t.Run("APP_TIMEZONE takes precedence over NOTIFIER_TIMEZONE", func(t *testing.T) {
		t.Setenv("NOTIFIER_TIMEZONE", "America/Manaus")
		t.Setenv("APP_TIMEZONE", "America/Recife")
		if got := PharmacyLocation().String(); got != "America/Recife" {
			t.Fatalf("location = %q, want America/Recife", got)
		}
	})

	t.Run("an unloadable zone degrades to UTC", func(t *testing.T) {
		// A clock that is wrong by hours beats a service that refuses to start.
		t.Setenv("APP_TIMEZONE", "Mars/Olympus_Mons")
		if got := PharmacyLocation(); got != time.UTC {
			t.Fatalf("location = %v, want UTC", got)
		}
	})
}
