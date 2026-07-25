package cli

import (
	"testing"

	"github.com/moontechs/signalforge/internal/config"
)

func TestCheckEnvVarsReddit(t *testing.T) {
	for _, value := range []struct {
		name, id, secret, idStatus, secretStatus string
	}{
		{"disabled", "", "", "ℹ️", "ℹ️"},
		{"enabled missing", "", "", "❌", "❌"},
		{"enabled ID only", "client", "", "✅", "❌"},
		{"enabled secret only", "", "secret", "❌", "✅"},
		{"enabled present", "client", "secret", "✅", "✅"},
	} {
		t.Run(value.name, func(t *testing.T) {
			t.Setenv("REDDIT_CLIENT_ID", value.id)
			t.Setenv("REDDIT_CLIENT_SECRET", value.secret)
			cfg := config.DefaultConfig()
			cfg.Sources.Reddit.Enabled = value.name != "disabled"
			results := checkEnvVars(cfg)
			byName := make(map[string]checkResult, len(results))
			for _, result := range results {
				byName[result.Name] = result
			}
			id, ok := byName["REDDIT_CLIENT_ID"]
			if !ok || id.Status != value.idStatus {
				t.Fatalf("Reddit client ID result = %+v, present=%v, want status %q", id, ok, value.idStatus)
			}
			secret, ok := byName["REDDIT_CLIENT_SECRET"]
			if !ok || secret.Status != value.secretStatus {
				t.Fatalf("Reddit client secret result = %+v, present=%v, want status %q", secret, ok, value.secretStatus)
			}
		})
	}
}
