# Vigo Design

## 1. 项目定位

Vigo 是一个偏后端基础设施的 Go Web 框架，核心目标不是提供“更多功能”，而是把常见 API 开发路径收敛成一套可组合、可约束、可扩展的最小内核：

- 路由注册保持简洁，支持静态、参数、正则、通配符路由。
- Handler 支持多种签名，框架统一转换为标准执行模型。
- 请求参数通过结构体标签自动绑定，减少重复解析代码。
- Middleware 采用 Before -> Handler -> After 的洋葱模型。
- 文档、事件调度、认证、静态文件、对象存储等能力以 `contrib/` 方式提供。

当前阶段以“完善主体能力”为主，`contrib/crud` 不作为主体设计约束的一部分，后续单独重构。

## 2. 设计原则

### 2.1 约定优于配置

业务代码优先表达输入、输出和执行顺序，框架负责：

- 路由匹配
- 参数绑定
- handler 标准化
- middleware 链调度
- 响应输出
- 文档提取

### 2.2 小核心，外围能力模块化

根目录提供框架核心：

- `router.go`: 路由树、注册、匹配、缓存
- `x.go`: 请求上下文、执行流、错误流
- `xparser.go`: 参数绑定
- `xwriter.go`: 输出能力
- `types.go`: handler 标准化与适配
- `app.go` / `server.go`: 应用启动与 HTTP Server 封装
- `doc.go`: 文档模型与自动提取
- `error.go`: 统一错误模型

外围能力放在 `contrib/`：

- `event`: 后台任务与调度
- `auth`: 权限抽象
- `common`: 通用 JSON/静态资源处理
- `config`: DB/Redis/Key 配置
- `ufs`: 文件系统抽象
- `limiter` / `cache` / `cors` / `proxy`: 常见 HTTP 能力补充

### 2.3 显式执行流

Vigo 的 middleware 不是“黑盒链式调用”，而是由 `X.Next()` 驱动：

```text
Before(parent -> child) -> Handler -> After(child -> parent)
```

配套控制能力：

- `x.Next()`: 进入后续阶段
- `x.Stop()`: 终止后续执行
- `x.Skip(n)`: 跳过后续若干 handler
- `SkipBefore`: 路由级跳过父级 Before

这种模型强调顺序可预测，适合 API 框架做权限、审计、响应封装。

## 3. 核心架构

## 3.1 Router

路由树节点统一使用 `route` 表示，支持：

- 静态节点: `/user`
- 参数节点: `/{id}`
- 通配节点: `/*`
- 捕获全部: `/**` 或 `/{path:*}`
- 正则节点: `/{id:[0-9]+}`
- 复合片段: `/{name}.{ext}`

路由注册流程：

1. 根据 path 切分 segment。
2. 每个 segment 解析为不同 node type。
3. 节点按优先级排序，优先静态，再参数/正则，最后 wildcard/catch-all。
4. method 级缓存 handler 链与文档信息。

路由匹配流程：

1. 逐 segment 深度匹配。
2. 遇到 param/regex/wildcard 时写入 `x.PathParams`。
3. 命中 method 后取出缓存好的 handler 链。
4. 执行 `x.Next()` 进入 pipeline。

设计重点：

- 优先使用树结构解决大部分匹配性能问题。
- 把 handler 链在注册期做缓存，避免请求期重复拼装。
- 文档和 handler 元信息跟 route 同步存储，便于自动生成 API 文档。

## 3.2 X Context

`X` 是 Vigo 的请求上下文，职责包括：

- 持有 `Request` 与 `ResponseWriter`
- 维护 `PathParams`
- 保存请求级变量 `vars`
- 暴露路由级变量 `routeVars`
- 管理 handler 链和当前执行位置
- 用 `PipeValue` 在 handler 间传递中间结果

设计上 `X` 既是：

- HTTP 上下文
- middleware 驱动器
- 参数/响应适配点

同时通过 `sync.Pool` 复用，减少高频请求下的对象分配。

## 3.3 Handler 标准化

Vigo 允许业务写多种签名，例如：

```go
func(*X) error
func(*X) (any, error)
func(*X, *Req) (*Resp, error)
func(http.ResponseWriter, *http.Request)
```

框架启动时将其统一适配成：

```go
func(*X) (any, error)
```

这样可以把参数解析、结果传递、错误处理都统一进 pipeline。

设计目标：

- 业务层保留类型安全
- 框架层只维护一种执行模型
- 注册期完成尽量多的检查和标准化

## 3.4 参数绑定

Vigo 参数绑定以结构体 tag 为中心：

- `src:"json"`
- `src:"query"`
- `src:"form"`
- `src:"header"`
- `src:"path"`
- `src:"path@id"` 这类别名形式

绑定规则：

- 非指针字段默认视为必填
- 指针字段或带 `default` 的字段视为可选
- 仅解析一级字段，不做深层嵌套自动绑定

设计取舍：

- 保持简单，避免引入复杂 schema 系统
- 让 handler 入参即接口契约
- 文档提取可直接复用相同元信息

## 3.5 响应输出

`xwriter.go` 提供：

- `JSON`
- `String`
- `HTMLTemplate`
- `File`
- `Embed`
- `Flush`

推荐使用统一 After middleware 做响应包裹，例如：

- `common.JsonResponse`
- `common.JsonErrorResponse`

框架本身不强制响应协议，但主体设计倾向：

- handler 返回数据
- After middleware 负责最终输出

## 3.6 错误模型

Vigo 使用 `Error{Code, Message}` 作为统一业务错误类型。

约定：

- 4xxxx: 客户端输入/权限/资源相关
- 5xxxx: 服务端、依赖、系统错误
- 对外响应保留稳定 code
- 内部错误细节可在日志里扩展，不直接暴露到客户端

设计目标：

- HTTP 状态码和业务错误码共存
- 业务可直接组合预定义错误
- 文档和前端可依赖稳定 code 做分支

## 3.7 应用与启动

`App[T]` 聚合四个核心输入：

- `name`
- `router`
- `config`
- `init`

启动流程：

1. 注册命令行参数
2. 加载配置
3. 执行 `Init()`
4. 启动事件系统
5. 创建 HTTP Server
6. 挂载 Router
7. 暴露 API 文档

这让 Vigo 更像一个“应用壳”，而不只是裸 HTTP Router。

## 3.8 文档系统

Vigo 内建轻量文档协议，不直接依赖 OpenAPI。

文档来源：

- 路由描述字符串
- handler 入参类型
- handler 返回类型
- handler 代码位置

文档系统目标：

- 低成本自动生成
- 面向框架自身结构
- 优先可读性，再考虑标准兼容

后续仍可以在此基础上导出 OpenAPI。

## 3.9 事件系统

`contrib/event` 是主体能力的一部分，因为它承担了应用内后台任务的统一管理。

支持的任务模式：

- 一次性任务
- 周期任务
- 定时任务
- 失败重启
- 依赖顺序
- 分布式执行

设计上它不是独立平台，而是应用启动流程的组成部分。

## 4. 当前边界

当前主体范围包括：

- 路由
- pipeline
- 参数解析
- 响应输出
- 错误模型
- server/app
- 文档系统
- event
- auth 抽象接口
- 基础 middleware 能力

当前不纳入主体设计冻结范围：

- `contrib/crud`
- plugin 的运行时装载能力
- 过于业务化的二次封装

这些模块可以更激进地调整。

## 5. 当前已知问题

以下问题已经确认存在，但属于后续主体完善范围：

- handler 标准化内部仍有较重的 `unsafe` 依赖
- server 缺少默认超时和更稳妥的资源限制
- 请求体大小、multipart 限制与 config 未完全打通
- JSON 输出语义还不够统一
- 部分 contrib 模块缺少生命周期管理和测试覆盖
- `GetRemoteIP` 目前默认信任转发头，不适合直接暴露到公网场景

`crud` 的问题单列处理，不在这里展开。

## 6. 主体演进方向

主体功能后续演进遵循以下顺序：

1. 先稳住执行模型和 server 边界。
2. 再统一参数解析和响应协议。
3. 然后补 observability、shutdown、测试与文档导出。
4. 最后再处理高阶扩展和生态模块。

核心判断标准：

- 是否降低业务代码重复度
- 是否提升线上稳定性
- 是否让框架行为更可预测
- 是否能被测试稳定覆盖

