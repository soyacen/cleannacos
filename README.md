# cleannacos - cleanenv 风格的 Nacos 配置读取库

`cleannacos` 把 [cleanenv](https://github.com/ilyakaznacheev/cleanenv) 的「本地文件源」替换为「Nacos 配置源」，解析、环境变量覆盖、默认值、描述生成全部复用 upstream cleanenv，本库只负责取数、合并编排与变更监听。

迁移成本的目标是「只改 import 路径」：

```go
// 之前：cleanenv
err := cleanenv.ReadConfig("config.yaml", &cfg)

// 之后：cleannacos
err := cleannacos.ReadConfig(ctx, "nacos://127.0.0.1:8848/config.yaml?group=DEFAULT_GROUP", &cfg)
```

原有的 struct tag 全部通用：`env` / `env-default` / `env-upd` / `env-description` / `env-required` / `env-prefix` / `env-separator` / `env-layout`。

## 特性

- **API 对齐**：`ReadConfig` / `UpdateConfig` / `ReadEnv` / `UpdateEnv` / `GetDescription` / `Usage` / `FUsage` 与 cleanenv 同名同签名。
- **语义一致**：合并顺序固定为 **Nacos 内容 → 环境变量覆盖 → env-default**，与 cleanenv 的 file → env → default 一一对应；`env-required`、`env-prefix`、`env-separator`、`env-layout` 等语义直接复用 cleanenv。
- **多格式**：`.yaml` / `.yml` / `.json` / `.toml` / `.env` / `.edn`，由 dataId 后缀决定解析器。
- **变更监听**：`Watch[T]` 泛型回调，回调串行化，配合 `atomic.Pointer[T]` 即可无数据竞争地热更新。

## 安装

```bash
go get github.com/soyacen/cleannacos
```

## 快速开始

```go
package main

import (
	"context"
	"log"

	"github.com/soyacen/cleannacos"
)

type Config struct {
	Addr  string `yaml:"addr" env:"ADDR" env-default:"localhost:8080" env-description:"监听地址"`
	Port  int    `yaml:"port" env:"PORT" env-default:"8080" env-description:"监听端口"`
	Debug bool   `yaml:"debug" env:"DEBUG" env-default:"false"`
}

func main() {
	var cfg Config

	// 读取 Nacos 内容 → 环境变量覆盖 → env-default
	if err := cleannacos.ReadConfig(context.Background(), "nacos://127.0.0.1:8848/config.yaml?group=DEFAULT_GROUP", &cfg); err != nil {
		log.Fatal(err)
	}

	// 打印支持的变量、说明与默认值
	description, err := cleannacos.GetDescription(&cfg, nil)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(description)

	// 手动重新拉取（无监听器的刷新场景）
	if err := cleannacos.UpdateConfig(context.Background(), "nacos://127.0.0.1:8848/config.yaml?group=DEFAULT_GROUP", &cfg); err != nil {
		log.Fatal(err)
	}
}
```

可运行示例见 [example/simple](example/simple) 与 [example/watch](example/watch)。

## DSN 规范

```
nacos://user:pass@host:8848/dataId.yaml?namespace=ns&group=g&timeoutMs=5000&logDir=/tmp/nacos/log&cacheDir=/tmp/nacos/cache&logLevel=debug&notLoadCacheAtStart=true&appName=xxx&allowEmpty=true
```

- 只支持 `nacos://` scheme 与单台 `host:port`。
- path 只承载 **dataId**，必须带后缀（`.yaml` / `.yml` / `.json` / `.toml` / `.env` / `.edn`，大小写不敏感），后缀决定解析器；缺失或未知后缀直接报错。
- dataId 可以包含 `/`：`nacos://host:8848/group/app.yaml` 对应的 dataId 是 `group/app.yaml`。

| query 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `namespace` | `""` | Nacos 命名空间，空串即 public |
| `group` | `DEFAULT_GROUP` | 配置分组 |
| `timeoutMs` | SDK 默认 | Nacos 客户端超时（毫秒） |
| `logDir` | `/tmp/nacos/log` | Nacos SDK 日志目录 |
| `cacheDir` | `/tmp/nacos/cache` | Nacos SDK 本地缓存目录 |
| `logLevel` | SDK 默认 | Nacos SDK 日志级别 |
| `notLoadCacheAtStart` | SDK 默认 | 启动时不加载本地缓存 |
| `appName` | SDK 默认 | Nacos 客户端 appName |
| `allowEmpty` | `false` | 内容为空时是否按空配置处理 |

用户名和密码通过 DSN 的 userinfo 传递（`nacos://admin:nacos@host:8848/...`）。已知参数的非法值（非数字 `timeoutMs`、非布尔 `notLoadCacheAtStart`/`allowEmpty`、非法端口等）会直接报错，未知 query 参数忽略。

取不到内容时（Nacos 返回空串且无错误）按 cleanenv「文件不存在」对齐返回明确错误：

```
cleannacos: config "app.yaml" (group "DEFAULT_GROUP", namespace "") is empty or not found, set allowEmpty=true to read it as an empty config
```

确需空配置时用 `allowEmpty=true` 放开，此时跳过内容解析，只执行环境变量覆盖与 env-default。

## 变更监听

`Watch` 会先注册 Nacos listener，再立即拉取一次基线快照并**同步**回调，因此 `Watch` 返回时回调至少已经被调用过一次；之后的变更以异步方式回调，每次传入全新的 `*T`，并且回调之间串行化：

```go
type Config struct {
	Addr  string `yaml:"addr" env:"ADDR" env-default:"localhost:8080"`
	Debug bool   `yaml:"debug" env:"DEBUG" env-default:"false"`
}

var current atomic.Pointer[Config]

stop, err := cleannacos.Watch(ctx, dsn, func(conf *Config) {
	current.Store(conf) // 回调已串行化，atomic 替换即可
})
if err != nil {
	return err
}
defer func() { _ = stop(ctx) }()

cfg := current.Load() // 此时已是基线快照
```

行为约定：

- 每次变更都重跑完整合并（Nacos → env → default），不区分 `env-upd`；`env-upd` 语义仍由 `UpdateEnv` 单独承担。
- `Watch` 不写调用方任何变量，只通过回调把新的 `*T` 交出去。
- 相同内容去重：只有成功解析的内容才会成为新的基线，因此 `good → 坏内容 → 同一 good` 不会再触发回调。
- 坏内容（解析失败）只走 `errorHandler`，不回调；未提供 `WithErrorHandler` 时默认用 `slog.Error` 输出。
- 取消传给 `Watch` 的 `ctx` 会静默停止；`StopFunc` 幂等，首次调用取消 listener 并关闭客户端。
- 基线拉取失败（网络错误 / dataId 不存在 / 内容解析失败）时，`Watch` 取消 listener、关闭客户端并返回错误。

## API

```go
// Nacos 内容 → env 覆盖 → env-default，语义与 cleanenv.ReadConfig 一致
func ReadConfig(ctx context.Context, dsn string, cfg interface{}) error

// 手动重新拉取并全量重合并（无监听器的刷新场景）
func UpdateConfig(ctx context.Context, dsn string, cfg interface{}) error

// 与 cleanenv 完全同名同签名，直接委托
func ReadEnv(cfg interface{}) error
func UpdateEnv(cfg interface{}) error
func GetDescription(cfg interface{}, headerText *string) (string, error)
func Usage(cfg interface{}, headerText *string, usageFuncs ...func()) func()
func FUsage(w io.Writer, cfg interface{}, headerText *string, usageFuncs ...func()) func()

// 变更监听：泛型回调「新的 *T」
type StopFunc func(ctx context.Context) error
type ErrFunc func(err error)
type WatchOption func(*watchOptions)

func WithErrorHandler(errFunc ErrFunc) WatchOption
func Watch[T any](ctx context.Context, dsn string, notify func(conf *T), options ...WatchOption) (StopFunc, error)
```

同时以别名/常量形式再导出 cleanenv 的既有面：`Setter`、`Updater`、`DefaultSeparator`、`TagEnv`、`TagEnvDefault`、`TagEnvUpd`、`TagEnvRequired`、`TagEnvSeparator`、`TagEnvLayout`、`TagEnvPrefix`、`TagEnvDescription`，以及 `ParseYAML` / `ParseJSON` / `ParseTOML`。

## 从 cleanenv 迁移

| cleanenv | cleannacos |
| --- | --- |
| `cleanenv.ReadConfig("config.yaml", &cfg)` | `cleannacos.ReadConfig(ctx, dsn, &cfg)` |
| `cleanenv.ReadEnv(&cfg)` | `cleannacos.ReadEnv(&cfg)` |
| `cleanenv.UpdateEnv(&cfg)` | `cleannacos.UpdateEnv(&cfg)` |
| `cleanenv.GetDescription(&cfg, nil)` | `cleannacos.GetDescription(&cfg, nil)` |
| `cleanenv.Usage(&cfg, nil)` | `cleannacos.Usage(&cfg, nil)` |
| — | `cleannacos.Watch[T](ctx, dsn, notify)` |

除了 `import` 路径和 `ReadConfig` 的参数（文件路径 → 上下文 + DSN），struct tag、默认值、环境变量覆盖行为都不需要调整。本地文件读取继续使用 upstream cleanenv，本库不提供 `file://`。

## 开发

```bash
make all                 # fmt-check + vet + test
make test                # 单元测试
make race                # 单元测试（-race）
make lint                # fmt-check + vet + golangci-lint
make tidy                # go mod tidy

# 集成测试：需要可用的 Nacos，未设置环境变量则自动跳过
CLEANNACOS_TEST_ADDR=127.0.0.1:8848 make integration-test
```

集成测试支持的环境变量：

| 变量 | 必填 | 说明 |
| --- | --- | --- |
| `CLEANNACOS_TEST_ADDR` | 是 | Nacos 地址，格式 `host:port`；未设置则跳过全部集成测试 |
| `CLEANNACOS_TEST_USERNAME` | 否 | 开启鉴权时的用户名 |
| `CLEANNACOS_TEST_PASSWORD` | 否 | 开启鉴权时的密码 |

## 已知限制

- 只有 `nacos://` 单一主机 DSN，不支持多地址集群列表，也不提供 `file://`（本地文件继续用 upstream cleanenv）。
- `Watch` 的 `T` 需要是 cleanenv 能解析的类型（通常是 struct）。
- 依赖的 nacos-sdk-go v2.3.5 在 `RpcClient.Shutdown` 中删除全局 client map 时没有加锁，而 `CreateClient` 读取该 map 时加锁，因此**在 `-race` 构建下**，「监听中关闭客户端」（即 `StopFunc` / 取消 `Watch` 的 ctx）可能报告一条发生在 SDK 内部的 data race，堆栈落在 `nacos-sdk-go` 的 `RpcClient.Shutdown` 与 `CreateClient` 上。这是上游缺陷，本库无法在自身代码里消除；单元测试仍然全程开启 `-race`，集成测试与 CI 的集成 job 因此不启用 `-race`。

## 许可证

MIT License，见 [LICENSE](LICENSE)。
