package cleannacos

import (
	"strings"
	"testing"
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
		{dataID: "group/config.json", want: formatJSON},
		{dataID: "config.env", wantErr: true},
		{dataID: "config.edn", wantErr: true},
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
	if err := formatTOML.parse("host = [broken\n", &cfg); err == nil {
		t.Fatal("parse invalid toml = nil, want error")
	}
	if err := format("bogus").parse("host: example\n", &cfg); err == nil {
		t.Fatal("parse unknown format = nil, want error")
	}
}

func TestFormatRemoved(t *testing.T) {
	for _, dataID := range []string{"config.env", "config.edn"} {
		err := func() error {
			_, err := resolveFormat(dataID)

			return err
		}()
		if err == nil {
			t.Fatalf("resolveFormat(%q) = nil, want error", dataID)
		}
		if !strings.Contains(err.Error(), "unsupported extension") {
			t.Fatalf("resolveFormat(%q) error = %q, want an unsupported extension error", dataID, err)
		}
	}
}

func TestParseHelpers(t *testing.T) {
	var cfg struct {
		Host string `yaml:"host" json:"host"`
		Port int    `yaml:"port" json:"port"`
	}

	if err := ParseYAML(strings.NewReader("host: example\nport: 8080\n"), &cfg); err != nil {
		t.Fatalf("ParseYAML() error = %v", err)
	}
	if cfg.Host != "example" || cfg.Port != 8080 {
		t.Fatalf("cfg = %+v, want host example and port 8080", cfg)
	}

	var jsonCfg struct {
		Host string `json:"host"`
	}
	if err := ParseJSON(strings.NewReader(`{"host":"example"}`), &jsonCfg); err != nil {
		t.Fatalf("ParseJSON() error = %v", err)
	}
	if jsonCfg.Host != "example" {
		t.Fatalf("Host = %q, want %q", jsonCfg.Host, "example")
	}

	var tomlCfg struct {
		Port int `toml:"port"`
	}
	if err := ParseTOML(strings.NewReader("port = 8080\n"), &tomlCfg); err != nil {
		t.Fatalf("ParseTOML() error = %v", err)
	}
	if tomlCfg.Port != 8080 {
		t.Fatalf("Port = %d, want 8080", tomlCfg.Port)
	}
}
