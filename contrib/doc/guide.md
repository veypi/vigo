# Doc 使用指南

## 页面操作

访问 `{{PREFIX}}/` 进入文档浏览器，文件列表居中显示。

- **浏览文件**: 点击文件名查看内容，文本文件显示源码，图片直接预览，二进制文件提示下载
- **下载文件**: 文件模式工具栏右侧有下载按钮，直接下载原始文件
- **返回上级**: 文件模式点击"← 返回"回到上级目录
- **搜索内容**: 搜索框输入关键词，按文件内容全文检索，结果按文件分组显示匹配行
- **浏览器导航**: 支持前进/后退，刷新自动恢复当前状态。URL 带 `?L=N` 可定位到指定行

## API 接口

API 接口默认可通过两种方式访问：

- 带 `X-No-Fallback: true` 头（推荐）
- 客户端 `Accept` 头不含 `text/html`（如 curl 的 `*/*`）

浏览器直接访问返回 SPA 页面，不会拿到原始数据。

### 目录列表

```
GET {{PREFIX}}/?depth=5
```

| 参数 | 说明 |
|------|------|
| depth | 最大递归深度，默认 1 |

返回 `ItemEntry` 树形结构，包含文件名、类型、大小、子项。

### 文件内容

```
GET {{PREFIX}}/{path}.md
```

返回原始 Markdown 文本（`Content-Type: text/plain`）。

### 文件搜索

```
GET {{PREFIX}}/?glob=**/*.md&pattern={regex}&limit=30&ignore_case=true
```

| 参数 | 说明 |
|------|------|
| glob | 文件名 glob 匹配模式，默认 `**/*` |
| pattern | 内容正则匹配，为空时只做文件名搜索 |
| limit | 返回结果上限，默认 100 |
| ignore_case | 大小写不敏感，默认 false |

返回 `SearchMatch[]`，每条包含文件路径、行号、行内容、列位置。

### 内容搜索示例

```bash
# 搜索包含 "vigo" 的文档
curl -H 'X-No-Fallback: true' '{{PREFIX}}/?glob=**/*.md&pattern=vigo'

# 搜索文件名包含 "api" 的文档
curl -H 'X-No-Fallback: true' '{{PREFIX}}/?glob=**/*api*.md'

# 忽略大小写搜索
curl -H 'X-No-Fallback: true' '{{PREFIX}}/?glob=**/*.md&pattern=Error&ignore_case=true'
```
