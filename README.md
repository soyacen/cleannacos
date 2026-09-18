# cleannacos - 纯 Nacos 配置读取库

`cleannacos` 只从 Nacos 读取配置：DSN 描述 Nacos 服务端，配置结构体的 `nacos-*` tag 描述每个字段来自哪个 dataId、哪个 group、哪个 namespace。不读环境变量，也不读本地文件。

```go
type Config struct {
	Server struct {
		Addr string `yaml:"addr" nacos-required:"true" nacos-description:"监听地址"`
	} `nacos-data-id:"server.yaml"`

	Database struct {
		Hosts []string `yaml:"hosts" nacos-separator:"|" nacos-default:"db-a|db-b"`
	} `nacos-data-id:"db.yaml" nacos-group:"DATABASE"`
}

var cfg Config
err := cleannacos.ReadConfig(ctx, "nacos://127.0.0.1:8848?group=DEFAULT_GROUP", &cfg)
```

## 特性

- **纯 Nacos 数据源**：没有环境变量层、没有本地文件层，取值链路只有「Nacos 内容 → 默认值 → 必填校验」。
- **按字段声明数据源**：`nacos-data-id` 可以出现在任意层级，按模块把配置拆到多个 dataId。
- **默认值与必填**：字段仍是零值时套 `nacos-default`，之后 `nacos-required` 字段必须持有值。
- **描述输出**：`GetDescription` 按 `dataId:键路径` 列出所有会读取的配置项、说明、默认值与必填标记。
- **变更监听**：`Watch[T]` 同时监听结构体声明的全部 dataId，回调串行化，配合 `atomic.Pointer[T]` 即可无数据竞争地热更新。
- **多格式**：`.yaml` / `.yml` / `.json` / `.toml`，由 dataId 后缀决定解析器。

## 安装

```bash
go get github.com/soyacen/cleannacos
```

## 快速开始

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/soyacen/cleannacos"
)

type ServerOptions struct {
	Addr    string `yaml:"addr" nacos-required:"true" nacos-description:"监听地址"`
	Debug   bool   `yaml:"debug" nacos-default:"false" nacos-description:"开启调试日志"`
	Timeout string `yaml:"timeout" nacos-default:"5s" nacos-description:"请求超时"`
}

type DatabaseOptions struct {
	Host string `yaml:"host" nacos-required:"true" nacos-description:"数据库地址"`
	Port int    `yaml:"port" nacos-default:"5432" nacos-description:"数据库端口"`
}

type Config struct {
	Server   ServerOptions   `nacos-data-id:"server.yaml"`
	Database DatabaseOptions `nacos-data-id:"db.yaml" nacos-group:"DATABASE"`
}

func main() {
	var cfg Config

	// Nacos 内容 → nacos-default → nacos-required
	if err := cleannacos.ReadConfig(context.Background(), "nacos://127.0.0.1:8848?group=DEFAULT_GROUP", &cfg); err != nil {
		log.Fatal(err)
	}

	description, err := cleannacos.GetDescription(&cfg, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(description)
}
```

可运行示例见 [example/simple](example/simple)（每个标签都用了一遍）与 [example/watch](example/watch)。

## 标签总览

| tag                   | 作用                                                       |
| --------------------- | ---------------------------------------------------------- |
| `nacos-data-id`     | 该字段子树的数据源，任意层级可用，多层重声明时内层覆盖外层 |
| `nacos-group`       | 覆盖该字段子树的 group，缺省用 DSN 的`group`             |
| `nacos-namespace`   | 覆盖该字段子树的 namespace，缺省用 DSN 的`namespace`     |
| `nacos-default`     | 字段仍是零值时填入的默认值                                 |
| `nacos-required`    | 默认值生效后仍为零值则报错，错误里带字段路径与 dataId      |
| `nacos-description` | 供`GetDescription` 输出的说明                            |
| `nacos-layout`      | 默认值转`time.Time` 等类型时使用的时间布局               |
| `nacos-separator`   | 默认值转 slice / map 时使用的分隔符，缺省`,`             |

除上述标签外，字段的键名照常由 `yaml` / `json` / `toml` tag 决定。

## DSN 规范

```
nacos://user:pass@host:8848?namespace=ns&group=g&timeoutMs=5000&logDir=/tmp/nacos/log&cacheDir=/tmp/nacos/cache&logLevel=debug&notLoadCacheAtStart=true&appName=xxx
```

- 只支持 `nacos://` scheme 与单台 `host:port`。
- **path 必须为空**：dataId 由 `nacos-data-id` 标签声明，写成 `nacos://host:8848/app.yaml` 会直接报错。

| query 参数              | 默认值               | 说明                          |
| ----------------------- | -------------------- | ----------------------------- |
| `namespace`           | `""`               | 默认 namespace，空串即 public |
| `group`               | `DEFAULT_GROUP`    | 默认 group                    |
| `timeoutMs`           | SDK 默认             | Nacos 客户端超时（毫秒）      |
| `logDir`              | `/tmp/nacos/log`   | Nacos SDK 日志目录            |
| `cacheDir`            | `/tmp/nacos/cache` | Nacos SDK 本地缓存目录        |
| `logLevel`            | SDK 默认             | Nacos SDK 日志级别            |
| `notLoadCacheAtStart` | SDK 默认             | 启动时不加载本地缓存          |
| `appName`             | SDK 默认             | Nacos 客户端 appName          |

用户名和密码通过 DSN 的 userinfo 传递（`nacos://admin:nacos@host:8848`）。已知参数的非法值（非数字 `timeoutMs`、非布尔 `notLoadCacheAtStart`、非法端口等）会直接报错，未知 query 参数忽略。不同 namespace 会各自建立一个客户端，group 只影响请求参数。

## 取值规则

1. **解析顺序**：先解析外层 dataId，再解析内层 dataId，因此内层文档里出现的键会覆盖外层同名键。
2. **数据源继承**：字段使用离它最近的、带 `nacos-data-id` 的祖先（含自身）作为数据源；同一个 `(namespace, group, dataId)` 只请求一次并复用内容。
3. **没有数据源的字段**：整棵子树保持零值，不套默认值也不做必填校验；但子树内自带 `nacos-data-id` 的字段照常生效。
4. **空内容**：某个 dataId 取到空内容（或配置不存在）时视为空文档，跳过解析，直接进入默认值与必填校验。
5. **默认值**：字段仍为零值时套用 `nacos-default`，转换规则与 cleanenv 一致（string、bool、int、uint、float、duration、time、slice、map、`encoding.TextUnmarshaler`），字段实现了 `SetValue(string) error` 时交给它处理；slice / map 用 `nacos-separator` 切分，时间类型用 `nacos-layout`。
6. **必填校验**：默认值处理完之后，`nacos-required` 字段仍为零值就报错，例如：

```
cleannacos: field "Database.Host" is required but no value was found in config "db.yaml" (group "DATABASE", namespace "")
```

解析失败同样会带上数据源：

```
cleannacos: parse config "db.yaml" (group "DATABASE", namespace "") as yaml: ...
```

## 变更监听

`Watch` 会先注册结构体声明的全部 Nacos listener，再立即拉取一次基线快照并**同步**回调，因此 `Watch` 返回时回调至少已经被调用过一次；之后任一数据源变更都会重新拉取全部数据源并整体重建，回调之间串行化：

```go
type Config struct {
	Server struct {
		Addr string `yaml:"addr" nacos-default:"localhost:8080"`
	} `nacos-data-id:"server.yaml"`

	Redis struct {
		Addr string `yaml:"addr" nacos-default:"127.0.0.1:6379"`
	} `nacos-data-id:"redis.yaml"`
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

- 只有内容真的变了才回调；同一内容重复推送会被去重。
- 坏内容（解析失败）只走 `errorHandler`，不回调，并保留上一次成功的快照。
- 未提供 `WithErrorHandler` 时默认用 `slog.Error` 输出。
- 取消传给 `Watch` 的 `ctx` 会静默停止；`StopFunc` 幂等，首次调用取消全部 listener 并关闭全部客户端。
- 基线拉取失败（网络错误 / 内容解析失败）时，`Watch` 取消 listener、关闭客户端并返回错误。

## API

```go
// 读取结构体声明的全部 Nacos 配置
func ReadConfig(ctx context.Context, dsn string, cfg interface{}) error

// 手动重新拉取并全量重合并（无监听器的刷新场景）
func UpdateConfig(ctx context.Context, dsn string, cfg interface{}) error

// 列出配置项：dataId:键路径 + 说明 + 默认值 + 必填标记
func GetDescription(cfg interface{}, headerText *string) (string, error)

// 变更监听：泛型回调「新的 *T」
type StopFunc func(ctx context.Context) error
type ErrFunc func(err error)
type WatchOption func(*watchOptions)

func WithErrorHandler(errFunc ErrFunc) WatchOption
func Watch[T any](ctx context.Context, dsn string, notify func(conf *T), options ...WatchOption) (StopFunc, error)

// 自定义默认值转换；解析器与常量
type Setter interface {
	SetValue(string) error
}

const DefaultSeparator = ","

func ParseYAML(r io.Reader, cfg interface{}) error
func ParseJSON(r io.Reader, cfg interface{}) error
func ParseTOML(r io.Reader, cfg interface{}) error
```

同时导出的标签常量：`TagNacosDataID`、`TagNacosGroup`、`TagNacosNamespace`、`TagNacosDefault`、`TagNacosRequired`、`TagNacosDescription`、`TagNacosLayout`、`TagNacosSeparator`。

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

### 用 Docker 起一个本地 Nacos

仓库自带的 `docker-compose.yml` 会启动一个单机 Nacos（`nacos/nacos-server:v2.5.2`，与 CI 同版本，关闭鉴权，数据不落盘），需要本机 Docker 已启动：

| 命令                            | 作用                                                                   |
| ------------------------------- | ---------------------------------------------------------------------- |
| `make nacos-up`               | 后台启动 Nacos（容器名`cleannacos-nacos`，默认映射 8848/9848）       |
| `make nacos-wait`             | 轮询就绪探针，最多等 120 秒                                            |
| `make integration-test-local` | 起 Nacos → 等就绪 → 跑集成测试 → 无论成败都清理容器                 |
| `make example-e2e`            | 往 Nacos 写入示例配置，实际运行`example/simple` 与 `example/watch` |
| `make verify-local`           | 上面两步合起来：一次命令完成集成测试 + 示例端到端，结束后自动清理      |
| `make nacos-down`             | 停掉容器并删除其中的数据                                               |

```bash
make verify-local        # 推荐：一条命令完成真实 Nacos 验证
make nacos-up            # 或者手动控制生命周期
make example-e2e
make nacos-down
```

默认端口是 8848/9848，可以整体挪开（Nacos SDK 用 HTTP 端口 +1000 推导 gRPC，所以 gRPC 端口会跟着走，集成测试地址也自动跟随）：

```bash
make verify-local NACOS_HTTP_PORT=18848
```

如果 8848 已被别的服务占用（例如本机另有一个 Nacos 容器），`nacos-up` 会直接报端口冲突，用上面的方式换端口即可。

集成测试支持的环境变量：

| 变量                         | 必填 | 说明                                                    |
| ---------------------------- | ---- | ------------------------------------------------------- |
| `CLEANNACOS_TEST_ADDR`     | 是   | Nacos 地址，格式`host:port`；未设置则跳过全部集成测试 |
| `CLEANNACOS_TEST_USERNAME` | 否   | 开启鉴权时的用户名                                      |
| `CLEANNACOS_TEST_PASSWORD` | 否   | 开启鉴权时的密码                                        |

## 已知限制

- 只有 `nacos://` 单一主机 DSN，不支持多地址集群列表，也不提供 `file://` 或环境变量数据源。
- 一个字段只能声明一个 dataId，不支持同一字段按顺序合并多个 dataId。
- 没有可追溯数据源的字段会被静默跳过，不做校验；需要用 `nacos-required` 表达必填时，请确保该字段在某个 `nacos-data-id` 子树内。
- `Watch` 的 `T` 需要是 struct。
- 依赖的 nacos-sdk-go v2.3.5 在 `RpcClient.Shutdown` 中删除全局 client map 时没有加锁，而 `CreateClient` 读取该 map 时加锁，因此**在 `-race` 构建下**，「监听中关闭客户端」（即 `StopFunc` / 取消 `Watch` 的 ctx）可能报告一条发生在 SDK 内部的 data race，堆栈落在 `nacos-sdk-go` 的 `RpcClient.Shutdown` 与 `CreateClient` 上。这是上游缺陷，本库无法在自身代码里消除；单元测试仍然全程开启 `-race`，集成测试与 CI 的集成 job 因此不启用 `-race`。

## 许可证

MIT License，见 [LICENSE](LICENSE)。
