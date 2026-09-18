// Package cleannacos reads configuration from Nacos into Go structures.
//
// The package only reads Nacos: there is no environment variable layer and no
// local file layer. The DSN describes the Nacos server, and every field of the
// config structure declares where its value comes from through nacos-* struct
// tags:
//
//	type Config struct {
//		Server struct {
//			Addr string `yaml:"addr" nacos-default:"localhost:8080" nacos-description:"listen address"`
//		} `nacos-data-id:"server.yaml"`
//		Database struct {
//			Hosts []string `yaml:"hosts" nacos-separator:"," nacos-default:"db-a,db-b"`
//		} `nacos-data-id:"db.yaml" nacos-group:"DATABASE"`
//	}
//
//	var cfg Config
//
//	err := cleannacos.ReadConfig(ctx, "nacos://127.0.0.1:8848?group=DEFAULT_GROUP", &cfg)
//
// The DSN must not carry a path:
//
//	nacos://user:pass@host:8848?namespace=ns&group=g&timeoutMs=5000
//
// The extension of a dataId selects the parser: .yaml/.yml, .json or .toml.
//
// # Merging
//
// Sources are parsed from the outside in, so a nested nacos-data-id overrides
// the values of its enclosing document. Afterwards every field that still
// holds its zero value receives its nacos-default, and fields marked with
// nacos-required must hold a value. Data sources are declared per field and
// may be placed at any nesting level; a field without a reachable dataId is
// left untouched.
//
// # Watching for changes
//
// Watch hands a freshly merged *T to the callback and serializes every
// callback, so the callback can simply swap an atomic pointer:
//
//	var current atomic.Pointer[Config]
//
//	stop, err := cleannacos.Watch(ctx, dsn, func(conf *Config) {
//		current.Store(conf)
//	})
//	if err != nil {
//		return err
//	}
//	defer func() { _ = stop(ctx) }()
//
//	cfg := current.Load()
package cleannacos
