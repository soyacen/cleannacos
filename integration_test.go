package cleannacos

import (
	"context"
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

	// The dataIds are part of the struct tags, so unlike earlier versions they
	// cannot be unique per run. Every test cleans up the configs it published.
	integrationServerDataID  = "cleannacos-it-server.yaml"
	integrationDBDataID      = "cleannacos-it-db.yaml"
	integrationWatchDataID   = "cleannacos-it-watch.yaml"
	integrationMissingDataID = "cleannacos-it-missing.yaml"
	integrationInvalidDataID = "cleannacos-it-invalid.yaml"

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

// integrationWatchConfig is the config watched by the watch integration tests.
type integrationWatchConfig struct {
	Server struct {
		Host string `yaml:"host" nacos-default:"fallback"`
	} `nacos-data-id:"cleannacos-it-watch.yaml"`
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

// dsn builds a nacos:// DSN pointing at the test server. The DSN carries no
// dataId: those come from the struct tags.
func (n testNacos) dsn(t *testing.T) string {
	t.Helper()

	endpoint := url.URL{
		Scheme: "nacos",
		Host:   net.JoinHostPort(n.host, strconv.FormatUint(n.port, 10)),
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

// publish writes content for a dataId and deletes it after the test.
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

// delete removes a test config without failing when it does not exist.
func (n testNacos) delete(t *testing.T, client config_client.IConfigClient, dataID string) {
	t.Helper()

	if _, err := client.DeleteConfig(vo.ConfigParam{DataId: dataID, Group: testGroup}); err != nil {
		t.Logf("delete %s: %v", dataID, err)
	}
}

func TestIntegrationReadConfig(t *testing.T) {
	n := newTestNacos(t)
	client := n.publisher(t)
	n.publish(t, client, integrationServerDataID, "addr: 10.0.0.1:9090\n")
	n.publish(t, client, integrationDBDataID, "host: db.internal\n")

	var cfg struct {
		Server struct {
			Addr    string        `yaml:"addr" nacos-required:"true"`
			Timeout time.Duration `yaml:"timeout" nacos-default:"5s"`
		} `nacos-data-id:"cleannacos-it-server.yaml"`
		Database struct {
			Host string `yaml:"host" nacos-required:"true"`
			Port int    `yaml:"port" nacos-default:"5432"`
		} `nacos-data-id:"cleannacos-it-db.yaml"`
	}

	if err := ReadConfig(context.Background(), n.dsn(t), &cfg); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}
	if cfg.Server.Addr != "10.0.0.1:9090" {
		t.Errorf("Server.Addr = %q, want %q", cfg.Server.Addr, "10.0.0.1:9090")
	}
	if cfg.Server.Timeout != 5*time.Second {
		t.Errorf("Server.Timeout = %s, want 5s", cfg.Server.Timeout)
	}
	if cfg.Database.Host != "db.internal" {
		t.Errorf("Database.Host = %q, want %q", cfg.Database.Host, "db.internal")
	}
	if cfg.Database.Port != 5432 {
		t.Errorf("Database.Port = %d, want 5432", cfg.Database.Port)
	}
}

func TestIntegrationWatch(t *testing.T) {
	n := newTestNacos(t)
	client := n.publisher(t)
	n.publish(t, client, integrationWatchDataID, "host: base\n")

	notifications := make(chan *integrationWatchConfig, 8)

	stop, err := Watch(context.Background(), n.dsn(t), func(conf *integrationWatchConfig) {
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
	if baseline.Server.Host != "base" {
		t.Fatalf("baseline Host = %q, want %q", baseline.Server.Host, "base")
	}

	if _, err := client.PublishConfig(vo.ConfigParam{DataId: integrationWatchDataID, Group: testGroup, Content: "host: changed\n"}); err != nil {
		t.Fatalf("publish change: %v", err)
	}

	updated := waitForNotification(t, notifications, pushTimeout)
	if updated.Server.Host != "changed" {
		t.Fatalf("updated Host = %q, want %q", updated.Server.Host, "changed")
	}
	if updated == baseline {
		t.Fatal("Watch reused the baseline object instead of building a new one")
	}
}

func TestIntegrationWatchStopsReceiving(t *testing.T) {
	n := newTestNacos(t)
	client := n.publisher(t)
	n.publish(t, client, integrationWatchDataID, "host: base\n")

	notifications := make(chan *integrationWatchConfig, 8)
	stop, err := Watch(context.Background(), n.dsn(t), func(c *integrationWatchConfig) {
		notifications <- c
	})
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}

	if baseline := waitForNotification(t, notifications, pushTimeout); baseline.Server.Host != "base" {
		t.Fatalf("baseline Host = %q, want %q", baseline.Server.Host, "base")
	}
	if err := stop(context.Background()); err != nil {
		t.Fatalf("stop() error = %v", err)
	}

	if _, err := client.PublishConfig(vo.ConfigParam{DataId: integrationWatchDataID, Group: testGroup, Content: "host: after-stop\n"}); err != nil {
		t.Fatalf("publish change: %v", err)
	}
	assertNoNotification(t, notifications, 10*time.Second)
}

func TestIntegrationMissingConfig(t *testing.T) {
	n := newTestNacos(t)
	client := n.publisher(t)
	n.delete(t, client, integrationMissingDataID)

	var cfg struct {
		Server struct {
			Addr string `yaml:"addr" nacos-default:"fallback"`
			Port int    `yaml:"port" nacos-default:"8080"`
		} `nacos-data-id:"cleannacos-it-missing.yaml"`
	}

	// A missing config is an empty document, so only the defaults are applied.
	if err := ReadConfig(context.Background(), n.dsn(t), &cfg); err != nil {
		t.Fatalf("ReadConfig() error = %v, want nil for a missing config", err)
	}
	if cfg.Server.Addr != "fallback" || cfg.Server.Port != 8080 {
		t.Fatalf("cfg = %+v, want the defaults of an empty document", cfg)
	}

	var required struct {
		Server struct {
			Addr string `yaml:"addr" nacos-required:"true"`
		} `nacos-data-id:"cleannacos-it-missing.yaml"`
	}
	err := ReadConfig(context.Background(), n.dsn(t), &required)
	if err == nil {
		t.Fatal("ReadConfig() = nil, want a required field error")
	}
	if !strings.Contains(err.Error(), "is required") {
		t.Fatalf("ReadConfig() error = %q, want a required field error", err)
	}
}

func TestIntegrationInvalidContent(t *testing.T) {
	n := newTestNacos(t)
	client := n.publisher(t)
	n.publish(t, client, integrationInvalidDataID, "host: [broken\n")

	var cfg struct {
		Server struct {
			Host string `yaml:"host"`
		} `nacos-data-id:"cleannacos-it-invalid.yaml"`
	}

	err := ReadConfig(context.Background(), n.dsn(t), &cfg)
	if err == nil {
		t.Fatal("ReadConfig() = nil, want a parse error")
	}
	if !strings.Contains(err.Error(), "cleannacos: parse") {
		t.Fatalf("ReadConfig() error = %q, want a cleannacos parse error", err)
	}
}
