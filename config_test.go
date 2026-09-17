package cleannacos

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const testDSN = "nacos://127.0.0.1:8848/app.yaml"

func TestReadConfigMergePrecedence(t *testing.T) {
	fake := newFakeClient("host: from-nacos\nport: 1\nuser: nacos-user\n")
	installFakeClient(t, fake)
	t.Setenv("CLEANNACOS_TEST_HOST", "from-env")

	var cfg struct {
		Host  string `yaml:"host" env:"CLEANNACOS_TEST_HOST" env-default:"from-default"`
		Port  int    `yaml:"port" env:"CLEANNACOS_TEST_PORT" env-default:"1234"`
		User  string `yaml:"user" env:"CLEANNACOS_TEST_USER" env-default:"default-user"`
		Pass  string `yaml:"password" env:"CLEANNACOS_TEST_PASS"`
		Level string `yaml:"level" env:"CLEANNACOS_TEST_LEVEL" env-default:"info"`
	}

	if err := ReadConfig(context.Background(), testDSN, &cfg); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}

	if cfg.Host != "from-env" {
		t.Errorf("Host = %q, want %q (environment overrides Nacos)", cfg.Host, "from-env")
	}
	if cfg.Port != 1 {
		t.Errorf("Port = %d, want 1 (Nacos overrides env-default)", cfg.Port)
	}
	if cfg.User != "nacos-user" {
		t.Errorf("User = %q, want %q", cfg.User, "nacos-user")
	}
	if cfg.Pass != "" {
		t.Errorf("Pass = %q, want empty", cfg.Pass)
	}
	if cfg.Level != "info" {
		t.Errorf("Level = %q, want %q (env-default fills the gap)", cfg.Level, "info")
	}
	if !fake.isClosed() {
		t.Error("ReadConfig() did not close the client")
	}
}

func TestReadConfigProviderError(t *testing.T) {
	fake := newFakeClient("")
	fake.getErr = errors.New("boom")
	installFakeClient(t, fake)

	var cfg struct{}
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

func TestReadConfigEmptyContent(t *testing.T) {
	t.Run("empty content is an error by default", func(t *testing.T) {
		fake := newFakeClient("")
		installFakeClient(t, fake)

		var cfg struct {
			Host string `yaml:"host" env:"CLEANNACOS_TEST_EMPTY_HOST" env-default:"fallback"`
		}
		err := ReadConfig(context.Background(), testDSN, &cfg)
		if err == nil {
			t.Fatal("ReadConfig() = nil, want error")
		}
		if !strings.Contains(err.Error(), "empty or not found") {
			t.Fatalf("ReadConfig() error = %q, want an empty config error", err)
		}
		if cfg.Host != "" {
			t.Errorf("Host = %q, want the struct to stay untouched", cfg.Host)
		}
	})

	t.Run("allowEmpty applies env overrides and defaults", func(t *testing.T) {
		fake := newFakeClient("")
		installFakeClient(t, fake)
		t.Setenv("CLEANNACOS_TEST_ALLOW_EMPTY", "from-env")

		var cfg struct {
			Host string `yaml:"host" env:"CLEANNACOS_TEST_ALLOW_EMPTY" env-default:"fallback"`
			Port int    `yaml:"port" env:"CLEANNACOS_TEST_ALLOW_EMPTY_PORT" env-default:"8080"`
		}

		dsn := testDSN + "?allowEmpty=true"
		if err := ReadConfig(context.Background(), dsn, &cfg); err != nil {
			t.Fatalf("ReadConfig() error = %v", err)
		}
		if cfg.Host != "from-env" {
			t.Errorf("Host = %q, want %q", cfg.Host, "from-env")
		}
		if cfg.Port != 8080 {
			t.Errorf("Port = %d, want 8080", cfg.Port)
		}
	})
}

func TestReadConfigRequiredField(t *testing.T) {
	fake := newFakeClient("{}")
	installFakeClient(t, fake)

	var cfg struct {
		Name string `yaml:"name" env:"CLEANNACOS_TEST_REQUIRED" env-required:"true"`
	}

	err := ReadConfig(context.Background(), testDSN, &cfg)
	if err == nil {
		t.Fatal("ReadConfig() = nil, want a required field error")
	}
	if !strings.Contains(err.Error(), "is required") {
		t.Fatalf("ReadConfig() error = %q, want a required field error", err)
	}
}

func TestReadConfigNestedPrefixAndSeparators(t *testing.T) {
	content := strings.Join([]string{
		"db:",
		"  hosts:",
		"    - nacos-a",
		"    - nacos-b",
		"  tags:",
		"    from: nacos",
		"",
	}, "\n")

	fake := newFakeClient(content)
	installFakeClient(t, fake)
	t.Setenv("CLEANNACOS_TEST_APP_HOSTS", "env-a|env-b")
	t.Setenv("CLEANNACOS_TEST_APP_TAGS", "k1:v1;k2:v2")

	var cfg struct {
		DB struct {
			Hosts   []string          `yaml:"hosts" env:"HOSTS" env-separator:"|"`
			Tags    map[string]string `yaml:"tags" env:"TAGS" env-separator:";"`
			TimeOut time.Duration     `yaml:"timeout" env:"TIMEOUT" env-default:"3s"`
		} `yaml:"db" env-prefix:"CLEANNACOS_TEST_APP_"`
	}

	if err := ReadConfig(context.Background(), testDSN, &cfg); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}

	if got, want := strings.Join(cfg.DB.Hosts, ","), "env-a,env-b"; got != want {
		t.Errorf("Hosts = %q, want %q", got, want)
	}
	if got, want := cfg.DB.Tags["k1"], "v1"; got != want {
		t.Errorf("Tags[k1] = %q, want %q", got, want)
	}
	if got, want := cfg.DB.Tags["k2"], "v2"; got != want {
		t.Errorf("Tags[k2] = %q, want %q", got, want)
	}
	if _, ok := cfg.DB.Tags["from"]; ok {
		t.Errorf("Tags = %+v, want the map to be replaced by the environment value", cfg.DB.Tags)
	}
	if cfg.DB.TimeOut != 3*time.Second {
		t.Errorf("TimeOut = %s, want 3s", cfg.DB.TimeOut)
	}
}

func TestReadConfigTimeLayout(t *testing.T) {
	fake := newFakeClient("{}")
	installFakeClient(t, fake)
	t.Setenv("CLEANNACOS_TEST_STARTED", "2026-09-17")

	var cfg struct {
		StartedAt time.Time `yaml:"startedAt" env:"CLEANNACOS_TEST_STARTED" env-layout:"2006-01-02"`
	}

	if err := ReadConfig(context.Background(), testDSN, &cfg); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}

	want := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	if !cfg.StartedAt.Equal(want) {
		t.Fatalf("StartedAt = %s, want %s", cfg.StartedAt, want)
	}
}

func TestReadConfigTargetsDSN(t *testing.T) {
	fake := newFakeClient("{}")
	installFakeClient(t, fake)

	var cfg struct{}
	dsn := "nacos://example.com:8849/group/app.json?group=APP"
	if err := ReadConfig(context.Background(), dsn, &cfg); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}

	if fake.lastGetParam.DataId != "group/app.json" {
		t.Errorf("DataId = %q, want %q", fake.lastGetParam.DataId, "group/app.json")
	}
	if fake.lastGetParam.Group != "APP" {
		t.Errorf("Group = %q, want %q", fake.lastGetParam.Group, "APP")
	}
}

func TestUpdateConfigRefetches(t *testing.T) {
	fake := newFakeClient("host: first\n")
	installFakeClient(t, fake)

	var cfg struct {
		Host string `yaml:"host"`
	}
	if err := ReadConfig(context.Background(), testDSN, &cfg); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}
	if cfg.Host != "first" {
		t.Fatalf("Host = %q, want %q", cfg.Host, "first")
	}

	fake.setContent("host: second\n")
	if err := UpdateConfig(context.Background(), testDSN, &cfg); err != nil {
		t.Fatalf("UpdateConfig() error = %v", err)
	}
	if cfg.Host != "second" {
		t.Fatalf("Host = %q, want %q", cfg.Host, "second")
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

func TestReadConfigCancelledContext(t *testing.T) {
	fake := newFakeClient("{}")
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

func TestEnvHelpers(t *testing.T) {
	t.Setenv("CLEANNACOS_TEST_READ_ENV", "from-env")

	var cfg struct {
		Value string `env:"CLEANNACOS_TEST_READ_ENV"`
	}
	if err := ReadEnv(&cfg); err != nil {
		t.Fatalf("ReadEnv() error = %v", err)
	}
	if cfg.Value != "from-env" {
		t.Fatalf("Value = %q, want %q", cfg.Value, "from-env")
	}
}

func TestUpdateEnvKeepsStaticFields(t *testing.T) {
	t.Setenv("CLEANNACOS_TEST_UPD_STATIC", "static-first")
	t.Setenv("CLEANNACOS_TEST_UPD_VALUE", "value-first")

	var cfg struct {
		Static string `env:"CLEANNACOS_TEST_UPD_STATIC"`
		Value  string `env:"CLEANNACOS_TEST_UPD_VALUE" env-upd:"true"`
	}
	if err := ReadEnv(&cfg); err != nil {
		t.Fatalf("ReadEnv() error = %v", err)
	}

	t.Setenv("CLEANNACOS_TEST_UPD_STATIC", "static-second")
	t.Setenv("CLEANNACOS_TEST_UPD_VALUE", "value-second")

	if err := UpdateEnv(&cfg); err != nil {
		t.Fatalf("UpdateEnv() error = %v", err)
	}
	if cfg.Static != "static-first" {
		t.Errorf("Static = %q, want %q (env-upd is not set)", cfg.Static, "static-first")
	}
	if cfg.Value != "value-second" {
		t.Errorf("Value = %q, want %q (env-upd is set)", cfg.Value, "value-second")
	}
}

func TestGetDescription(t *testing.T) {
	cfg := struct {
		Addr string `env:"CLEANNACOS_TEST_DESC_ADDR" env-description:"listen address" env-default:"localhost:8080"`
		Port int    `env:"CLEANNACOS_TEST_DESC_PORT" env-description:"listen port" env-default:"8080"`
	}{}

	header := "Test config:"
	description, err := GetDescription(&cfg, &header)
	if err != nil {
		t.Fatalf("GetDescription() error = %v", err)
	}

	for _, want := range []string{
		"Test config:",
		"CLEANNACOS_TEST_DESC_ADDR",
		"listen address",
		`"localhost:8080"`,
		"CLEANNACOS_TEST_DESC_PORT",
	} {
		if !strings.Contains(description, want) {
			t.Errorf("description %q does not contain %q", description, want)
		}
	}
}

func TestFUsage(t *testing.T) {
	cfg := struct {
		Addr string `env:"CLEANNACOS_TEST_USAGE_ADDR" env-description:"listen address"`
	}{}

	var buf bytes.Buffer
	FUsage(&buf, &cfg, nil)()

	if !strings.Contains(buf.String(), "CLEANNACOS_TEST_USAGE_ADDR") {
		t.Fatalf("usage output %q does not contain the environment variable name", buf.String())
	}
}
