// Package cleannacos reads configuration from Nacos with the cleanenv API.
//
// It mirrors github.com/ilyakaznacheev/cleanenv and replaces the local file
// source with a Nacos config source, so migrating only means changing the
// import path and passing a DSN:
//
//	// before
//	err := cleanenv.ReadConfig("config.yml", &cfg)
//
//	// after
//	err := cleannacos.ReadConfig(ctx, "nacos://127.0.0.1:8848/config.yml?group=DEFAULT_GROUP", &cfg)
//
// Parsing, environment variable overrides, env-default values and the
// description helpers are delegated to cleanenv; this package only fetches the
// content, orders the merge (Nacos content -> environment variables ->
// env-default) and adds change watching.
//
// The DSN carries the connection parameters and always addresses one config:
//
//	nacos://user:pass@host:8848/dataId.yaml?namespace=ns&group=g&timeoutMs=5000
//
// The dataId extension selects the parser: .yaml/.yml, .json, .toml, .env or
// .edn.
//
// # Watching for changes
//
// Watch hands a freshly merged *T to the callback and serializes every
// callback, so the callback can simply swap an atomic pointer:
//
//	type Config struct {
//		Addr string `yaml:"addr" env:"ADDR" env-default:"localhost:8080"`
//	}
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
