package cleannacos

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

const testDSN = "nacos://127.0.0.1:8848"

// multiSourceConfig is the config structure used by most tests below.
type multiSourceConfig struct {
	Server struct {
		Addr    string        `yaml:"addr" nacos-default:"localhost:8080" nacos-description:"listen address"`
		Timeout time.Duration `yaml:"timeout" nacos-default:"5s"`
	} `nacos-data-id:"server.yaml"`

	Database struct {
		Host      string            `yaml:"host" nacos-required:"true" nacos-description:"database host"`
		Port      int               `yaml:"port" nacos-default:"5432"`
		Hosts     []string          `yaml:"hosts" nacos-separator:"|" nacos-default:"db-a|db-b"`
		Tags      map[string]string `yaml:"tags" nacos-separator:";" nacos-default:"env:prod;tier:1"`
		StartedAt time.Time         `yaml:"startedAt" nacos-layout:"2006-01-02" nacos-default:"2026-09-17"`
	} `nacos-data-id:"db.yaml"`
}

func TestReadConfigMultiSource(t *testing.T) {
	fake := newFakeClient()
	fake.setContent("server.yaml", "addr: 10.0.0.1:9090\n")
	fake.setContent("db.yaml", "host: db.internal\n")
	installFakeClient(t, fake)

	var cfg multiSourceConfig
	if err := ReadConfig(context.Background(), testDSN, &cfg); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}

	if cfg.Server.Addr != "10.0.0.1:9090" {
		t.Errorf("Server.Addr = %q, want %q", cfg.Server.Addr, "10.0.0.1:9090")
	}
	if cfg.Server.Timeout != 5*time.Second {
		t.Errorf("Server.Timeout = %s, want 5s (default)", cfg.Server.Timeout)
	}
	if cfg.Database.Host != "db.internal" {
		t.Errorf("Database.Host = %q, want %q", cfg.Database.Host, "db.internal")
	}
	if cfg.Database.Port != 5432 {
		t.Errorf("Database.Port = %d, want 5432 (default)", cfg.Database.Port)
	}
	if got, want := strings.Join(cfg.Database.Hosts, ","), "db-a,db-b"; got != want {
		t.Errorf("Database.Hosts = %q, want %q", got, want)
	}
	if cfg.Database.Tags["env"] != "prod" || cfg.Database.Tags["tier"] != "1" {
		t.Errorf("Database.Tags = %+v, want env:prod and tier:1", cfg.Database.Tags)
	}
	want := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	if !cfg.Database.StartedAt.Equal(want) {
		t.Errorf("Database.StartedAt = %s, want %s", cfg.Database.StartedAt, want)
	}

	if got, want := fake.getDataIDs(), []string{"db.yaml", "server.yaml"}; !reflect.DeepEqual(got, want) {
		t.Errorf("fetched dataIds = %v, want %v", got, want)
	}
}

func TestReadConfigDefaultsOnlyForZeroValues(t *testing.T) {
	fake := newFakeClient()
	fake.setContent("server.yaml", "addr: from-nacos\ntimeout: 1s\n")
	fake.setContent("db.yaml", "host: db\n")
	installFakeClient(t, fake)

	var cfg multiSourceConfig
	cfg.Server.Addr = "preset"

	if err := ReadConfig(context.Background(), testDSN, &cfg); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}

	if cfg.Server.Addr != "from-nacos" {
		t.Errorf("Server.Addr = %q, want %q (content wins over the preset)", cfg.Server.Addr, "from-nacos")
	}
	if cfg.Server.Timeout != time.Second {
		t.Errorf("Server.Timeout = %s, want 1s (content wins over the default)", cfg.Server.Timeout)
	}
}

func TestReadConfigNestedDataIDOverrides(t *testing.T) {
	fake := newFakeClient()
	fake.setContent("app.yaml", "host: from-app\nport: 1\n")
	fake.setContent("override.yaml", "port: 2\n")
	installFakeClient(t, fake)

	var cfg struct {
		DB struct {
			Host string `yaml:"host"`
			Port int    `yaml:"port"`
			Pool struct {
				Size int `yaml:"size" nacos-default:"10"`
			} `yaml:"pool" nacos-data-id:"override.yaml"`
		} `yaml:"db" nacos-data-id:"app.yaml"`
	}

	if err := ReadConfig(context.Background(), testDSN, &cfg); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}

	if cfg.DB.Host != "from-app" {
		t.Errorf("DB.Host = %q, want %q", cfg.DB.Host, "from-app")
	}
	if cfg.DB.Port != 1 {
		t.Errorf("DB.Port = %d, want 1", cfg.DB.Port)
	}
	if cfg.DB.Pool.Size != 10 {
		t.Errorf("DB.Pool.Size = %d, want 10 (default of the nested source)", cfg.DB.Pool.Size)
	}
}

func TestReadConfigSourceOverrides(t *testing.T) {
	fake := newFakeClient()
	fake.setContentIn("dev", "DATABASE", "db.yaml", "host: scoped\n")
	installFakeClient(t, fake)

	var cfg struct {
		Database struct {
			Host string `yaml:"host" nacos-required:"true"`
		} `nacos-data-id:"db.yaml" nacos-group:"DATABASE" nacos-namespace:"dev"`
	}

	dsn := testDSN + "?namespace=other&group=OTHER"
	if err := ReadConfig(context.Background(), dsn, &cfg); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}
	if cfg.Database.Host != "scoped" {
		t.Fatalf("Database.Host = %q, want %q", cfg.Database.Host, "scoped")
	}
	if got := fake.lastGet().Group; got != "DATABASE" {
		t.Errorf("group = %q, want %q (nacos-group wins)", got, "DATABASE")
	}
}

func TestReadConfigDeduplicatesSources(t *testing.T) {
	fake := newFakeClient()
	fake.setContent("shared.yaml", "host: shared\n")
	installFakeClient(t, fake)

	var cfg struct {
		First  struct{ Host string } `nacos-data-id:"shared.yaml"`
		Second struct{ Host string } `nacos-data-id:"shared.yaml"`
	}

	if err := ReadConfig(context.Background(), testDSN, &cfg); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}

	if count := fake.countCalls("get"); count != 1 {
		t.Errorf("get calls = %d, want 1 for a dataId used twice", count)
	}
	if cfg.First.Host != "shared" || cfg.Second.Host != "shared" {
		t.Errorf("cfg = %+v, want both fields filled from the shared dataId", cfg)
	}
}

func TestReadConfigUntaggedFieldsAreIgnored(t *testing.T) {
	fake := newFakeClient()
	fake.setContent("app.yaml", "host: from-nacos\n")
	installFakeClient(t, fake)

	var cfg struct {
		Local struct {
			Mode string `nacos-default:"debug"`
		}
		Remote struct {
			Host string `yaml:"host"`
		} `nacos-data-id:"app.yaml"`
	}

	if err := ReadConfig(context.Background(), testDSN, &cfg); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}

	if cfg.Local.Mode != "" {
		t.Errorf("Local.Mode = %q, want it to stay untouched without a dataId", cfg.Local.Mode)
	}
	if cfg.Remote.Host != "from-nacos" {
		t.Errorf("Remote.Host = %q, want %q", cfg.Remote.Host, "from-nacos")
	}
}

func TestReadConfigPointerSubtree(t *testing.T) {
	fake := newFakeClient()
	fake.setContent("db.yaml", "host: db.internal\n")
	installFakeClient(t, fake)

	type dbOptions struct {
		Host string `yaml:"host" nacos-required:"true"`
		Port int    `yaml:"port" nacos-default:"5432"`
	}

	var cfg struct {
		DB *dbOptions `nacos-data-id:"db.yaml"`
	}

	if err := ReadConfig(context.Background(), testDSN, &cfg); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}
	if cfg.DB == nil {
		t.Fatal("DB = nil, want an allocated subtree")
	}
	if cfg.DB.Host != "db.internal" || cfg.DB.Port != 5432 {
		t.Fatalf("DB = %+v, want host db.internal and port 5432", *cfg.DB)
	}
}

func TestReadConfigRequiredInNilPointerSubtree(t *testing.T) {
	fake := newFakeClient()
	installFakeClient(t, fake)

	type dbOptions struct {
		Host string `yaml:"host" nacos-required:"true"`
	}

	var cfg struct {
		DB *dbOptions `nacos-data-id:"db.yaml"`
	}

	err := ReadConfig(context.Background(), testDSN, &cfg)
	if err == nil {
		t.Fatal("ReadConfig() = nil, want a required field error")
	}
	if !strings.Contains(err.Error(), "is required") {
		t.Fatalf("ReadConfig() error = %q, want a required field error", err)
	}
	if cfg.DB != nil {
		t.Errorf("DB = %+v, want it to stay nil", cfg.DB)
	}
}

func TestReadConfigNestedDataIDInsideUntaggedField(t *testing.T) {
	fake := newFakeClient()
	fake.setContent("nested.yaml", "host: nested\n")
	installFakeClient(t, fake)

	var cfg struct {
		Local struct {
			Mode string `nacos-default:"debug"`
			Sub  struct {
				Host string `yaml:"host"`
			} `nacos-data-id:"nested.yaml"`
		}
	}

	if err := ReadConfig(context.Background(), testDSN, &cfg); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}

	if cfg.Local.Sub.Host != "nested" {
		t.Errorf("Local.Sub.Host = %q, want %q", cfg.Local.Sub.Host, "nested")
	}
	if cfg.Local.Mode != "" {
		t.Errorf("Local.Mode = %q, want it to stay untouched", cfg.Local.Mode)
	}
}

func TestReadConfigEmptyContentIsAnEmptyDocument(t *testing.T) {
	fake := newFakeClient()
	installFakeClient(t, fake)

	var cfg struct {
		Server struct {
			Addr string `yaml:"addr" nacos-default:"fallback"`
			Port int    `yaml:"port" nacos-default:"8080"`
		} `nacos-data-id:"missing.yaml"`
	}

	if err := ReadConfig(context.Background(), testDSN, &cfg); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}
	if cfg.Server.Addr != "fallback" || cfg.Server.Port != 8080 {
		t.Fatalf("cfg = %+v, want the defaults of an empty document", cfg)
	}
}

func TestReadConfigRequiredField(t *testing.T) {
	fake := newFakeClient()
	fake.setContent("app.yaml", "name: \n")
	installFakeClient(t, fake)

	var cfg struct {
		App struct {
			Name string `yaml:"name" nacos-required:"true"`
		} `nacos-data-id:"app.yaml"`
	}

	err := ReadConfig(context.Background(), testDSN, &cfg)
	if err == nil {
		t.Fatal("ReadConfig() = nil, want a required field error")
	}
	if !strings.Contains(err.Error(), "is required") || !strings.Contains(err.Error(), "app.yaml") {
		t.Fatalf("ReadConfig() error = %q, want a required field error naming the dataId", err)
	}
}

func TestReadConfigRequiredSatisfiedByDefault(t *testing.T) {
	fake := newFakeClient()
	installFakeClient(t, fake)

	var cfg struct {
		App struct {
			Name string `yaml:"name" nacos-required:"true" nacos-default:"fallback"`
		} `nacos-data-id:"app.yaml"`
	}

	if err := ReadConfig(context.Background(), testDSN, &cfg); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}
	if cfg.App.Name != "fallback" {
		t.Fatalf("App.Name = %q, want %q", cfg.App.Name, "fallback")
	}
}

func TestReadConfigCustomSetter(t *testing.T) {
	fake := newFakeClient()
	installFakeClient(t, fake)

	var cfg struct {
		App struct {
			Mode upper `yaml:"mode" nacos-default:"debug"`
		} `nacos-data-id:"app.yaml"`
	}

	if err := ReadConfig(context.Background(), testDSN, &cfg); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}
	if cfg.App.Mode != "DEBUG!" {
		t.Fatalf("App.Mode = %q, want %q", cfg.App.Mode, "DEBUG!")
	}
}

type upper string

func (u *upper) SetValue(value string) error {
	*u = upper(strings.ToUpper(value) + "!")

	return nil
}

func TestReadConfigJSONAndTOML(t *testing.T) {
	fake := newFakeClient()
	fake.setContent("app.json", `{"host":"json-host"}`)
	fake.setContent("app.toml", "port = 7000\n")
	installFakeClient(t, fake)

	var cfg struct {
		JSON struct {
			Host string `json:"host"`
		} `nacos-data-id:"app.json"`
		TOML struct {
			Port int `toml:"port"`
		} `nacos-data-id:"app.toml"`
	}

	if err := ReadConfig(context.Background(), testDSN, &cfg); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}
	if cfg.JSON.Host != "json-host" {
		t.Errorf("JSON.Host = %q, want %q", cfg.JSON.Host, "json-host")
	}
	if cfg.TOML.Port != 7000 {
		t.Errorf("TOML.Port = %d, want 7000", cfg.TOML.Port)
	}
}

func TestReadConfigUnsupportedExtension(t *testing.T) {
	fake := newFakeClient()
	fake.setContent("app.env", "HOST=from-env\n")
	installFakeClient(t, fake)

	var cfg struct {
		App struct {
			Host string `yaml:"host"`
		} `nacos-data-id:"app.env"`
	}

	err := ReadConfig(context.Background(), testDSN, &cfg)
	if err == nil {
		t.Fatal("ReadConfig() = nil, want an unsupported extension error")
	}
	if !strings.Contains(err.Error(), "unsupported extension") {
		t.Fatalf("ReadConfig() error = %q, want an unsupported extension error", err)
	}
}

func TestReadConfigProviderError(t *testing.T) {
	fake := newFakeClient()
	fake.getErr = errors.New("boom")
	installFakeClient(t, fake)

	var cfg struct {
		App struct {
			Host string `yaml:"host"`
		} `nacos-data-id:"app.yaml"`
	}

	err := ReadConfig(context.Background(), testDSN, &cfg)
	if err == nil {
		t.Fatal("ReadConfig() = nil, want error")
	}
	if !strings.HasPrefix(err.Error(), "cleannacos:") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("ReadConfig() error = %q, want cleannacos: prefix and provider error", err)
	}
	if !fake.isClosed() {
		t.Error("ReadConfig() did not close the client")
	}
}

func TestReadConfigParseErrorNamesTheSource(t *testing.T) {
	fake := newFakeClient()
	fake.setContent("app.yaml", "host: [broken\n")
	installFakeClient(t, fake)

	var cfg struct {
		App struct {
			Host string `yaml:"host"`
		} `nacos-data-id:"app.yaml"`
	}

	err := ReadConfig(context.Background(), testDSN, &cfg)
	if err == nil {
		t.Fatal("ReadConfig() = nil, want a parse error")
	}
	if !strings.Contains(err.Error(), `parse config "app.yaml"`) {
		t.Fatalf("ReadConfig() error = %q, want it to name the source", err)
	}
}

func TestUpdateConfigRefetches(t *testing.T) {
	fake := newFakeClient()
	fake.setContent("app.yaml", "host: first\n")
	installFakeClient(t, fake)

	var cfg struct {
		App struct {
			Host string `yaml:"host"`
		} `nacos-data-id:"app.yaml"`
	}
	if err := ReadConfig(context.Background(), testDSN, &cfg); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}
	if cfg.App.Host != "first" {
		t.Fatalf("App.Host = %q, want %q", cfg.App.Host, "first")
	}

	fake.setContent("app.yaml", "host: second\n")
	if err := UpdateConfig(context.Background(), testDSN, &cfg); err != nil {
		t.Fatalf("UpdateConfig() error = %v", err)
	}
	if cfg.App.Host != "second" {
		t.Fatalf("App.Host = %q, want %q", cfg.App.Host, "second")
	}
}

func TestReadConfigWithoutSourcesSkipsTheClient(t *testing.T) {
	fake := newFakeClient()
	installFakeClient(t, fake)

	var cfg struct {
		Local string `nacos-default:"ignored"`
	}

	if err := ReadConfig(context.Background(), testDSN, &cfg); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}
	if calls := fake.callOrder(); len(calls) != 0 {
		t.Fatalf("client calls = %v, want none", calls)
	}
}

func TestReadConfigInvalidDSN(t *testing.T) {
	var cfg struct{}

	err := ReadConfig(context.Background(), "file://config.yaml", &cfg)
	if err == nil {
		t.Fatal("ReadConfig() = nil, want error")
	}
	if !strings.Contains(err.Error(), "only \"nacos\" is supported") {
		t.Fatalf("ReadConfig() error = %q, want an unsupported scheme error", err)
	}
}

func TestReadConfigRejectsNonPointer(t *testing.T) {
	err := ReadConfig(context.Background(), testDSN, struct{}{})
	if err == nil {
		t.Fatal("ReadConfig() = nil, want error")
	}
	if !strings.Contains(err.Error(), "non-nil pointer") {
		t.Fatalf("ReadConfig() error = %q, want a pointer error", err)
	}
}

func TestReadConfigCancelledContext(t *testing.T) {
	fake := newFakeClient()
	installFakeClient(t, fake)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var cfg struct{}
	err := ReadConfig(ctx, testDSN, &cfg)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ReadConfig() error = %v, want context.Canceled", err)
	}
	if len(fake.callOrder()) != 0 {
		t.Fatalf("client calls = %v, want none", fake.callOrder())
	}
}

func TestGetDescription(t *testing.T) {
	cfg := struct {
		Server struct {
			Addr string `yaml:"addr" nacos-description:"listen address" nacos-default:"localhost:8080"`
		} `nacos-data-id:"server.yaml"`
		Database struct {
			Host string `yaml:"host" nacos-required:"true" nacos-description:"database host"`
		} `nacos-data-id:"db.yaml"`
		Ignored string `nacos-description:"never read"`
	}{}

	description, err := GetDescription(&cfg, nil)
	if err != nil {
		t.Fatalf("GetDescription() error = %v", err)
	}

	for _, want := range []string{
		"Nacos configuration:",
		"server.yaml:addr",
		"listen address",
		`(default "localhost:8080")`,
		"db.yaml:host",
		"database host",
		"(required)",
	} {
		if !strings.Contains(description, want) {
			t.Errorf("description %q does not contain %q", description, want)
		}
	}
	if strings.Contains(description, "never read") {
		t.Errorf("description %q contains an untagged field", description)
	}

	header := "Test config:"
	custom, err := GetDescription(&cfg, &header)
	if err != nil {
		t.Fatalf("GetDescription() error = %v", err)
	}
	if !strings.HasPrefix(custom, "Test config:") {
		t.Errorf("description %q does not start with the custom header", custom)
	}
}

func TestGetDescriptionKeyPathsAreRelativeToTheDataSource(t *testing.T) {
	cfg := struct {
		Server struct {
			Pool struct {
				Size int `yaml:"size" nacos-description:"pool size"`
			} `yaml:"pool"`
		} `nacos-data-id:"server.yaml"`
	}{}

	description, err := GetDescription(&cfg, nil)
	if err != nil {
		t.Fatalf("GetDescription() error = %v", err)
	}
	if !strings.Contains(description, "server.yaml:pool.size") {
		t.Fatalf("description = %q, want the key path relative to the dataId", description)
	}
}

// EmbeddedOptions is embedded to check that inlined fields keep the key path of
// their embedding struct.
type EmbeddedOptions struct {
	Addr string `yaml:"addr" nacos-description:"embedded address"`
}

func TestGetDescriptionInlinesEmbeddedStructs(t *testing.T) {
	cfg := struct {
		Server struct {
			EmbeddedOptions
			Pool struct {
				Size int `yaml:"size" nacos-description:"pool size"`
			} `yaml:"pool"`
		} `nacos-data-id:"server.yaml"`
	}{}

	description, err := GetDescription(&cfg, nil)
	if err != nil {
		t.Fatalf("GetDescription() error = %v", err)
	}
	for _, want := range []string{"server.yaml:addr", "server.yaml:pool.size"} {
		if !strings.Contains(description, want) {
			t.Errorf("description = %q, want it to contain %q", description, want)
		}
	}
}

func TestGetDescriptionIsEmptyWithoutSources(t *testing.T) {
	var cfg struct {
		Local string `nacos-description:"never read"`
	}

	description, err := GetDescription(&cfg, nil)
	if err != nil {
		t.Fatalf("GetDescription() error = %v", err)
	}
	if description != "" {
		t.Fatalf("GetDescription() = %q, want an empty string", description)
	}
}

func TestGetDescriptionRejectsNonPointer(t *testing.T) {
	if _, err := GetDescription(struct{}{}, nil); err == nil {
		t.Fatal("GetDescription() = nil, want error")
	}
}
