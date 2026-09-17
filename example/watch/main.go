// Command watch follows a Nacos config and swaps the current snapshot with an
// atomic pointer, which is safe because cleannacos serializes its callbacks.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/soyacen/cleannacos"
)

// Config is the watched configuration structure.
type Config struct {
	Addr  string `yaml:"addr" env:"ADDR" env-default:"localhost:8080" env-description:"listen address"`
	Debug bool   `yaml:"debug" env:"DEBUG" env-default:"false" env-description:"enable debug logging"`
}

func main() {
	dsn := os.Getenv("CLEANNACOS_DSN")
	if dsn == "" {
		dsn = "nacos://127.0.0.1:8848/config.yaml?group=DEFAULT_GROUP"
	}

	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	// current always holds the latest *Config handed over by the watch.
	var current atomic.Pointer[Config]

	stop, err := cleannacos.Watch(ctx, dsn, func(conf *Config) {
		current.Store(conf)
		log.Printf("config updated: %+v", conf)
	})
	if err != nil {
		log.Fatalf("watch config: %v", err)
	}
	defer func() {
		if err := stop(context.Background()); err != nil {
			log.Printf("stop watch: %v", err)
		}
	}()

	// Watch already delivered the baseline snapshot synchronously.
	if conf := current.Load(); conf != nil {
		fmt.Printf("baseline: %+v\n", conf)
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("stopping")

			return
		case <-ticker.C:
			if conf := current.Load(); conf != nil {
				fmt.Printf("current: %+v\n", conf)
			}
		}
	}
}
