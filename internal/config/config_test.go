package config_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
)

const minimal = `version: 1
sources:
  company:
    owners: [Acme]
    credential: company-app
filters:
  applications:
    sources: [company]
    include:
      - topics:
          all: [GitOps]
          any: [backend, worker]
        nameRegex: '^service-'
      - repositories: [Acme/legacy]
    exclude:
      - nameRegex: '-sandbox$'
`

func TestDecodeDefaultsAndTypedFields(t *testing.T) {
	cfg, err := config.Decode([]byte(minimal))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Document().Version != 1 || cfg.Server().Listen != ":8080" || time.Duration(cfg.Scan().Interval) != 10*time.Minute || time.Duration(cfg.Scan().Timeout) != 2*time.Minute {
		t.Fatalf("unexpected defaults: %#v", cfg)
	}
	if got := cfg.Sources()["company"]; !reflect.DeepEqual(got, config.Source{Owners: []string{"Acme"}, Credential: "company-app"}) {
		t.Fatalf("source = %#v", got)
	}
	f := cfg.Document().Filters["applications"]
	if !reflect.DeepEqual(f.Sources, []config.SourceName{"company"}) || len(f.Include) != 2 || len(f.Exclude) != 1 || f.Include[0].NameRegex != "^service-" {
		t.Fatalf("filter = %#v", f)
	}
	if !reflect.DeepEqual(f.Include[0].Topics, &config.Topics{All: []string{"GitOps"}, Any: []string{"backend", "worker"}}) {
		t.Fatalf("topics = %#v", f.Include[0].Topics)
	}
}

func TestDecodeStrictValidationHasFieldPaths(t *testing.T) {
	cases := []struct{ name, input, path string }{
		{"unknown root", minimal + "credentials: {}\n", "credentials"},
		{"unknown nested", strings.Replace(minimal, "credential: company-app", "credential: company-app\n    token: secret", 1), "sources.company.token"},
		{"duplicate YAML key", minimal + "version: 1\n", "version"},
		{"duplicate nested key", strings.Replace(minimal, "owners: [Acme]", "owners: [Acme]\n    owners: [Other]", 1), "sources.company.owners"},
		{"multiple documents", minimal + "---\nversion: 1\n", "document"},
		{"missing version", strings.TrimPrefix(minimal, "version: 1\n"), "version"},
		{"unsupported version", strings.Replace(minimal, "version: 1", "version: 2", 1), "version"},
		{"invalid source name", strings.ReplaceAll(minimal, "company:", "Company:"), "sources.Company"},
		{"invalid filter name", strings.Replace(minimal, "applications:", "bad/name:", 1), "filters.bad/name"},
		{"invalid credential name", strings.Replace(minimal, "company-app", "App/Key", 1), "sources.company.credential"},
		{"empty owners", strings.Replace(minimal, "[Acme]", "[]", 1), "sources.company.owners"},
		{"empty owner", strings.Replace(minimal, "[Acme]", "['']", 1), "sources.company.owners"},
		{"missing credential", strings.Replace(minimal, "    credential: company-app\n", "", 1), "sources.company.credential"},
		{"unknown source", strings.Replace(minimal, "sources: [company]", "sources: [missing]", 1), "filters.applications.sources"},
		{"empty sources", strings.Replace(minimal, "sources: [company]", "sources: []", 1), "filters.applications.sources"},
		{"bad regex", strings.Replace(minimal, "'^service-'", "'['", 1), "filters.applications.include[0].nameRegex"},
		{"empty topics all", strings.Replace(minimal, "all: [GitOps]", "all: []", 1), "filters.applications.include[0].topics.all"},
		{"empty topics any", strings.Replace(minimal, "any: [backend, worker]", "any: []", 1), "filters.applications.include[0].topics.any"},
		{"empty topic", strings.Replace(minimal, "all: [GitOps]", "all: ['']", 1), "filters.applications.include[0].topics.all"},
		{"empty repo list", strings.Replace(minimal, "[Acme/legacy]", "[]", 1), "filters.applications.include[1].repositories"},
		{"repo must be full name", strings.Replace(minimal, "Acme/legacy", "legacy", 1), "filters.applications.include[1].repositories"},
		{"malformed repo", strings.Replace(minimal, "Acme/legacy", "Acme//legacy", 1), "filters.applications.include[1].repositories"},
		{"empty exclusion", strings.Replace(minimal, "- nameRegex: '-sandbox$'", "- {}", 1), "filters.applications.exclude[0]"},
		{"empty include", `version: 1
sources: {company: {owners: [acme], credential: token}}
filters: {applications: {sources: [company], include: []}}`, "filters.applications.include"},
		{"empty rule", `version: 1
sources: {company: {owners: [acme], credential: token}}
filters: {applications: {sources: [company], include: [{}]}}`, "filters.applications.include[0]"},
		{"invalid interval", minimal + "scan: {interval: soon}\n", "scan.interval"},
		{"zero interval", minimal + "scan: {interval: 0s}\n", "scan.interval"},
		{"negative timeout", minimal + "scan: {timeout: -1s}\n", "scan.timeout"},
		{"numeric duration", minimal + "scan: {interval: 123}\n", "scan.interval"},
		{"unknown server setting", minimal + "server: {listen: ':8080', tls: true}\n", "server.tls"},
		{"JSON duplicate", `{"version":1,"version":1,"sources":{},"filters":{}}`, "version"},
		{"JSON unknown field", `{"version":1,"sources":{},"filters":{},"privateKeyFile":"/private/key"}`, "privateKeyFile"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Decode([]byte(tc.input))
			if err == nil {
				t.Fatal("invalid config accepted")
			}
			if !strings.Contains(err.Error(), tc.path) {
				t.Fatalf("error %q lacks field path %q", err, tc.path)
			}
		})
	}
}

func TestJSONRoundTripRetainsCanonicalConfigAndStringDurations(t *testing.T) {
	cfg, err := config.Decode([]byte(minimal + "server: {listen: '127.0.0.1:9000'}\nscan: {interval: 30s, timeout: 15s}\n"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(cfg.Document())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"interval":"30s"`) || !strings.Contains(string(data), `"timeout":"15s"`) {
		t.Fatalf("duration wire format must be strings: %s", data)
	}
	again, err := config.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Document(), again.Document()) {
		t.Fatalf("round trip changed configuration: %#v -> %#v", cfg, again)
	}
}

func TestApplyEnvOverridesOnlyExplicitSupportedSettings(t *testing.T) {
	cfg, err := config.Decode([]byte(minimal))
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"FRD_LISTEN": "127.0.0.1:9999", "FRD_SCAN_INTERVAL": "1m", "FRD_SCAN_TIMEOUT": "20s", "FRD_SOURCES_COMPANY_OWNERS": "evil"}
	runtime, err := config.Prepare(cfg, env, []config.CredentialName{"company-app"})
	if err != nil {
		t.Fatal(err)
	}
	effective := runtime.Effective()
	if effective.Server().Listen != "127.0.0.1:9999" || time.Duration(effective.Scan().Interval) != time.Minute || time.Duration(effective.Scan().Timeout) != 20*time.Second {
		t.Fatalf("overrides = %#v", effective)
	}
	if cfg.Server().Listen != ":8080" || time.Duration(cfg.Scan().Interval) != 10*time.Minute {
		t.Fatal("ApplyEnv mutated loaded config")
	}
	if !reflect.DeepEqual(effective.Sources(), cfg.Sources()) || !reflect.DeepEqual(effective.Document().Filters, cfg.Document().Filters) {
		t.Fatal("env changed discovery rules")
	}
	unchanged, err := config.Prepare(cfg, nil, []config.CredentialName{"company-app"})
	if err != nil || !reflect.DeepEqual(unchanged.Effective().Document(), cfg.Document()) {
		t.Fatalf("absent env must preserve config: %#v, %v", unchanged, err)
	}
}

func TestApplyEnvRejectsInvalidOverrides(t *testing.T) {
	cfg, err := config.Decode([]byte(minimal))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"FRD_SCAN_INTERVAL", "FRD_SCAN_TIMEOUT"} {
		for _, value := range []string{"", "invalid", "0s", "-1m"} {
			t.Run(key+"="+value, func(t *testing.T) {
				_, err := config.Prepare(cfg, map[string]string{key: value}, []config.CredentialName{"company-app"})
				if err == nil {
					t.Fatal("invalid override accepted")
				}
				if !strings.Contains(err.Error(), key) {
					t.Fatalf("error %q lacks env name", err)
				}
			})
		}
	}
}

func TestCredentialReferencesValidatedWithoutReadingSecrets(t *testing.T) {
	cfg, err := config.Decode([]byte(minimal))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.Prepare(cfg, nil, []config.CredentialName{"company-app"}); err != nil {
		t.Fatal(err)
	}
	_, err = config.Prepare(cfg, nil, []config.CredentialName{"other"})
	if err == nil || !strings.Contains(err.Error(), "sources.company.credential") {
		t.Fatalf("unknown credential reference: %v", err)
	}
}

func FuzzDecodeDoesNotPanic(f *testing.F) {
	for _, seed := range []string{minimal, "{}", "", "version: 1\nversion: 1", "[[]]", "{\"version\":1}"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		cfg, err := config.Decode(data)
		if err != nil {
			return
		}
		wire, err := json.Marshal(cfg.Document())
		if err != nil {
			t.Fatal(err)
		}
		again, err := config.Decode(wire)
		if err != nil {
			t.Fatalf("accepted config does not round trip: %v", err)
		}
		if !reflect.DeepEqual(cfg.Document(), again.Document()) {
			t.Fatal("accepted config changes after JSON round trip")
		}
	})
}
