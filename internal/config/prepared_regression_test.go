package config_test

import (
	"strings"
	"testing"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
)

// A successful prepared value must remain representable on the existing JSON
// boundary. Go strings may contain bytes that JSON cannot represent as UTF-8.
func TestPreparedListenRejectsInvalidUTF8AtConfigurationBoundary(t *testing.T) {
	invalidListen := string([]byte{0xff}) + ":8080"
	t.Run("typed declaration", func(t *testing.T) {
		document := preparedDocument()
		document.Server.Listen = invalidListen
		cfg, err := config.Parse(document)
		if err == nil || !cfg.IsZero() || !strings.Contains(err.Error(), "server.listen") {
			t.Fatalf("non-serializable declaration escaped config parsing: zero=%t error=%v", cfg.IsZero(), err)
		}
	})
	t.Run("runtime override", func(t *testing.T) {
		cfg, err := config.Parse(preparedDocument())
		if err != nil {
			t.Fatal(err)
		}
		runtime, err := config.Prepare(cfg, map[string]string{"FRD_LISTEN": invalidListen}, []config.CredentialName{"company-app"})
		if err == nil || !runtime.IsZero() || !strings.Contains(err.Error(), "FRD_LISTEN") {
			t.Fatalf("non-serializable override escaped runtime preparation: zero=%t error=%v", runtime.IsZero(), err)
		}
	})
}
