// Command simple reads three Nacos configs into one structure and prints the
// supported configuration keys. It uses every tag this package understands:
//
//	nacos-data-id     the dataId a field subtree is read from (any nesting level)
//	nacos-group       group override of a field subtree
//	nacos-namespace   namespace override of a field subtree
//	nacos-default     value used when the field is still at its zero value
//	nacos-required    the field must hold a value once defaults were applied
//	nacos-description description shown by GetDescription
//	nacos-layout      layout used to parse a time.Time default
//	nacos-separator   separator used to parse a slice or map default
//
// The configs the structure refers to:
//
//	server.yaml  in the DEFAULT_GROUP of the public namespace
//
//		addr: 0.0.0.0:8080
//		debug: true
//
//	db.yaml      in the DATABASE group of the public namespace
//
//		host: db.internal
//		password: secret
//
//	redis.yaml   in the public group of the middleware namespace
//
//		addr: redis.internal:6379
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/soyacen/cleannacos"
)

// ServerOptions comes from the server.yaml dataId of the DSN group.
type ServerOptions struct {
	Addr         string        `yaml:"addr" nacos-required:"true" nacos-description:"listen address"`
	Debug        bool          `yaml:"debug" nacos-default:"false" nacos-description:"enable debug logging"`
	Timeout      time.Duration `yaml:"timeout" nacos-default:"5s" nacos-description:"request timeout"`
	VirtualHosts []string      `yaml:"virtualHosts" nacos-separator:"|" nacos-default:"api.local|ops.local" nacos-description:"accepted host names"`
	StartedAt    time.Time     `yaml:"startedAt" nacos-layout:"2006-01-02" nacos-default:"2026-09-17" nacos-description:"release date"`
}

// DatabaseOptions comes from the db.yaml dataId of the DATABASE group.
type DatabaseOptions struct {
	Host     string            `yaml:"host" nacos-required:"true" nacos-description:"database host"`
	Port     int               `yaml:"port" nacos-default:"5432" nacos-description:"database port"`
	User     string            `yaml:"user" nacos-default:"postgres" nacos-description:"database user"`
	Password string            `yaml:"password" nacos-required:"true" nacos-description:"database password"`
	Tags     map[string]string `yaml:"tags" nacos-separator:";" nacos-default:"tier:core;owner:platform" nacos-description:"database labels"`
}

// RedisOptions comes from the redis.yaml dataId of the middleware namespace.
type RedisOptions struct {
	Addr     string `yaml:"addr" nacos-default:"127.0.0.1:6379" nacos-description:"redis address"`
	Password string `yaml:"password" nacos-description:"redis password"`
	DB       int    `yaml:"db" nacos-default:"0" nacos-description:"redis database index"`
}

// Config declares which Nacos configs make up the application configuration.
// The DSN only carries the server address, so every subtree states its own
// dataId.
type Config struct {
	Server   ServerOptions   `nacos-data-id:"server.yaml"`
	Database DatabaseOptions `nacos-data-id:"db.yaml" nacos-group:"DATABASE"`
	Redis    RedisOptions    `nacos-data-id:"redis.yaml" nacos-namespace:"middleware"`
}

func main() {
	dsn := os.Getenv("CLEANNACOS_DSN")
	if dsn == "" {
		dsn = "nacos://127.0.0.1:8848?group=DEFAULT_GROUP"
	}

	// Nacos content -> nacos-default -> nacos-required.
	var cfg Config
	if err := cleannacos.ReadConfig(context.Background(), dsn, &cfg); err != nil {
		log.Fatalf("read config: %v", err)
	}

	fmt.Printf("config: %+v\n\n", cfg)

	description, err := cleannacos.GetDescription(&cfg, nil)
	if err != nil {
		log.Fatalf("describe config: %v", err)
	}
	fmt.Println(description)
}
