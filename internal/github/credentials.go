package github

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"go.yaml.in/yaml/v3"
)

// CredentialOptions resolves server-side secret references. Nil functions use
// os.LookupEnv and os.ReadFile. Secret values must not be exposed in errors.
type CredentialOptions struct {
	LookupEnv func(string) (string, bool)
	ReadFile  func(string) ([]byte, error)
}

// Credentials is an opaque, immutable registry of loaded server credentials.
type Credentials struct {
	entries map[config.CredentialName]credential
}

type credential struct {
	token          string
	appID          int64
	installationID int64
	key            *rsa.PrivateKey
}

type credentialDocument struct {
	Credentials map[config.CredentialName]credentialDeclaration `yaml:"credentials"`
}

type credentialDeclaration struct {
	Token *struct {
		ValueEnv  string `yaml:"valueEnv"`
		ValueFile string `yaml:"valueFile"`
	} `yaml:"token"`
	App *struct {
		AppID          int64  `yaml:"appId"`
		InstallationID int64  `yaml:"installationId"`
		PrivateKeyEnv  string `yaml:"privateKeyEnv"`
		PrivateKeyFile string `yaml:"privateKeyFile"`
	} `yaml:"githubApp"`
}

var credentialName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

func LoadCredentials(data []byte, options CredentialOptions) (*Credentials, error) {
	// Decoder errors can quote the source document, including accidentally
	// inlined secrets. Never return them to the caller.
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var document credentialDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("credentials: invalid YAML document or unknown/duplicate field")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("credentials: expected exactly one YAML document")
	}
	if len(document.Credentials) == 0 {
		return nil, fmt.Errorf("credentials: at least one credential is required")
	}
	if options.LookupEnv == nil {
		options.LookupEnv = os.LookupEnv
	}
	if options.ReadFile == nil {
		options.ReadFile = os.ReadFile
	}
	registry := &Credentials{entries: make(map[config.CredentialName]credential, len(document.Credentials))}
	names := make([]config.CredentialName, 0, len(document.Credentials))
	for name := range document.Credentials {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if !credentialName.MatchString(string(name)) {
			return nil, fmt.Errorf("credentials: invalid credential name")
		}
		declaration := document.Credentials[name]
		value, err := loadCredential(declaration, options)
		if err != nil {
			return nil, fmt.Errorf("credentials.%s: %w", name, err)
		}
		registry.entries[name] = value
	}
	return registry, nil
}

func loadCredential(declaration credentialDeclaration, options CredentialOptions) (credential, error) {
	if (declaration.Token == nil) == (declaration.App == nil) {
		return credential{}, fmt.Errorf("exactly one of token or githubApp is required")
	}
	if declaration.Token != nil {
		token, err := resolveSecret(declaration.Token.ValueEnv, declaration.Token.ValueFile, options)
		if err != nil {
			return credential{}, fmt.Errorf("token: %w", err)
		}
		if !validToken(token) {
			return credential{}, fmt.Errorf("token: invalid token value")
		}
		return credential{token: token}, nil
	}
	app := declaration.App
	if app.AppID <= 0 || app.InstallationID <= 0 {
		return credential{}, fmt.Errorf("githubApp: appId and installationId must be positive integers")
	}
	encoded, err := resolveSecret(app.PrivateKeyEnv, app.PrivateKeyFile, options)
	if err != nil {
		return credential{}, fmt.Errorf("githubApp: %w", err)
	}
	key, err := parsePrivateKey([]byte(encoded))
	if err != nil {
		return credential{}, err
	}
	return credential{appID: app.AppID, installationID: app.InstallationID, key: key}, nil
}

func resolveSecret(env, file string, options CredentialOptions) (string, error) {
	if (env == "") == (file == "") {
		return "", fmt.Errorf("exactly one env or file reference is required")
	}
	var value string
	if env != "" {
		var found bool
		value, found = options.LookupEnv(env)
		if !found {
			return "", fmt.Errorf("referenced environment variable is unavailable")
		}
	} else {
		data, err := options.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("referenced secret file could not be read")
		}
		value = string(data)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("referenced secret is empty")
	}
	return value, nil
}

func validToken(value string) bool {
	return value != "" && strings.IndexFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || r < '!' || r > '~'
	}) == -1
}

func parsePrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, rest := pem.Decode(data)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 || len(block.Headers) != 0 {
		return nil, fmt.Errorf("githubApp: invalid RSA private key PEM")
	}
	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, _ = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err == nil {
			key, _ = parsed.(*rsa.PrivateKey)
		}
	}
	if key == nil || key.N.BitLen() < 2048 {
		return nil, fmt.Errorf("githubApp: a valid RSA private key of at least 2048 bits is required")
	}
	if err := key.Validate(); err != nil {
		return nil, fmt.Errorf("githubApp: invalid RSA private key")
	}
	// Avoid lazy mutation while several scans sign JWTs concurrently.
	key.Precompute()
	return key, nil
}

func (c *Credentials) Has(name config.CredentialName) bool {
	if c == nil {
		return false
	}
	_, ok := c.entries[name]
	return ok
}

func (c *Credentials) Names() []config.CredentialName {
	if c == nil {
		return nil
	}
	names := make([]config.CredentialName, 0, len(c.entries))
	for name := range c.entries {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
