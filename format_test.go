package cleannacos

import (
	"os"
	"testing"
	"time"
)

func TestResolveFormat(t *testing.T) {
	tests := []struct {
		dataID  string
		want    format
		wantErr bool
	}{
		{dataID: "config.yaml", want: formatYAML},
		{dataID: "config.yml", want: formatYAML},
		{dataID: "config.YAML", want: formatYAML},
		{dataID: "config.json", want: formatJSON},
		{dataID: "config.toml", want: formatTOML},
		{dataID: "config.env", want: formatENV},
		{dataID: "config.edn", want: formatEDN},
		{dataID: "group/config.json", want: formatJSON},
		{dataID: "config", wantErr: true},
		{dataID: "", wantErr: true},
		{dataID: "config.txt", wantErr: true},
		{dataID: "config.yaml.bak", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.dataID, func(t *testing.T) {
			got, err := resolveFormat(tt.dataID)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveFormat(%q) = %q, want error", tt.dataID, got)
				}

				return
			}
			if err != nil {
				t.Fatalf("resolveFormat(%q) unexpected error: %v", tt.dataID, err)
			}
			if got != tt.want {
				t.Fatalf("resolveFormat(%q) = %q, want %q", tt.dataID, got, tt.want)
			}
		})
	}
}

func TestFormatParse(t *testing.T) {
	t.Run("yaml", func(t *testing.T) {
		var cfg struct {
			Host string `yaml:"host"`
		}
		if err := formatYAML.parse("host: example\n", &cfg); err != nil {
			t.Fatalf("parse yaml: %v", err)
		}
		if cfg.Host != "example" {
			t.Fatalf("Host = %q, want %q", cfg.Host, "example")
		}
	})

	t.Run("json", func(t *testing.T) {
		var cfg struct {
			Port int `json:"port"`
		}
		if err := formatJSON.parse(`{"port": 8080}`, &cfg); err != nil {
			t.Fatalf("parse json: %v", err)
		}
		if cfg.Port != 8080 {
			t.Fatalf("Port = %d, want 8080", cfg.Port)
		}
	})

	t.Run("toml", func(t *testing.T) {
		var cfg struct {
			Port int `toml:"port"`
		}
		if err := formatTOML.parse("port = 8080\n", &cfg); err != nil {
			t.Fatalf("parse toml: %v", err)
		}
		if cfg.Port != 8080 {
			t.Fatalf("Port = %d, want 8080", cfg.Port)
		}
	})

	t.Run("edn", func(t *testing.T) {
		var cfg struct {
			Host string `edn:"host"`
		}
		if err := formatEDN.parse(`{:host "example"}`, &cfg); err != nil {
			t.Fatalf("parse edn: %v", err)
		}
		if cfg.Host != "example" {
			t.Fatalf("Host = %q, want %q", cfg.Host, "example")
		}
	})

	t.Run("env", func(t *testing.T) {
		const key = "CLEANNACOS_TEST_ENV_FILE"
		t.Setenv(key, "placeholder")

		if err := formatENV.parse(key+"=from-nacos\n", nil); err != nil {
			t.Fatalf("parse env: %v", err)
		}
		if got := os.Getenv(key); got != "from-nacos" {
			t.Fatalf("%s = %q, want %q", key, got, "from-nacos")
		}
	})
}

func TestFormatParseErrors(t *testing.T) {
	var cfg struct {
		Host string `yaml:"host"`
	}

	if err := formatYAML.parse("host: [broken\n", &cfg); err == nil {
		t.Fatal("parse invalid yaml = nil, want error")
	}
	if err := formatJSON.parse("{", &cfg); err == nil {
		t.Fatal("parse invalid json = nil, want error")
	}
	if err := formatEDN.parse("{", &cfg); err == nil {
		t.Fatal("parse invalid edn = nil, want error")
	}
	if err := formatENV.parse("not a valid .env line\n", nil); err == nil {
		t.Fatal("parse invalid env = nil, want error")
	}
	if err := format("bogus").parse("host: example\n", &cfg); err == nil {
		t.Fatal("parse unknown format = nil, want error")
	}
}

func TestFormatParseKeepsContentParsable(t *testing.T) {
	// A layout based parser keeps working through the format indirection.
	var cfg struct {
		StartedAt time.Time `yaml:"startedAt" env:"CLEANNACOS_TEST_LAYOUT" env-layout:"2006-01-02"`
	}
	t.Setenv("CLEANNACOS_TEST_LAYOUT", "2026-09-17")

	if err := formatYAML.parse("startedAt: 2026-01-02T00:00:00Z\n", &cfg); err != nil {
		t.Fatalf("parse yaml: %v", err)
	}
	if err := ReadEnv(&cfg); err != nil {
		t.Fatalf("ReadEnv() error = %v", err)
	}

	want := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	if !cfg.StartedAt.Equal(want) {
		t.Fatalf("StartedAt = %s, want %s", cfg.StartedAt, want)
	}
}
