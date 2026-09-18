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

// newClient builds the config client for a DSN and namespace. It is a variable
// so tests can inject a fake client.
var newClient = func(d *dsn, namespace string) (configClient, error) {
	return newNacosClient(d, namespace)
}

// newNacosClient creates a Nacos config client for one namespace. The rest of
// the connection settings come from the DSN.
func newNacosClient(d *dsn, namespace string) (configClient, error) {
	options := []constant.ClientOption{
		constant.WithNamespaceId(namespace),
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
		return nil, fmt.Errorf("cleannacos: create nacos client for %s and namespace %q: %w", d.serverIdent(), namespace, err)
	}

	return client, nil
}

// closeClient releases the client resources when the client supports it.
func closeClient(c configClient) {
	if cl, ok := c.(closer); ok {
		cl.CloseClient()
	}
}

// clientPool keeps one Nacos client per namespace, because the namespace is a
// client level setting of the Nacos SDK while nacos-namespace is a field level
// tag.
type clientPool struct {
	dsn     *dsn
	factory func(*dsn, string) (configClient, error)
	clients map[string]configClient
}

// newClientPool creates a pool that builds clients lazily.
func newClientPool(d *dsn) *clientPool {
	return &clientPool{
		dsn:     d,
		factory: newClient,
		clients: make(map[string]configClient),
	}
}

// client returns the client of a namespace, creating it on first use.
func (p *clientPool) client(namespace string) (configClient, error) {
	if client, ok := p.clients[namespace]; ok {
		return client, nil
	}

	client, err := p.factory(p.dsn, namespace)
	if err != nil {
		return nil, err
	}
	p.clients[namespace] = client

	return client, nil
}

// configParam addresses one source inside a namespace.
func (s source) configParam() vo.ConfigParam {
	return vo.ConfigParam{DataId: s.dataID, Group: s.group}
}

// closeAll releases every client created by the pool.
func (p *clientPool) closeAll() {
	for _, client := range p.clients {
		closeClient(client)
	}
}
