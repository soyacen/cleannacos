package cleannacos

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/config_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

const (
	testAddrEnv     = "CLEANNACOS_TEST_ADDR"
	testUsernameEnv = "CLEANNACOS_TEST_USERNAME"
	testPasswordEnv = "CLEANNACOS_TEST_PASSWORD"
	testGroup       = "CLEANNACOS"

	// pushTimeout is how long a change may take to reach the client.
	pushTimeout = 30 * time.Second
)

// testNacos describes the Nacos instance used by the integration tests.
type testNacos struct {
	host     string
	port     uint64
	username string
	password string
}

// integrationWatchConfig is the config watched by TestIntegrationWatch.
type integrationWatchConfig struct {
	Host string `yaml:"host" env:"CLEANNACOS_TEST_IT_WATCH_HOST" env-default:"fallback"`
}

// newTestNacos skips the test unless CLEANNACOS_TEST_ADDR points to a server.
func newTestNacos(t *testing.T) testNacos {
	t.Helper()

	addr := os.Getenv(testAddrEnv)
	if addr == "" {
		t.Skipf("%s is not set, skipping the Nacos integration test", testAddrEnv)
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("invalid %s %q: %v", testAddrEnv, addr, err)
	}
	value, err := strconv.ParseUint(port, 10, 64)
	if err != nil {
		t.Fatalf("invalid port in %s %q: %v", testAddrEnv, addr, err)
	}

	return testNacos{
		host:     host,
		port:     value,
		username: os.Getenv(testUsernameEnv),
		password: os.Getenv(testPasswordEnv),
	}
}

// dsn builds a nacos:// DSN pointing at the test server.
func (n testNacos) dsn(t *testing.T, dataID string, extra map[string]string) string {
	t.Helper()

	endpoint := url.URL{
		Scheme: "nacos",
		Host:   net.JoinHostPort(n.host, strconv.FormatUint(n.port, 10)),
		Path:   "/" + dataID,
	}
	if n.username != "" {
		endpoint.User = url.UserPassword(n.username, n.password)
	}

	query := url.Values{}
	query.Set("group", testGroup)
	query.Set("logDir", filepath.Join(t.TempDir(), "log"))
	query.Set("cacheDir", filepath.Join(t.TempDir(), "cache"))
	query.Set("logLevel", "error")
	query.Set("notLoadCacheAtStart", "true")
	for key, value := range extra {
		query.Set(key, value)
	}
	endpoint.RawQuery = query.Encode()

	return endpoint.String()
}

// publisher returns a Nacos client used to publish and delete test configs.
func (n testNacos) publisher(t *testing.T) config_client.IConfigClient {
	t.Helper()

	options := []constant.ClientOption{
		constant.WithTimeoutMs(uint64((10 * time.Second).Milliseconds())),
		constant.WithLogDir(filepath.Join(t.TempDir(), "log")),
		constant.WithCacheDir(filepath.Join(t.TempDir(), "cache")),
		constant.WithLogLevel("error"),
		constant.WithNotLoadCacheAtStart(true),
	}
	if n.username != "" {
		options = append(options, constant.WithUsername(n.username), constant.WithPassword(n.password))
	}

	client, err := clients.NewConfigClient(vo.NacosClientParam{
		ClientConfig:  constant.NewClientConfig(options...),
		ServerConfigs: []constant.ServerConfig{*constant.NewServerConfig(n.host, n.port)},
	})
	if err != nil {
		t.Fatalf("create nacos client: %v", err)
	}
	t.Cleanup(client.CloseClient)

	return client
}

// publish writes content for a fresh dataId and deletes it after the test.
func (n testNacos) publish(t *testing.T, client config_client.IConfigClient, dataID, content string) {
	t.Helper()

	if _, err := client.PublishConfig(vo.ConfigParam{DataId: dataID, Group: testGroup, Content: content}); err != nil {
		t.Fatalf("publish %s: %v", dataID, err)
	}
	t.Cleanup(func() {
		if _, err := client.DeleteConfig(vo.ConfigParam{DataId: dataID, Group: testGroup}); err != nil {
			t.Logf("delete %s: %v", dataID, err)
		}
	})
}

// testDataID returns a dataId that does not collide with other runs.
func testDataID(t *testing.T, suffix string) string {
	t.Helper()

	return fmt.Sprintf("cleannacos-it-%d-%s", time.Now().UnixNano(), suffix)
}

func TestIntegrationReadConfig(t *testing.T) {
	n := newTestNacos(t)
	client := n.publisher(t)
	dataID := testDataID(t, "read.yaml")
	n.publish(t, client, dataID, "host: from-nacos\nport: 1\n")

	t.Setenv("CLEANNACOS_TEST_IT_HOST", "from-env")

	var cfg struct {
		Host string `yaml:"host" env:"CLEANNACOS_TEST_IT_HOST" env-default:"fallback"`
		Port int    `yaml:"port" env:"CLEANNACOS_TEST_IT_PORT" env-default:"9"`
		Tag  string `yaml:"tag" env:"CLEANNACOS_TEST_IT_TAG" env-default:"default-tag"`
	}

	if err := ReadConfig(context.Background(), n.dsn(t, dataID, nil), &cfg); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}
	if cfg.Host != "from-env" {
		t.Errorf("Host = %q, want %q", cfg.Host, "from-env")
	}
	if cfg.Port != 1 {
		t.Errorf("Port = %d, want 1", cfg.Port)
	}
	if cfg.Tag != "default-tag" {
		t.Errorf("Tag = %q, want %q", cfg.Tag, "default-tag")
	}
}

func TestIntegrationWatch(t *testing.T) {
	n := newTestNacos(t)
	client := n.publisher(t)
	dataID := testDataID(t, "watch.yaml")
	n.publish(t, client, dataID, "host: base\n")

	notifications := make(chan *integrationWatchConfig, 8)

	stop, err := Watch(context.Background(), n.dsn(t, dataID, nil), func(conf *integrationWatchConfig) {
		notifications <- conf
	})
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}
	defer func() {
		if err := stop(context.Background()); err != nil {
			t.Errorf("stop() error = %v", err)
		}
	}()

	baseline := waitForNotification(t, notifications, pushTimeout)
	if baseline.Host != "base" {
		t.Fatalf("baseline Host = %q, want %q", baseline.Host, "base")
	}

	if _, err := client.PublishConfig(vo.ConfigParam{DataId: dataID, Group: testGroup, Content: "host: changed\n"}); err != nil {
		t.Fatalf("publish change: %v", err)
	}

	updated := waitForNotification(t, notifications, pushTimeout)
	if updated.Host != "changed" {
		t.Fatalf("updated Host = %q, want %q", updated.Host, "changed")
	}
	if updated == baseline {
		t.Fatal("Watch reused the baseline object instead of building a new one")
	}
}

func TestIntegrationWatchStopsReceiving(t *testing.T) {
	n := newTestNacos(t)
	client := n.publisher(t)
	dataID := testDataID(t, "stop.yaml")
	n.publish(t, client, dataID, "host: base\n")

	type conf struct {
		Host string `yaml:"host"`
	}

	notifications := make(chan *conf, 8)
	stop, err := Watch(context.Background(), n.dsn(t, dataID, nil), func(c *conf) {
		notifications <- c
	})
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}

	if baseline := waitForNotification(t, notifications, pushTimeout); baseline.Host != "base" {
		t.Fatalf("baseline Host = %q, want %q", baseline.Host, "base")
	}
	if err := stop(context.Background()); err != nil {
		t.Fatalf("stop() error = %v", err)
	}

	if _, err := client.PublishConfig(vo.ConfigParam{DataId: dataID, Group: testGroup, Content: "host: after-stop\n"}); err != nil {
		t.Fatalf("publish change: %v", err)
	}
	assertNoNotification(t, notifications, 10*time.Second)
}

func TestIntegrationMissingConfig(t *testing.T) {
	n := newTestNacos(t)
	dataID := testDataID(t, "missing.yaml")
	t.Setenv("CLEANNACOS_TEST_IT_MISSING_HOST", "from-env")

	var cfg struct {
		Host string `yaml:"host" env:"CLEANNACOS_TEST_IT_MISSING_HOST" env-default:"fallback"`
		Port int    `yaml:"port" env:"CLEANNACOS_TEST_IT_MISSING_PORT" env-default:"8080"`
	}

	err := ReadConfig(context.Background(), n.dsn(t, dataID, nil), &cfg)
	if err == nil {
		t.Fatal("ReadConfig() = nil, want error for a missing config")
	}
	if !strings.Contains(err.Error(), "empty or not found") {
		t.Fatalf("ReadConfig() error = %q, want an empty config error", err)
	}
	t.Logf("missing config error: %v", err)

	// allowEmpty treats the missing content as an empty config, so only the
	// environment overrides and the env-default values are applied.
	err = ReadConfig(context.Background(), n.dsn(t, dataID, map[string]string{"allowEmpty": "true"}), &cfg)
	if err != nil {
		t.Fatalf("ReadConfig(allowEmpty) error = %v, want nil", err)
	}
	if cfg.Host != "from-env" {
		t.Errorf("Host = %q, want %q", cfg.Host, "from-env")
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
}

func TestIntegrationInvalidContent(t *testing.T) {
	n := newTestNacos(t)
	client := n.publisher(t)
	dataID := testDataID(t, "invalid.yaml")
	n.publish(t, client, dataID, "host: [broken\n")

	var cfg struct {
		Host string `yaml:"host"`
	}

	err := ReadConfig(context.Background(), n.dsn(t, dataID, nil), &cfg)
	if err == nil {
		t.Fatal("ReadConfig() = nil, want a parse error")
	}
	if !strings.Contains(err.Error(), "cleannacos: parse") {
		t.Fatalf("ReadConfig() error = %q, want a cleannacos parse error", err)
	}
}
