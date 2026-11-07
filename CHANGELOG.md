# Changelog

版本以 git tag（`vX.Y.Z`）为准，遵循 SemVer。本文只记录已完成变更，未来计划写入
[docs/development/todo.md](docs/development/todo.md)。v0.8.2 之前的历史见 git 记录。

## 0.8.3 — 2026-11-07

- **ufs 内容哈希条件写入（`X-FS-Expected-Hash`）**：`FS` 增加可选接口 `ContentHashWriter`
  （`WriteFileIfHash`），HTTP `PUT` 带 `X-FS-Expected-Hash: sha256:<64 位小写十六进制>` 时
  先校验当前内容哈希再原子替换：哈希不符返回 412 `file_hash_conflict`，格式非法返回 400
  `invalid_file_hash`。`localFS` 同一路径的写/重命名/删除串行化（引用计数路径锁），避免
  「校验后、提交前」被另一次写入插入；`embedFS` 的 ETag 改用纳秒 mtime + size，仅作 HTTP
  缓存校验，与内容哈希条件写无关。测试：`contrib/ufs`（含并发条件写只有一个获胜、锁不泄漏）。

## 0.8.2 — 2026-10-27

### 文档

- 统一文档架构（无代码变更）：`agents.md` → `AGENTS.md`；`docs.md` →
  `docs/reference/doc-protocol.md`；`docs/design.md` → `docs/architecture/overview.md`；
  `docs/TODO.md` → `docs/development/todo.md`；新增 [docs/index.md](docs/index.md)
  文档地图与本 CHANGELOG，README 增加文档入口。
