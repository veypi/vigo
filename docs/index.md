# Vigo 文档地图

统一架构：根目录放 `README.md` / `CHANGELOG.md` / `AGENTS.md` / `LICENSE`，其余文档进
`docs/`，按 `architecture/`（架构设计）、`reference/`（契约规范）、`guides/`（使用操作）、
`development/`（计划与过程文档）分类。`contrib/` 各模块的使用说明跟随模块源码，不搬进
`docs/`。

## 入口

| 文档 | 内容 | 何时读 |
| --- | --- | --- |
| [README.md](../README.md) | 定位、安装、快速开始、路由语法、Handler 流水线 | 使用框架前 |
| [AGENTS.md](../AGENTS.md) | 后端开发规范（路由、参数绑定、中间件、错误处理、数据库） | 用 Vigo 写业务代码前 |
| [CHANGELOG.md](../CHANGELOG.md) | 已完成变更 | 升级框架时 |

## architecture

| 文档 | 内容 | 何时读 |
| --- | --- | --- |
| [architecture/overview.md](architecture/overview.md) | 项目定位、设计原则、内核与 contrib 边界 | 改框架内核前 |

## reference

| 文档 | 内容 | 何时读 |
| --- | --- | --- |
| [reference/doc-protocol.md](reference/doc-protocol.md) | Doc 协议（文档接口的描述与暴露契约） | 接入或修改 doc 能力时 |

## development

| 文档 | 内容 | 何时读 |
| --- | --- | --- |
| [development/todo.md](development/todo.md) | 主体能力的未来规划、开发约束与优先级 | 评估后续工作时；已完成事实写 CHANGELOG |

## contrib（随模块源码）

| 文档 | 内容 |
| --- | --- |
| [contrib/USAGE.md](../contrib/USAGE.md) | contrib 模块使用总览 |
| [contrib/doc/guide.md](../contrib/doc/guide.md) / [contrib/doc/README.md](../contrib/doc/README.md) | Doc 中间件用法 |
| [contrib/event/README.md](../contrib/event/README.md) | 事件包 |
| [contrib/plugin/README.md](../contrib/plugin/README.md) / [contrib/plugin/AGENTS.md](../contrib/plugin/AGENTS.md) | 插件库与插件开发规范 |
| [flags/README.md](../flags/README.md) | 配置与命令行 |

## 约定

- 版本以 git tag（`vX.Y.Z`）为准；已完成的变更写根目录 CHANGELOG，未来计划只写
  `development/todo.md`。
- 使用文档（README）、设计文档（architecture）、过程文档（development）三者不互相
  重复；contrib 的文档留在各自模块目录。
