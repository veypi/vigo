# Vigo Doc Protocol

Vigo Doc 是一个专为 Vigo 框架设计的简洁、扁平化 API 描述协议。它旨在替代复杂的 OpenAPI 规范，提供更直观、更易读的 API 文档结构，特别适合快速开发和内部协作。

## 1. 核心设计理念

*   **扁平化 (Flat Structure)**: 移除 OpenAPI 中复杂的层级嵌套 (如 `paths` -> `method` -> `content` -> `schema`)，直接在路由层级展示参数和数据结构。
*   **统一参数 (Unified Params)**: 将 Path, Query, Header 参数统一管理，通过 `in` 字段区分。
*   **递归字段 (Recursive Fields)**: 使用统一的 `DocField` 结构递归描述 Object 和 Array，结构清晰。
*   **简洁类型 (Simple Types)**: 使用通用类型名称，屏蔽语言差异。

## 2. 协议结构

### 2.1 Doc (根对象)

API 文档的入口点。

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `title` | `string` | API 标题 |
| `version` | `string` | API 版本 |
| `routes` | `[]DocRoute` | 路由列表 |
| `others` | `map[uint]DocRoute` | 全局描述其他路由返回格式（如 404, 500 等） |

### 2.2 DocRoute (路由)

描述单个 API 接口端点。

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `method` | `string` | HTTP 方法 (GET, POST, PUT, DELETE, etc.) |
| `path` | `string` | 请求路径 (e.g., `/api/users/{id}`) |
| `summary` | `string` | 接口简短描述 |
| `params` | `[]DocParam` | 非 Body 参数列表 (Path, Query, Header) |
| `body` | `DocBody` | 请求体定义 |
| `response` | `DocBody` | 响应体定义 (描述 200 OK 成功响应) |
| `others` | `map[uint]DocRoute` | 其他 HTTP 状态码响应（如 404, 500 等） |



### 2.3 DocParam (参数)

描述 Path, Query 或 Header 参数。

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `name` | `string` | 参数名称 |
| `in` | `string` | 参数位置: `path`, `query`, `header` |
| `type` | `string` | 参数类型 (见类型系统) |
| `required` | `bool` | 是否必填 |
| `desc` | `string` | 参数描述 |

### 2.4 DocBody (数据体)

描述请求体或响应体。

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `content_type` | `string` | 内容类型 (e.g., `application/json`, `multipart/form-data`) |
| `fields` | `[]DocField` | 字段列表 |

### 2.5 DocField (字段 - 递归)

描述 JSON 对象属性、表单字段或数组元素。

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `name` | `string` | 字段名称 |
| `type` | `string` | 字段类型 (见类型系统) |
| `required` | `bool` | 是否必填 |
| `desc` | `string` | 字段描述 |
| `item` | `DocField` | **仅当 type=array 时有效**。描述数组元素的结构。 |
| `fields` | `[]DocField` | **仅当 type=object 时有效**。描述对象的子字段列表。 |

## 3. 类型系统

Vigo Doc 使用以下通用类型名称：

*   `string`: 字符串, 日期时间
*   `int`: 整数 (int, int64, uint, etc.)
*   `number`: 浮点数 (float32, float64)
*   `bool`: 布尔值
*   `file`: 文件对象 (用于 `multipart/form-data`)
*   `array`: 数组/切片
*   `object`: 结构体/映射/复杂对象

## 4. 示例

### YAML 格式 (易读)

```yaml
title: Vigo API
version: 1.0.0
routes:
  - method: POST
    path: /api/upload
    summary: 文件上传接口
    body:
      content_type: multipart/form-data
      fields:
        - name: description
          type: string
          required: true
        - name: files
          type: array
          required: true
          item:
            type: file
    response:
      content_type: application/json
      fields:
        - name: code
          type: int
          required: true
        - name: url
          type: string
          required: true
```

### JSON 格式 (紧缩)

```json
{
  "title": "Vigo API",
  "version": "1.0.0",
  "routes": [
    {
      "method": "GET",
      "path": "/api/users/{id}",
      "summary": "获取用户信息",
      "params": [
        {
          "name": "id",
          "in": "path",
          "type": "int",
          "required": true
        }
      ],
      "response": {
        "content_type": "application/json",
        "fields": [
          {
            "name": "id",
            "type": "int",
            "required": true
          },
          {
            "name": "username",
            "type": "string",
            "required": true
          }
        ]
      }
    }
  ]
}
```
