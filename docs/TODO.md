# Vigo TODO

## 1. 范围说明

本文件只记录 Vigo 主体能力的未来规划、开发约束和阶段性优先级。

当前明确不处理：

- `contrib/crud`

它后续单独重构，不作为当前主体改造的一部分。

## 2. 开发约束

## 2.1 总体约束

- 优先增强主体能力，不先做业务糖衣。
- 优先消除不稳定实现，再新增能力。
- 新能力默认要能被文档系统表达，或至少不破坏现有文档提取。
- 新能力默认要能和现有 `Before -> Handler -> After` 模型共存。
- 对外接口一旦公开，优先向后兼容；如必须变更，先补迁移说明。

## 2.2 代码约束

- 谨慎引入 `unsafe`，框架核心默认不再扩大其使用范围。
- 避免把反射、序列化、网络细节泄漏到业务 handler。
- 请求期逻辑优先无锁或低锁；重初始化和元信息计算尽量放到注册期。
- 不把临时 demo 代码放进核心包。
- `contrib/` 模块如果会启动 goroutine，必须具备可停止或可托管的生命周期。
- 新配置项必须接入真实运行路径，不能只定义 struct 字段。

## 2.3 API 约束

- handler 仍然以“返回值 + error”为核心模型。
- 错误码保持稳定，不轻易改已有 code。
- 参数绑定规则必须简单、一致、可文档化。
- 默认行为要偏安全，危险能力通过显式配置开启。

## 2.4 测试约束

- 核心路径修改必须补测试：router、pipeline、parser、writer、server、doc。
- 修 bug 时至少补一个失败前置测试。
- 新 middleware 若改变响应语义，需要补集成测试。
- 与并发、超时、回收相关的改动，要补 race-friendly 测试或生命周期测试。

## 3. 近期优先级

## P0 主体稳定性

- 收敛 handler 标准化实现，逐步移除或隔离高风险 `unsafe` 路径。
- 给 `server` 增加合理的默认超时和 `MaxHeaderBytes`。
- 增加优雅关闭能力，包括 HTTP server 和 event manager 的 shutdown。
- 统一 body size limit，打通 `Config.PostMaxMemory` 与实际解析逻辑。
- 明确可信代理策略，修正 `GetRemoteIP` 的默认行为。

## P1 主体一致性

- 统一 JSON/文本/错误响应语义，规范 `Content-Type` 和状态码写入时机。
- 统一 `x.Parse` 与手动解码路径，避免同类功能出现多套规则。
- 补齐文档中对 default、required、content-type 的表达一致性。
- 清理 Config 中未真正生效或语义不清晰的字段。
- 梳理 `contrib/common` 的响应封装边界，避免和 `x.JSON` 语义冲突。

## P2 可维护性

- 补充核心模块测试覆盖。
- 给 `contrib/cache`、`contrib/limiter` 等模块增加生命周期控制。
- 统一日志上下文字段，增加 request id、耗时、路由摘要等基础观测信息。
- 为 doc system 增加更清晰的开发文档和示例。

## 4. 中期规划

## 4.1 文档与接口描述

- 在现有 `Doc` 协议之上增加 OpenAPI 3 导出。
- 支持更完整的字段描述：枚举、示例值、deprecated、格式约束。
- 补文档过滤和分组能力，支持大项目拆分查看。

## 4.2 请求处理能力

- 增加可选的严格 JSON 模式，如 `DisallowUnknownFields`。
- 增加可组合的参数校验标签，而不是把校验都推给业务层。
- 为 multipart/form-data 提供更稳定的统一解析和上限控制。
- 完善 streaming / SSE / file response 的标准用法。

## 4.3 应用生命周期

- 统一 app、server、event 的启动和关闭钩子。
- 支持更清晰的 readiness / liveness 检查模式。
- 明确 domain router、sub app 集成、静态资源挂载的生命周期边界。

## 4.4 可观测性

- 增加 metrics middleware。
- 增加 tracing hook。
- 增加 panic / error 的结构化输出。
- 增加慢请求阈值和采样控制。

## 5. 长期方向

- 将 Vigo 的文档、参数绑定、响应协议进一步标准化，形成稳定的框架契约。
- 让主体能力在“轻量”和“生产可用”之间达到更平衡的默认状态。
- 保持核心简单，把复杂特性留给可插拔模块，而不是不断膨胀核心包。

## 6. 暂不做

以下内容在主体完善前不优先处理：

- `crud` 通用控制器重构
- 为所有 contrib 做功能扩张
- 引入大型依赖只为解决局部问题
- 把 Vigo 改造成全家桶平台

## 7. 执行建议

建议按下面顺序推进：

1. server / lifecycle / parser / response
2. test coverage / observability
3. doc export / validation / stricter config
4. 再处理 `crud` 和更高层抽象

