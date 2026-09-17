package cleannacos

import (
	"fmt"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

// configClient is the narrow slice of config_client.IConfigClient this package
// depends on. It is satisfied by the real Nacos client and by test fakes.
type configClient interface {
	GetConfig(param vo.ConfigParam) (string, error)
	ListenConfig(param vo.ConfigParam) error
	CancelListenConfig(param vo.ConfigParam) error
}

// closer is implemented by clients that can release their resources.
type closer interface {
	CloseClient()
}

// newClient builds the config client for a DSN. It is a variable so tests can
// inject a fake client.
var newClient = func(d *dsn) (configClient, error) {
	return newNacosClient(d)
}

// newNacosClient creates a Nacos config client from the parsed DSN.
func newNacosClient(d *dsn) (configClient, error) {
	options := []constant.ClientOption{
		constant.WithNamespaceId(d.namespace),
		constant.WithLogDir(d.logDir),
		constant.WithCacheDir(d.cacheDir),
	}
	if d.username != "" {
		options = append(options, constant.WithUsername(d.username))
	}
	if d.password != "" {
		options = append(options, constant.WithPassword(d.password))
	}
	if d.timeoutMs != nil {
		options = append(options, constant.WithTimeoutMs(*d.timeoutMs))
	}
	if d.logLevel != nil {
		options = append(options, constant.WithLogLevel(*d.logLevel))
	}
	if d.appName != nil {
		options = append(options, constant.WithAppName(*d.appName))
	}
	if d.notLoadCacheAtStart != nil {
		options = append(options, constant.WithNotLoadCacheAtStart(*d.notLoadCacheAtStart))
	}

	client, err := clients.NewConfigClient(vo.NacosClientParam{
		ClientConfig:  constant.NewClientConfig(options...),
		ServerConfigs: []constant.ServerConfig{*constant.NewServerConfig(d.host, d.port)},
	})
	if err != nil {
		return nil, fmt.Errorf("cleannacos: create nacos client for %s: %w", d.ident(), err)
	}

	return client, nil
}

// closeClient releases the client resources when the client supports it.
func closeClient(c configClient) {
	if cl, ok := c.(closer); ok {
		cl.CloseClient()
	}
}

// configParam addresses the config described by the DSN.
func (d *dsn) configParam() vo.ConfigParam {
	return vo.ConfigParam{DataId: d.dataID, Group: d.group}
}
