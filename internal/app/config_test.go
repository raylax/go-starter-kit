package app

import "testing"

func TestConfigurationBoundaries(t *testing.T) {
	base := map[string]string{
		"DATABASE_URL":  "postgres://user:password@localhost/test",
		"AUTH_ISSUER":   "https://identity.example.com/",
		"AUTH_AUDIENCE": "starter-api",
		"AUTH_JWKS_URL": "https://identity.example.com/jwks",
	}
	for _, tc := range []struct {
		name   string
		values map[string]string
		valid  bool
	}{
		{"production defaults", nil, true},
		{"production dev auth rejected", map[string]string{"AUTH_MODE": "dev", "DEV_AUTH_TOKEN": "local-development-token", "DEV_AUTH_SUBJECT": "demo"}, false},
		{"explicit development", map[string]string{"APP_ENV": "development", "AUTH_MODE": "dev", "DEV_AUTH_TOKEN": "local-development-token", "DEV_AUTH_SUBJECT": "demo"}, true},
		{"unknown environment", map[string]string{"APP_ENV": "prodution"}, false},
		{"missing database", map[string]string{"DATABASE_URL": ""}, false},
		{"invalid pool", map[string]string{"DB_MAX_CONNS": "0"}, false},
		{"invalid boolean", map[string]string{"DOCS_ENABLED": "yes"}, false},
		{"negative timeout", map[string]string{"REQUEST_TIMEOUT": "-1s"}, false},
		{"missing audience", map[string]string{"AUTH_AUDIENCE": ""}, false},
		{"insecure JWKS", map[string]string{"AUTH_JWKS_URL": "http://identity.example.com/jwks"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lookup := func(key string) (string, bool) {
				if value, ok := tc.values[key]; ok {
					return value, true
				}
				value, ok := base[key]
				return value, ok
			}
			cfg, err := Parse(lookup)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
			if tc.name == "production defaults" && (cfg.DocsEnabled || cfg.AuthMode != "jwt") {
				t.Fatal("unsafe production defaults")
			}
		})
	}
}
