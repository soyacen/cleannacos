// Command simple reads a Nacos config with cleanenv semantics and prints the
// supported environment variables.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/soyacen/cleannacos"
)

// Config is the configuration structure, tagged exactly like a cleanenv one.
type Config struct {
	Addr    string `yaml:"addr" env:"ADDR" env-default:"localhost:8080" env-description:"listen address"`
	Port    int    `yaml:"port" env:"PORT" env-default:"8080" env-description:"listen port"`
	Debug   bool   `yaml:"debug" env:"DEBUG" env-default:"false" env-description:"enable debug logging"`
	Timeout string `yaml:"timeout" env:"TIMEOUT" env-default:"5s" env-description:"request timeout"`
}

func main() {
	dsn := os.Getenv("CLEANNACOS_DSN")
	if dsn == "" {
		dsn = "nacos://127.0.0.1:8848/config.yaml?group=DEFAULT_GROUP"
	}

	// 读取 Nacos 内容 → 环境变量覆盖 → env-default
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
