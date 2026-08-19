# Red Token 后端 API 接口文档

本文档依据当前 `backend` 源码整理，覆盖健康检查、模型与代理接口，以及全部后台管理接口。

## 1. 基础信息

- 默认后端地址：`http://localhost:4000`
- 默认响应类型：`application/json`
- 默认监听地址可通过 `RT_LISTEN_ADDR` 修改。
- Chrome CDP 登录凭据同步默认连接 `http://127.0.0.1:9222`，可通过 `RT_CHROME_CDP_URL` 修改。
- 所有响应都会带有 `X-Request-ID`。客户端可以通过同名请求头传入请求 ID；未传时由服务端生成。
- 时间字段使用 RFC 3339/RFC 3339 Nano 字符串，通常为 UTC，例如 `2026-08-05T08:30:00Z`。

### 1.1 鉴权

`/v1/*` 接口必须提供已启用的客户端密钥，支持以下两种方式：

```http
Authorization: Bearer tg-xxxxxxxx
```

或：

```http
X-Api-Key: tg-xxxxxxxx
```

当两个请求头同时存在时优先使用 `Authorization`。`Authorization` 也接受不带 `Bearer` 前缀的原始密钥。

当前 `/admin/api/*` 管理接口没有鉴权中间件。部署到非可信网络前，必须在反向代理层增加访问控制。管理接口的部分响应包含上游 API Key、控制台凭据、代理密码、客户端 Token 和请求/响应预览等敏感数据。

### 1.2 JSON 请求约定

- 除配置更新接口外，使用结构体接收的 JSON 请求会拒绝未知字段。
- 请求体不是合法 JSON、字段类型错误或包含未知字段时，通常返回 `400`。
- 创建接口通常要求完整对象；更新后端接口为字段级 PATCH 语义，但 HTTP 方法为 `PUT`。

### 1.3 通用错误格式

```json
{
  "error": {
    "message": "错误信息",
    "type": "red_token_error"
  }
}
```

控制台同步和签到接口的错误响应还会包含已执行的上游请求：

```json
{
  "error": {
    "message": "错误信息",
    "type": "red_token_error"
  },
  "requests": [
    {
      "time": "2026-08-05T08:30:00.123Z",
      "method": "GET",
      "path": "/api/account",
      "status_code": 200,
      "body": "{...}"
    }
  ]
}
```

常见状态码：

| 状态码 | 含义 |
| --- | --- |
| `200` | 查询、更新、删除或操作成功 |
| `201` | 创建或导入成功 |
| `307` | `/` 临时重定向到 `/admin/` |
| `400` | 请求参数、ID、JSON 或业务校验失败 |
| `401` | `/v1/*` 缺少或使用了无效/已禁用的客户端密钥 |
| `404` | 资源不存在或代理路径不支持 |
| `405` | 路径存在但 HTTP 方法不匹配 |
| `409` | 创建的资源稳定 ID 已存在 |
| `499` | 客户端在代理请求完成前取消连接 |
| `500` | 数据库或服务端内部错误 |
| `502` | 工作流上游请求、响应解析、表达式或输出校验失败 |
| `502` | 后端控制台请求失败 |
| `503` | 没有可用上游后端，或所有候选后端均失败 |

### 1.4 分页格式

支持分页的接口统一使用：

| 查询参数 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `page` | integer | `1` | 小于 `1` 时按 `1` 处理 |
| `limit` | integer | `10` | 小于等于 `0` 时按 `10` 处理，最大 `10000` |

响应格式：

```json
{
  "items": [],
  "total": 0,
  "page": 1,
  "limit": 10
}
```

## 2. 通用数据模型

下列模型会被多个接口引用。标记为“可省略”的字段使用了 `omitempty`，值为空时可能不出现在 JSON 中。

### 2.1 `BackendAPIKey`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | string | 上游 Key ID；历史手工数据可以为空 |
| `key` | string | 上游 API Key |
| `name` | string | Key 名称；可以为空，前端显示时回退为“未知” |
| `group` | string | Key 分组，必填 |
| `models` | string[] | 支持的模型；支持 `*`、`?` 通配模式，至少一项 |
| `model_mapping` | object<string,string> | 客户端模型名到上游模型名的映射 |
| `used_quota` | number | 已用额度，允许有限小数且必须大于等于 `0` |

示例：

```json
{
  "id": "56382",
  "key": "sk-upstream",
  "name": "production-key",
  "group": "default",
  "models": ["gpt-5", "gpt-4.*"],
  "model_mapping": {
    "gpt-5-client": "gpt-5"
  },
  "used_quota": 0
}
```

### 2.2 `SocksProxy`

```json
{
  "id": 1,
  "name": "proxy-cn",
  "address": "127.0.0.1:1080",
  "username": "user",
  "password": "password",
  "enabled": true,
  "created_at": "2026-08-05T08:30:00Z",
  "updated_at": "2026-08-05T08:30:00Z"
}
```

`password` 为空时可省略。

### 2.3 前端 `Backend`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | integer | 后端 ID |
| `name` | string | 名称，数据库中唯一 |
| `protocol` | string | `openai`、`anthropic` 或 `both` |
| `base_url` | string | 上游推理 API 基础地址 |
| `api_keys` | `BackendAPIKey[]` | 上游密钥及模型路由配置 |
| `console_url` | string | 管理控制台基础地址 |
| `tags` | string[] | 标签 |
| `console_username` | string | 控制台用户名 |
| `console_password` | string | 控制台密码，可省略 |
| `console_checkin_workflow_id` | string | 绑定的签到工作流 ID；空值表示不启用签到 |
| `manual_checkin` | boolean | 传递给签到工作流 `$runtime.manual_checkin` 的手工签到标记，默认 `false` |
| `console_headers` | object<string,string> | 控制台请求头 |
| `console_account` | string | 已归一化账户对象的 JSON 字符串；额度均为最终金额 |
| `console_models` | string | 已归一化模型数组的 JSON 字符串；价格均为最终价格 |
| `notes` | string | 备注 |
| `proxy_id` | integer | SOCKS5 代理 ID；`0` 表示直连 |
| `status` | string | `normal`、`abnormal` 或 `disabled` |
| `weight` | integer | 调度权重，最小为 `1`，数值越大优先级越高 |
| `created_at` | string | 创建时间 |
| `updated_at` | string | 更新时间 |
| `avg_latency_ms` | number | 历史平均请求延迟 |

账户 JSON 固定使用 `id`、`username`、`quota`、`quota_unit`、`used_quota`、`today_reward`，有值时还包含 `last_checkin_at` 和 `last_workflow_at`。签到工作流的固定输出由服务端转换；前端不再读取 `quota_per_unit`。

模型 JSON 是数组。按量模型使用：

```json
{
  "name": "gpt-5.6-sol",
  "cheapest_groups": ["default"],
  "price_type": 0,
  "in_price": 2.5,
  "out_price": 20
}
```

按次模型使用 `price_type: 1` 和最终 `price` 字段。前端不再根据 `model_ratio`、`group_ratio` 或 `quota_per_unit` 二次计算。

### 2.4 `ClientKey`

```json
{
  "id": 1,
  "name": "production",
  "token": "tg-xxxxxxxx",
  "token_prefix": "tg-xxxxx",
  "allowed_models": "gpt-5,gpt-4.1",
  "enabled": true,
  "created_at": "2026-08-05T08:30:00Z",
  "updated_at": "2026-08-05T08:30:00Z"
}
```

`token_hash` 永远不会输出。`token` 为空时可省略。当前代码会保存 `allowed_models`，但代理鉴权和后端选择尚未使用该字段限制模型。

### 2.6 `UsageLog`

```json
{
  "id": 1,
  "request_id": "abc123",
  "client_id": 1,
  "client_name": "production",
  "client_token_prefix": "tg-xxxxx",
  "method": "POST",
  "path": "/v1/responses",
  "query": "",
  "endpoint": "responses",
  "model": "gpt-5",
  "backend_id": 2,
  "backend_name": "upstream-a",
  "proxy_id": 0,
  "proxy_name": "direct",
  "attempts": 1,
  "status_code": 200,
  "status_family": "2xx",
  "duration_ms": 820,
  "error_message": "",
  "client_ip": "127.0.0.1:50000",
  "user_agent": "curl/8.0",
  "trace_id": "abc123",
  "request_bytes": 120,
  "response_bytes": 860,
  "input_tokens": 20,
  "output_tokens": 50,
  "input_cache_tokens": 0,
  "request_headers_json": "{...}",
  "request_body_preview": "{...}",
  "response_headers_json": "{...}",
  "response_body_preview": "{...}",
  "preview_truncated": false,
  "is_stream": false,
  "created_at": "2026-08-05T08:30:00Z"
}
```

请求和响应正文预览上限为 16 KiB；敏感请求头会经过脱敏，但正文预览仍可能包含业务数据。

### 2.7 `AuditEvent`

```json
{
  "id": 1,
  "level": "info",
  "type": "admin_backend_sync",
  "category": "admin_backend_sync",
  "severity": "info",
  "actor": "admin",
  "resource_type": "backend",
  "resource_id": 2,
  "message": "backend console synced: upstream-a",
  "client_name": "",
  "model": "",
  "endpoint": "",
  "backend_name": "upstream-a",
  "created_at": "2026-08-05T08:30:00Z"
}
```

### 2.8 资源详情格式

后端、客户端密钥和 SOCKS5 代理详情接口使用以下通用结构：

```json
{
  "overview": [
    {"key": "name", "label": "Name", "value": "resource-name"}
  ],
  "configuration": [],
  "metadata": [
    {"key": "id", "label": "ID", "value": 1}
  ],
  "raw": {},
  "activity": {
    "usage": [],
    "usage_logs": [],
    "events": [],
    "backends": []
  }
}
```

`activity` 内为空的数组字段可能被省略。

## 3. 系统与公共代理接口

### 3.1 健康检查

`GET /healthz`

请求：无请求体、无鉴权。

成功响应 `200`：

```json
{
  "ok": true
}
```

### 3.2 根路径重定向

`GET /`

请求：无请求体、无鉴权。

响应：`307 Temporary Redirect`，`Location: /admin/`。这不是 JSON API。

### 3.3 获取可用模型

`GET /v1/models`

鉴权：需要客户端密钥。

请求：无请求体。

响应 `200`：

```json
{
  "object": "list",
  "data": [
    {
      "id": "gpt-5",
      "object": "model",
      "owned_by": "red-token"
    }
  ]
}
```

模型来源于状态为 `normal` 的后端：

- `model_mapping` 的客户端模型名会被输出。
- 被映射的上游模型名不会重复输出。
- 含 `*` 或 `?` 的模型模式不会输出。
- 当前返回顺序不保证稳定。

### 3.4 推理代理接口

鉴权：全部需要客户端密钥。

支持的路径：

| 推荐方法 | 路径 | 内部端点类型 | 请求/响应协议 |
| --- | --- | --- | --- |
| `POST` | `/v1/chat/completions` | `chat` | OpenAI Chat Completions |
| `POST` | `/v1/responses` | `responses` | OpenAI Responses |
| `POST` | `/v1/embeddings` | `embeddings` | OpenAI Embeddings |
| `POST` | `/v1/images/generations` | `images` | OpenAI Images |
| `POST` | `/v1/messages` | `messages` | Anthropic Messages |
| `POST` | `/v1/messages/count_tokens` | `messages` | Anthropic Token Counting |

路由层实际接受任意 HTTP 方法，但这些上游推理接口通常应使用 `POST`。

请求头：除认证头外，其余请求头会转发到上游；`Authorization`、`X-Api-Key`、`Host` 和 `Content-Length` 会被移除并替换为当前候选后端的认证信息。

请求体：JSON 格式由对应 OpenAI/Anthropic 接口定义，但顶层必须包含非空 `model`：

```json
{
  "model": "gpt-5",
  "input": "你好"
}
```

响应：

- 成功时透传被选中上游的状态码、响应头和响应体。
- 支持普通 JSON 和 `text/event-stream` 响应格式。当前实现会为了解压、协议转换和日志记录而完整缓冲上游响应，因此 SSE 内容不是实时逐块转发，而是在上游响应读取完成后写给客户端。
- 上游的 `gzip`、`deflate`、`br`、`zstd` 响应会先解压，再返回给客户端。
- 当客户端请求 `/v1/messages` 而后端协议为 OpenAI 时，会转换为 `/v1/responses` 请求并把响应转换回 Anthropic Messages 格式。
- 当客户端请求 `/v1/responses` 而后端协议为 Anthropic 时，会转换为 `/v1/messages` 请求并把响应转换回 OpenAI Responses 格式。
- 跨协议转换同时支持 JSON 和 SSE；`/v1/messages/count_tokens` 不做跨协议转换。
- 非 `2xx` 上游响应通常触发下一个候选后端，而不是直接透传；`/v1/messages/count_tokens` 例外，会直接返回上游响应。

代理层错误示例：

```json
{
  "error": {
    "message": "no backend available",
    "type": "red_token_error"
  }
}
```

主要错误：

| 状态码 | 条件 |
| --- | --- |
| `400` | 请求体无法读取、不是合法 JSON 或缺少 `model` |
| `401` | 缺少、无效或已禁用的客户端密钥 |
| `404` | `/v1/` 下的路径不在上述支持列表中 |
| `499` | 客户端取消请求 |
| `503` | 没有匹配模型的正常后端，或所有候选后端失败 |

后端候选按 `weight` 降序排列，权重相同则按后端 ID 升序；每个匹配模型的 API Key 都会形成一个候选。

## 4. 仪表盘与搜索

### 4.1 总览

`GET /admin/api/overview`

请求：无请求体、无查询参数。

响应 `200`：

```json
{
  "backends": [],
  "socks_proxies": 2,
  "client_keys": 3,
  "events": []
}
```

- `backends`：`BackendView[]`，包含最近 30 分钟和最近一小时统计。
- `events`：最近 20 条 `AuditEvent`。

### 4.2 仪表盘摘要

`GET /admin/api/dashboard/summary`

请求：无请求体、无查询参数。

响应 `200`：

```json
{
  "cards": {
    "backends": {"count": 3, "enabled": 2, "failures": 1},
    "client_keys": {"count": 4, "enabled": 2},
    "proxies": {"count": 1}
  },
  "counts": {
    "backends": 3,
    "client_keys": 4,
    "socks_proxies": 1
  },
  "growth": {
    "requests": 12.5,
    "errors": -20
  },
  "status": {
    "healthy_backends": 2,
    "recent_errors": 1,
    "active_clients": 2
  },
  "sparkline": [
    {"label": "Jul 30", "requests": 20}
  ]
}
```

`growth` 为当前 7 个 UTC 日历日（含当天未结束部分）相对之前 7 日的百分比；前一周期为 `0` 而当前周期非 `0` 时返回 `100`。`recent_errors` 统计最近 24 小时，`active_clients` 统计当前 7 日窗口。

### 4.3 仪表盘用量序列

`GET /admin/api/dashboard/usage`

查询参数：

| 参数 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `range` | string | `7d` | `24h`/`1d`、`7d` 或 `30d`；其他值按 `7d` 处理 |

响应 `200`：

```json
{
  "range": "24h",
  "series": [
    {
      "label": "08:00",
      "requests": 10,
      "successes": 9,
      "failures": 1,
      "latency_ms": 8300,
      "traffic_bytes": 120000,
      "error_rate": 10
    }
  ]
}
```

`latency_ms` 是时间桶内延迟总和，不是平均值。值为 `0` 的 `successes`、`failures`、`latency_ms`、`traffic_bytes` 可能省略。

### 4.4 仪表盘最近活动

`GET /admin/api/dashboard/activity`

请求：无请求体、无查询参数。

响应 `200`：

```json
{
  "events": [],
  "usage": [],
  "usage_logs": [],
  "summary": [
    {"category": "admin_backend_sync", "count": 2}
  ]
}
```

- `events`：最近 10 条 `AuditEvent`。
- `usage` 和 `usage_logs`：同一批最近 10 条 `UsageLog`，两个字段内容相同。

### 4.5 全局搜索

`GET /admin/api/search`

查询参数：

| 参数 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `q` | string | 空 | 搜索词；为空时各结果数组均为空 |
| `limit` | integer | `6` | 每一种资源的最大结果数，最大 `20` |

响应 `200`：

```json
{
  "query": "gpt",
  "results": {
    "backends": [],
    "client_keys": [],
    "proxies": [],
    "usage_logs": [],
    "events": []
  }
}
```

每个结果项格式：

```json
{
  "kind": "backend",
  "id": 1,
  "title": "upstream-a",
  "subtitle": "https://api.example.com",
  "meta": {"base_url": "https://api.example.com"},
  "status": "normal",
  "target_page": "backends",
  "target_id": 1
}
```

`kind` 可能为 `backend`、`client_key`、`socks_proxy`、`usage_log` 或 `event`；`meta` 内容随资源类型变化。

## 5. SOCKS5 代理管理

### 5.1 代理列表

`GET /admin/api/socks-proxies`

请求：无请求体；支持通用分页参数。

响应 `200`：通用分页结构，`items` 每项包含 `SocksProxy` 全部字段以及：

```json
{
  "bound_backend_count": 2,
  "request_count": 100,
  "traffic_bytes": 2048000,
  "avg_latency_ms": 650.5,
  "last_used_at": "2026-08-05T08:30:00Z"
}
```

`last_used_at` 没有数据时省略。

### 5.2 代理详情

`GET /admin/api/socks-proxies/{id}/detail`

路径参数：`id` 必须为正整数。

请求：无请求体。

响应 `200`：资源详情格式。

- `raw`：完整 `SocksProxy`，可能包含密码。
- `activity.usage` 与 `activity.usage_logs`：最近 10 条相关用量日志。
- `activity.backends`：绑定该代理的后端列表。
- `activity.events`：当前为空。

错误：无效 ID 返回 `400`，不存在返回 `404`。

### 5.3 创建代理

`POST /admin/api/socks-proxies`

请求体：

```json
{
  "name": "proxy-cn",
  "address": "127.0.0.1:1080",
  "username": "user",
  "password": "password",
  "enabled": true
}
```

校验：

- `address` 必填，必须为 `host:port`。
- 主机不能为空，端口必须在 `1-65535`。
- `name` 受数据库唯一约束。

响应 `201`：创建后的 `SocksProxy`。

### 5.4 更新代理

`PUT /admin/api/socks-proxies/{id}`

路径参数：`id` 必须为正整数。

请求体与创建接口相同，所有字段都会覆盖原值；省略字段会使用 Go 零值，因此应发送完整对象。

响应 `200`：更新后的 `SocksProxy`。

错误：无效 ID 或参数返回 `400`，不存在返回 `404`。

### 5.5 删除代理

`DELETE /admin/api/socks-proxies/{id}`

请求：无请求体。

响应 `200`：

```json
{
  "deleted": 1
}
```

错误：无效 ID 返回 `400`，不存在返回 `404`，数据库删除失败返回 `500`。

## 6. 后端管理

### 6.1 后端列表

`GET /admin/api/backends`

请求：无请求体；支持通用分页参数。

响应 `200`：通用分页结构，`items` 使用统一的前端 `Backend` 格式。列表不再包含请求数、小时统计、近期成功/失败数、恢复状态或代理详情。

```json
{
  "items": [
    {
      "id": 1,
      "name": "lyclaude",
      "protocol": "both",
      "base_url": "https://free.lyclaude.site/v1",
      "api_keys": [
        {
          "id": "56382",
          "key": "sk-upstream",
          "name": "wahaha",
          "group": "default",
          "models": ["gpt-5.6-sol", "claude-opus-4-6"],
          "model_mapping": {"claude-opus-4-6": "claude-opus-5"},
          "used_quota": 0
        }
      ],
      "console_url": "https://free.lyclaude.site",
      "console_username": "user@example.com",
      "console_password": "password",
      "console_checkin_workflow_id": "relay-default-checkin",
      "console_headers": {"Cookie": "session=..."},
      "console_models": "[{\"cheapest_groups\":[\"default\"],\"in_price\":2.5,\"name\":\"gpt-5.6-sol\",\"out_price\":20,\"price_type\":0}]",
      "console_account": "{\"id\":\"49722\",\"quota\":2600,\"quota_unit\":\"USD\",\"today_reward\":0,\"used_quota\":0,\"username\":\"\"}",
      "notes": "",
      "proxy_id": 0,
      "status": "normal",
      "weight": 88,
      "created_at": "2026-06-23T05:37:56Z",
      "updated_at": "2026-08-14T17:28:31Z",
      "avg_latency_ms": 2655.9,
      "tags": []
    }
  ],
  "total": 1,
  "page": 1,
  "limit": 10
}
```

该管理接口包含未脱敏的 API Key、控制台密码和控制台请求头，只能暴露在受保护的管理端。

### 6.2 后端详情

`GET /admin/api/backends/{id}/detail`

路径参数：`id` 必须为正整数。

请求：无请求体。

响应 `200`：资源详情格式。

- `overview`：控制台地址、签到工作流、状态、失败次数、恢复时间、代理、协议和权重等。
- `configuration`：已脱敏的 API Key、标签、备注、基础地址、控制台账户和定价对象。
- `raw`：已对 API Key、控制台密码、Cookie 和控制台请求头值做 `set` 标记。
- `activity.usage` 与 `activity.usage_logs`：最近 10 条相关用量日志。
- `activity.events`：最近 10 条相关事件。

错误：无效 ID 返回 `400`，不存在返回 `404`。

### 6.3 创建后端

`POST /admin/api/backends`

请求体：

```json
{
  "name": "upstream-a",
  "protocol": "openai",
  "base_url": "https://api.example.com",
  "api_keys": [
    {
      "id": "56382",
      "key": "sk-upstream",
      "name": "production-key",
      "group": "default",
      "models": ["gpt-5", "gpt-4.*"],
      "model_mapping": {"gpt-5-client": "gpt-5"},
      "used_quota": 0
    }
  ],
  "console_url": "https://console.example.com",
  "tags": ["primary"],
  "console_username": "admin",
  "console_password": "password",
  "console_checkin_workflow_id": "relay-default-checkin",
  "manual_checkin": false,
  "console_headers": {"Authorization": "Bearer ..."},
  "notes": "main backend",
  "proxy_id": 0,
  "status": "normal",
  "weight": 10,
  "api_key": "",
  "models": [],
  "model_mapping": {},
  "endpoints": []
}
```

字段和校验说明：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `name` | 是 | 数据库唯一；当前处理器没有非空校验，因此首次提交空字符串可能被保存，不应依赖该行为 |
| `protocol` | 否 | `anthropic`/`claude` -> `anthropic`；`both`/`dual` 等 -> `both`；其他值 -> `openai` |
| `base_url` | 是 | 必须是包含主机的 `http` 或 `https` URL |
| `api_keys` | 条件必填 | 至少一项；每项必须有 `key`、`group` 和非空 `models`；读取时会返回 `id` |
| `api_key`、`models`、`model_mapping` | 兼容字段 | `api_keys` 为空时，可用这三个旧字段构造默认分组 Key |
| `console_url` | 否 | 非空时必须是合法 HTTP(S) URL |
| `proxy_id` | 否 | `0` 为直连；大于 `0` 时代理必须存在；不能小于 `0` |
| `weight` | 否 | 小于 `1` 时归一化为 `1` |

控制台字段处理：

- `console_headers` 保存工作流和 Chrome CDP 使用的控制台请求头。
- `console_headers` 的名称和值不能为空，名称必须是合法 HTTP Header 名，值不能含非法控制字符。
- `console_checkin_workflow_id` 非空时启用签到；工作流输出负责更新账户、API Key 和模型价格。
- `manual_checkin` 为布尔值，默认 `false`；每次签到工作流执行都会将其作为 `$runtime.manual_checkin` 和 `{{runtime#/manual_checkin}}` 提供。
- 请求中的 `status` 和 `endpoints` 当前会被解析但不会用于创建；新后端状态固定归一化为 `normal`。

响应 `201`：创建后的统一前端 `Backend`。

### 6.4 更新后端

`PUT /admin/api/backends/{id}`

路径参数：`id` 必须为正整数。

请求体：所有字段均可选，只更新显式提供的字段。

```json
{
  "name": "upstream-a-new",
  "protocol": "both",
  "base_url": "https://api.example.com/v1",
  "api_keys": [],
  "console_url": "https://console.example.com",
  "tags": ["primary", "updated"],
  "console_username": "admin",
  "console_password": "new-password",
  "console_checkin_workflow_id": "relay-default-checkin",
  "manual_checkin": true,
  "console_headers": {"Cookie": "session=..."},
  "notes": "updated",
  "proxy_id": 1,
  "status": "disabled",
  "weight": 20,
  "api_key": "legacy-key",
  "models": ["gpt-5"],
  "model_mapping": {},
  "endpoints": []
}
```

规则：

- `api_keys` 存在时按完整数组替换并执行与创建相同的校验。
- 未提供 `api_keys` 时，可以使用旧字段更新第一条 API Key；空的 `api_key` 不会清除旧值。
- `status` 仅允许 `normal`、`disabled` 或空字符串；不能手工设为 `abnormal`。状态改变时会清零连续失败次数和恢复时间。
- `weight` 小于 `1` 时归一化为 `1`。
- `endpoints` 当前被解析但不会写入更新补丁。
- 如果请求体 `{}`，返回当前对象，不产生字段变更。

响应 `200`：更新后的统一前端 `Backend`。

错误：无效 ID/字段返回 `400`，不存在返回 `404`。

### 6.5 删除后端

`DELETE /admin/api/backends/{id}`

请求：无请求体。

响应 `200`：

```json
{
  "deleted": 1
}
```

### 6.6 导出后端

`GET /admin/api/backends/export`

请求：无请求体。

响应头：

```http
Content-Disposition: attachment; filename="red-token-backends.json"
Content-Type: application/json
```

响应 `200`：

```json
{
  "backends": [
    {
      "name": "upstream-a",
      "protocol": "openai",
      "base_url": "https://api.example.com",
      "api_keys": [],
      "console_url": "https://console.example.com",
      "tags": [],
      "console_username": "admin",
      "console_password": "password",
      "console_headers": {},
      "console_account_json": "{}",
      "console_pricing_json": "{}",
      "notes": "",
      "proxy_id": 0,
      "status": "normal",
      "consecutive_failures": 0,
      "weight": 1
    }
  ]
}
```

导出内容包含未脱敏凭据，应按敏感文件处理。

### 6.7 导入后端

`POST /admin/api/backends/import`

请求体：与导出格式相同。

```json
{
  "backends": []
}
```

额外校验：

- 导入对象之间不能重名，也不能与现有后端重名，比较时忽略大小写和首尾空格。
- `status` 允许 `normal`、`abnormal`、`disabled`；空值按 `normal`。
- `consecutive_failures` 必须大于等于 `0`。
- 任一对象校验或写入失败时，整个事务回滚。

响应 `201`：

```json
{
  "imported": 2,
  "backends": []
}
```

`backends` 为导入后的 `Backend[]`。

### 6.8 同步单个后端控制台

`POST /admin/api/backends/{id}/console/sync`

请求体：无。

查询参数：

| 参数 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `stream` | string | 空 | 为 `1` 时返回 NDJSON 流 |
| `audit` | string | 空 | 为 `0` 时不写入同步成功审计事件 |

也可以通过 `Accept: application/x-ndjson` 启用流式响应。

前置条件：

- 后端存在且 `console_url` 为合法 HTTP(S) URL。
- 必须配置存在的 `console_checkin_workflow_id`。
- 工作流使用控制台请求头、用户名密码、Chrome CDP 写入的 Cookie/Authorization 和代理配置完成签到，并通过固定输出同步账户、API Key 与模型价格。

普通 JSON 响应 `200`：

```json
{
  "backend": {},
  "checkin": {},
  "account": {},
  "pricing": {},
  "requests": [
    {
      "time": "2026-08-05T08:30:00Z",
      "method": "GET",
      "path": "/api/account",
      "status_code": 200,
      "body": "{...}"
    }
  ]
}
```

`checkin` 是工作流固定输出，`account` 和 `pricing` 是写入中转站后的归一化快照。

NDJSON 响应：HTTP 状态在流建立时固定为 `200`，每行一个 JSON 对象。

上游请求事件：

```json
{"type":"request","request":{"time":"...","method":"GET","path":"/api/account","status_code":200,"body":"{...}"}}
```

工作流执行消息会按发生顺序实时写入同一响应流：

```json
{"type":"workflow_log","log":{"time":"...","level":"info","step_id":"account","phase":"step_start","message":"step execution started"}}
```

`workflow_log.log` 与工作流执行接口返回的 `debug_logs[]` 结构相同，包含步骤、阶段、级别、消息、耗时和可选详情。

完成事件：

```json
{"type":"complete","response":{"backend":{},"account":{},"pricing":{},"requests":[]}}
```

错误事件：

```json
{"type":"error","status":502,"message":"错误信息","requests":[]}
```

非流式主要错误：`400` 表示后端配置无效，`502` 表示控制台请求/响应失败，`500` 表示保存失败。

### 6.9 执行签到工作流

`POST /admin/api/backends/{id}/console/checkin`

请求体：无。

要求合法 `console_url` 和已绑定的 `console_checkin_workflow_id`。工作流使用中转站保存的 Headers、用户名、密码和 Chrome CDP 凭据。

响应 `200`：

```json
{
  "backend": {},
  "checkin": {},
  "account": {},
  "requests": []
}
```

`checkin` 是工作流固定输出，`account` 为归一化后的账户快照。

### 6.10 从 Chrome CDP 同步控制台登录凭据

`POST /admin/api/backends/{id}/console/cookie/sync`

请求体：无。服务端通过 Chrome DevTools Protocol 读取浏览器 Cookie 以及匹配页面的 `localStorage`、`sessionStorage`。Cookie 会按后端 `console_url` 的域名、路径、HTTPS 和过期条件过滤，然后替换 `console_headers.Cookie`；浏览器存储中的有效 JWT 访问令牌会更新 `console_headers.Authorization`。如果页面没有把令牌持久化到存储，服务端会临时启用 Network、刷新匹配页面并从同源实际请求头捕获 Bearer 令牌。未找到访问令牌时保留原 Authorization，其他控制台请求头保持不变；空 Cookie 会移除旧的 Cookie 请求头。响应和日志不会返回令牌值。

Chrome 运行在宿主机、后端运行在 WSL 时，默认使用 `http://127.0.0.1:9222`，并在连接失败后尝试 WSL 默认网关、`/etc/resolv.conf` 中的 nameserver 和 `host.docker.internal`。也可以通过环境变量 `RT_CHROME_CDP_URL` 指定 CDP HTTP 地址。宿主机 Chrome 必须以 `--remote-debugging-port=9222 --remote-debugging-address=0.0.0.0 --user-data-dir=<专用目录>` 启动，并允许防火墙访问该端口；Chrome 136 及以上版本要求远程调试使用非默认用户数据目录，因此需要在这个专用 Chrome 配置中登录中转站控制台。

响应 `200`：

```json
{
  "backend": {},
  "cookie_count": 3,
  "authorization_updated": true
}
```

`400` 表示后端不存在、没有合法 `console_url`；`502` 表示无法连接 CDP、WebSocket 握手或 Cookie 读取失败；`500` 表示保存失败。除非 `audit=0`，成功同步会写入 `admin_backend_cookie_sync` 审计事件。

### 6.12 记录全局同步摘要

`POST /admin/api/backends/console/sync-summary`

请求体：

```json
{
  "total": 3,
  "success_count": 2,
  "failure_count": 1
}
```

校验：`total > 0`，成功和失败数均不能为负数，且两者之和必须等于 `total`。

响应 `200`：原样返回上述对象，并写入一条全局同步审计事件。

### 6.13 后端小时模型统计

`GET /admin/api/backend-hourly-model-stats`

查询参数：

| 参数 | 类型 | 说明 |
| --- | --- | --- |
| `backend` | string | 精确匹配后端名称 |
| `model` | string | 精确匹配模型名称 |
| `start_hour` | string | RFC 3339 时间，转换到 UTC 后必须整点 |
| `end_hour` | string | RFC 3339 时间，转换到 UTC 后必须整点，且不能早于 `start_hour` |

响应 `200`：

```json
{
  "query": {
    "backend": "upstream-a",
    "model": "gpt-5",
    "start_hour": "2026-08-05T00:00:00Z",
    "end_hour": "2026-08-05T08:00:00Z"
  },
  "scope": {
    "backends": [{"id": 1, "name": "upstream-a"}],
    "models": ["gpt-5"],
    "time_range": {
      "start_hour": "2026-08-05T00:00:00Z",
      "end_hour": "2026-08-05T08:00:00Z",
      "timezone": "UTC"
    }
  },
  "items": [
    {
      "backend_id": 1,
      "backend": "upstream-a",
      "model": "gpt-5",
      "hour": "2026-08-05T08:00:00Z",
      "requests": 10,
      "successes": 9,
      "failures": 1,
      "input_tokens": 1000,
      "output_tokens": 500,
      "input_cache_tokens": 100,
      "success_avg_duration_ms": 820.5,
      "success_request_bytes": 12000,
      "success_response_bytes": 50000
    }
  ]
}
```

未提供的查询字段在响应中为 `null`。统计中的 Token、延迟和流量字段只累计成功请求。

## 7. HTTP 工作流管理

工作流配置使用 [`http-workflow/v1`](http_workflow.md) 至 `http-workflow/v4` 语义。需要基于当前响应状态码执行条件跳转时使用 v2 或更高版本；需要对数组 alias 逐项串行发送请求时使用 v3 或更高版本；需要工作流全局请求 Header 时使用 v4。创建和更新接口的请求体就是完整工作流定义，不需要再包一层字符串字段。数据库只保存通过语法、表达式编译以及固定签到 `output` 顶层字段校验后的规范化配置。

工作流执行时使用指定后端的 `console_url` 作为基础 URL，并自动应用该后端保存的控制台请求头、Cookie、SOCKS5 代理和全局控制台 User-Agent。工作流不得覆盖宿主提供的 `Authorization` 与 `Cookie`。每次执行使用独立 Cookie jar，响应 `Set-Cookie` 自动用于后续步骤；成功后 Cookie 变更会新增或覆盖所选中转站的 `console_headers.Cookie`。

### 7.1 工作流列表

`GET /admin/api/workflows`

支持通用 `page`、`limit` 分页参数。响应 `200`：

```json
{
  "items": [
    {
      "id": "relay-default-checkin",
      "name": "中转站默认签到",
      "definition": {
        "spec": "http-workflow/v4",
        "id": "relay-default-checkin",
        "name": "中转站默认签到",
        "headers": {},
        "steps": [],
        "output": {}
      },
      "created_at": "2026-08-10T08:00:00Z",
      "updated_at": "2026-08-10T08:00:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "limit": 10
}
```

`definition` 的完整字段及 jq 表达式规则见工作流规范。列表返回的 `items` 无数据时固定为 `[]`。

### 7.2 创建工作流

`POST /admin/api/workflows`

请求体是工作流定义本身：

```json
{
  "spec": "http-workflow/v4",
  "id": "relay-default-checkin",
  "name": "中转站默认签到",
  "headers": {
    "X-Workflow-Profile": "relay-default"
  },
  "steps": [
    {
      "id": "get_profile",
      "request": {
        "method": "GET",
        "path": "/api/v1/auth/me"
      },
      "extract": [
        { "alias": "user_id", "expression": ".data.id | tostring" }
      ]
    }
  ],
  "output": {
    "user_id": "{{user_id}}"
  }
}
```

上例只展示配置传输格式；可执行签到配置的 `output` 必须包含固定业务 Schema 的全部字段，完整示例见工作流规范。

响应 `201`：返回 7.1 中的单个工作流对象。定义非法、存在未知/重复字段、jq 编译失败时返回 `400`；相同 `id` 已存在时返回 `409`。

### 7.3 获取工作流

`GET /admin/api/workflows/{id}`

响应 `200`：返回 7.1 中的单个工作流对象。不存在返回 `404`。

### 7.4 更新工作流

`PUT /admin/api/workflows/{id}`

请求体是完整的新工作流定义，执行全量替换。路径 `id` 必须与定义中的 `id` 完全一致，稳定 ID 不可通过更新修改。

响应 `200`：返回更新后的工作流对象。ID 不一致或定义非法返回 `400`，工作流不存在返回 `404`。已有成功执行结果不会因修改配置自动删除；下一次成功执行会替换对应后端的旧结果。

### 7.5 删除工作流

`DELETE /admin/api/workflows/{id}`

响应 `200`：

```json
{
  "deleted": "relay-default-checkin"
}
```

工作流不存在返回 `404`。删除配置时会同时删除它在所有后端上的持久化结果。

### 7.6 在指定后端执行工作流

`POST /admin/api/workflows/{id}/execute`

请求体：

```json
{
  "backend_id": 1,
  "aliases": {}
}
```

- `backend_id`：必填正整数，指定执行请求所使用的后端/中转站。
- `aliases`：可选对象，作为本次运行的初始 alias store；名称和值仍受工作流规范约束。
- `$runtime` 除规范字段外还包含 `backend_id`、`backend_name`，并使用所选中转站填充 `username`、`password`、`user_id`、`manual_checkin` 和宿主提供的基础控制台 `headers` 字典。`manual_checkin` 是中转站配置的布尔值。jq 表达式通过 `$runtime.username`、`$runtime.manual_checkin` 等字段读取；请求模板通过 `{{runtime#/username}}`、`{{runtime#/password}}`、`{{runtime#/user_id}}`、`{{runtime#/manual_checkin}}` 和 `{{runtime#/headers/Header-Name}}` 读取。`runtime` 不会写入响应的 `aliases`。

响应 `200`：

```json
{
  "workflow_id": "relay-default-checkin",
  "backend": {
    "id": 1,
    "name": "relay-a"
  },
  "output": {
    "user_id": "user-1",
    "username": "alice@example.com",
    "quota": 2400,
    "quota_unit": "USD",
    "used_quota": 0,
    "today_reward": 123,
    "api_keys": [],
    "models": []
  },
  "aliases": {
    "user_id": "user-1"
  },
  "executed_at": "2026-08-10T08:05:00Z",
  "requests": [
    {
      "time": "2026-08-10T08:05:00Z",
      "method": "GET",
      "path": "/api/v1/auth/me",
      "status_code": 200,
      "body": "{...}"
    }
  ],
  "debug_logs": [
    {
      "time": "2026-08-10T08:05:00Z",
      "level": "debug",
      "step_id": "profile",
      "phase": "response",
      "message": "HTTP response received",
      "duration_ms": 128,
      "details": {
        "status_code": 200,
        "body": {"user_id": "user-1"}
      }
    }
  ]
}
```

只有所有 step 成功且 `output` 通过工作流规范中的固定签到 Schema 后，服务端才会在同一个事务中更新所选 backend 的控制台账户摘要、API Key 列表、模型价格缓存、响应 Cookie，并原子替换 `(workflow_id, backend_id)` 的上一次成功结果。失败前已经收到的响应 Cookie 也会独立更新到 `console_headers.Cookie`，但不会写入部分业务输出。已有 API Key 的 `models` 和 `model_mapping` 按 key 值原样保留，与工作流输出的模型列表及 `cheapest_groups` 无关；账户的 `quota`、`quota_unit`、`used_quota`、`today_reward`、`username`、`user_id` 和最近成功时间也会同步更新，旧的 `balance` 与 `total_actual_cost` 会被移除。若 `today_reward` 为 `0`，则保留账户和上一次成功结果中的原值。API Key 的 `used_quota` 和模型的 `price_type` 会同步写入后端运行数据。写入 backend 时会使用系统设置 `focus_models` 过滤价格缓存中的模型；该设置为空时不过滤。`aliases`、`requests` 和 `debug_logs` 只随本次响应返回，不属于持久化业务快照。

工作流或后端不存在返回 `404`；`backend_id`、`console_url` 或代理配置非法返回 `400`；传输、非 2xx 默认预期、JSON 解析、jq、模板或输出 Schema 失败返回 `502`。`502` 响应包含已执行的 `requests` 和逐阶段 `debug_logs`，不会覆盖旧的业务运行数据或成功结果，但已收到的响应 Cookie 会同步到中转站配置。调试日志覆盖工作流校验、请求渲染、HTTP 响应、expect、alias 提取、output 渲染与 Schema 校验；header、path（含 query）、请求 body 和响应 body 按原值返回，单项预览最多 64 KiB。

### 7.7 获取后端上的最近成功结果

`GET /admin/api/workflows/{id}/results/{backend_id}`

响应 `200`：

```json
{
  "workflow_id": "relay-default-checkin",
  "backend_id": 1,
  "output": {
    "user_id": "user-1",
    "username": "alice@example.com",
    "quota": 2400,
    "quota_unit": "USD",
    "used_quota": 0,
    "today_reward": 123,
    "api_keys": [],
    "models": []
  },
  "executed_at": "2026-08-10T08:05:00Z"
}
```

`backend_id` 非正整数返回 `400`，当前组合尚无成功结果返回 `404`。删除对应工作流或后端时，该结果会级联删除。

## 8. 客户端密钥管理

### 8.1 密钥列表

`GET /admin/api/client-keys`

请求：无请求体；支持通用分页参数。

响应 `200`：通用分页结构，`items` 每项包含 `ClientKey` 全部字段以及：

```json
{
  "masked_token": "tg-xxxxx...abcd",
  "usage_count": 120,
  "req_success": 115,
  "req_fail": 5,
  "token_input": 320000,
  "token_output": 96000,
  "last_used_at": "2026-08-05T08:30:00Z"
}
```

`usage_count` 等于 `req_success + req_fail`。请求次数和 Token 只按每个客户端请求的最终结果累计一次，不包含中间重试；累计值存储在客户端密钥记录中，清空使用日志不会重置。

注意：除 `masked_token` 外，嵌入的 `ClientKey.token` 仍可能包含完整 Token。

### 8.2 密钥详情

`GET /admin/api/client-keys/{id}/detail`

请求：无请求体。

响应 `200`：资源详情格式。

- `overview`：名称、启用状态、Token 前缀、累计成功/失败次数、累计输入/输出 Token、使用次数和最后使用时间。
- `configuration`：包含完整 Token。
- `raw`：完整 `ClientKey`。
- `activity.usage` 与 `activity.usage_logs`：最近 10 条相关用量日志。
- `activity.events`：最近 10 条相关事件。

### 8.3 创建客户端密钥

`POST /admin/api/client-keys`

请求体：

```json
{
  "name": "production",
  "token": "",
  "allowed_models": "gpt-5,gpt-4.1",
  "enabled": true
}
```

- `token` 为空时生成 `tg-` 开头、基于 24 字节随机数的 URL-safe Token。
- 非空 `token` 会按原值使用，数据库保存 SHA-256 哈希用于鉴权，同时也保存原始 Token 用于管理界面展示。
- Token 哈希必须唯一。

响应 `201`：

```json
{
  "client": {},
  "issued_token": "tg-xxxxxxxx"
}
```

`client` 为创建后的 `ClientKey`；`issued_token` 是本次实际签发或使用的完整 Token。

### 8.4 更新客户端密钥

`PUT /admin/api/client-keys/{id}`

请求体：

```json
{
  "name": "production-new",
  "token": "ignored-value",
  "allowed_models": "gpt-5",
  "enabled": false
}
```

所有非 Token 字段按完整对象覆盖。当前实现会解析但忽略 `token`，不能通过此接口轮换或清除 Token。

响应 `200`：

```json
{
  "client": {},
  "issued_token": ""
}
```

### 8.5 删除客户端密钥

`DELETE /admin/api/client-keys/{id}`

请求：无请求体。

响应 `200`：

```json
{
  "deleted": 1
}
```

## 9. 审计事件

### 9.1 事件列表

`GET /admin/api/events`

请求：无请求体；支持通用分页参数及以下过滤参数：

| 参数 | 类型 | 说明 |
| --- | --- | --- |
| `category` | string | 精确匹配 `category` 或 `type` |
| `severity` | string | 匹配 `severity` 或 `level`；`warn` 与 `warning` 视为同类 |
| `actor` | string | 精确匹配 `actor` 或 `client_name` |
| `backend` | string | 精确匹配 `backend_name` |
| `q` | string | 模糊搜索类型、分类、消息、客户端、模型和后端 |
| `date_from` | string | RFC 3339，包含边界 |
| `date_to` | string | RFC 3339，包含边界 |

无法解析的日期会被静默忽略。

响应 `200`：通用分页结构，`items` 为 `AuditEvent[]`。

### 9.2 事件摘要

`GET /admin/api/events/summary`

查询参数：与事件列表的过滤参数相同，不使用分页参数。

响应 `200`：

```json
{
  "total": 20,
  "categories": [{"category": "admin_backend_sync", "count": 5}],
  "severities": [{"severity": "info", "count": 18}],
  "actors": [{"actor": "admin", "count": 10}],
  "time_series": []
}
```

`time_series` 当前固定为空数组。

### 9.3 事件详情

`GET /admin/api/events/{id}`

请求：无请求体。

响应 `200`：

```json
{
  "overview": {
    "type": "admin_backend_sync",
    "message": "backend console synced: upstream-a",
    "category": "admin_backend_sync",
    "severity": "info",
    "actor": "admin",
    "backend": "upstream-a",
    "client_name": "",
    "model": "",
    "endpoint": ""
  },
  "configuration": {},
  "metadata": {
    "id": 1,
    "created_at": "2026-08-05T08:30:00Z",
    "resource_type": "backend",
    "resource_id": 2
  },
  "raw": {},
  "activity": {}
}
```

`raw` 为完整 `AuditEvent`。

### 9.4 清空事件

`DELETE /admin/api/events`

请求：无请求体、无过滤参数；会删除全部审计事件。

响应 `200`：

```json
{
  "cleared": true,
  "deleted": 120
}
```

## 10. 用量日志

### 10.1 日志列表

`GET /admin/api/usage-logs`

请求：无请求体；支持通用分页参数及以下过滤参数：

| 参数 | 类型 | 说明 |
| --- | --- | --- |
| `backend` | string | 精确匹配后端名称 |
| `model` | string | 精确匹配模型 |
| `client_key` | string | 精确匹配客户端名称，不是密钥值 |
| `proxy` | string | 精确匹配代理名称；直连通常为 `direct` |
| `status` | string | 仅允许 `2xx`、`3xx`、`4xx`、`5xx` |
| `q` | string | 模糊搜索请求 ID、Trace ID、路径、客户端、模型、后端和错误信息 |
| `date_from` | string | RFC 3339，包含边界 |
| `date_to` | string | RFC 3339，包含边界 |

无效 `status` 返回 `400`；无法解析的日期会被静默忽略。

响应 `200`：通用分页结构，`items` 为 `UsageLog[]`。

### 10.2 日志统计

`GET /admin/api/usage-logs/stats`

查询参数：与日志列表过滤参数相同，不使用分页参数。

响应 `200`：

```json
{
  "totals": {
    "requests": 100,
    "successes": 95,
    "failures": 5
  },
  "latency": {
    "avg_ms": 820.5,
    "p95_ms": 1500
  },
  "status_families": [
    {"family": "2xx", "count": 95},
    {"family": "5xx", "count": 5}
  ]
}
```

统计实现中 `successes` 计入 `2xx` 和 `3xx`，而 `failures` 按“非 `2xx`”判断，因此 `3xx` 会同时计入成功数和失败数；各状态族的原始分布以 `status_families` 为准。

### 10.3 日志详情

`GET /admin/api/usage-logs/{id}`

请求：无请求体。

响应 `200`：

```json
{
  "overview": {
    "request_id": "abc123",
    "status_code": 200,
    "backend": "upstream-a",
    "model": "gpt-5",
    "input_tokens": 20,
    "output_tokens": 50,
    "input_cache_tokens": 0
  },
  "request": {
    "bytes": 120,
    "body_preview": "{...}",
    "headers_json": "{...}",
    "method": "POST",
    "path": "/v1/responses",
    "query": ""
  },
  "response": {
    "bytes": 860,
    "body_preview": "{...}",
    "headers_json": "{...}",
    "status_family": "2xx",
    "is_stream": false
  },
  "metadata": {
    "id": 1,
    "trace_id": "abc123",
    "proxy_name": "direct",
    "preview_truncated": false,
    "created_at": "2026-08-05T08:30:00Z"
  },
  "raw": {}
}
```

`raw` 为完整 `UsageLog`。

### 10.4 日志过滤选项

`GET /admin/api/usage-log-options`

请求：无请求体。

响应 `200`：

```json
{
  "backends": ["upstream-a"],
  "models": ["gpt-5"],
  "client_keys": ["production"],
  "proxies": ["proxy-cn"]
}
```

所有数组均去重并按字符串升序排列。`proxies` 来源于已配置的 SOCKS5 代理，不会自动加入用量日志中的 `direct`。

### 10.5 清理日志

`DELETE /admin/api/usage-logs`

请求：无请求体；支持与日志列表相同的过滤参数。

- 不提供任何有效过滤条件时删除全部用量日志。
- 提供过滤条件时只删除匹配记录。

响应 `200`：

```json
{
  "cleared": true,
  "filter": {
    "BackendName": "upstream-a",
    "Model": "",
    "ClientName": "",
    "ProxyName": "",
    "Status": "5xx",
    "Query": "",
    "DateFrom": "0001-01-01T00:00:00Z",
    "DateTo": "0001-01-01T00:00:00Z"
  },
  "deleted": 5
}
```

注意：`filter` 直接序列化 Go 结构体，因此字段名为大写驼峰，不是查询参数使用的蛇形命名；未设置时间会序列化为 Go 零时间。

## 11. 配置管理

### 11.1 获取配置

`GET /admin/api/config`

请求：无请求体。

响应 `200`：所有值均为字符串。

```json
{
  "listen_addr": ":4000",
  "db_path": "./red-token.db",
  "log_level": "info",
  "backend_cooldown": "20m0s",
  "backend_fails": "3",
  "backend_console_user_agent": "Red-Token/1.0",
  "focus_models": "",
  "request_timeout": "2m0s",
  "shutdown_timeout": "30s"
}
```

值优先来自数据库设置；数据库没有非空值时回退到启动配置或环境变量。

### 11.2 更新配置

`PUT /admin/api/config`

请求体：`object<string,string>`，可只提供需要更新的键。

```json
{
  "log_level": "debug",
  "backend_cooldown": "10m",
  "backend_fails": "5",
  "backend_console_user_agent": "Red-Token/1.0",
  "focus_models": "gpt-5,gpt-4.*",
  "request_timeout": "90s",
  "shutdown_timeout": "30s"
}
```

已知字段校验：

| 字段 | 校验 |
| --- | --- |
| `log_level` | `debug`、`info`、`warn`、`warning`、`error` |
| `backend_cooldown` | Go `time.ParseDuration` 格式，例如 `20m` |
| `backend_fails` | 可被解析为整数；当前没有正数范围校验 |
| `backend_console_user_agent` | 去除首尾空格后非空、最多 512 字符且不能含 CR/LF |
| `request_timeout` | Go Duration 格式 |
| `shutdown_timeout` | Go Duration 格式 |

请求体使用字符串映射，因此未知键不会被 JSON 解码器拒绝，并会写入数据库，但获取配置接口不会返回未列出的键。

响应 `200`：与获取配置接口格式相同，返回更新后的配置视图。

运行时说明：`log_level` 会立即更新日志级别；其他字段会更新数据库和部分内存配置。监听地址、数据库路径和关闭超时需要重启才能实际改变进程行为；调度器和已创建的代理客户端也可能继续使用启动时参数，建议修改关键运行参数后重启服务。

### 11.3 重新加载配置

`POST /admin/api/config/reload`

请求：无请求体。

从数据库重新载入以下字段到内存配置：

- `log_level`
- `backend_cooldown`
- `backend_fails`
- `backend_console_user_agent`
- `focus_models`
- `request_timeout`

响应 `200`：

```json
{
  "status": "reloaded"
}
```

与更新接口相同，重新载入不重建调度器、HTTP 代理服务或监听中的 HTTP Server；需要完整生效时应重启进程。

## 12. 接口清单

| 方法 | 路径 | 功能 |
| --- | --- | --- |
| `GET` | `/healthz` | 健康检查 |
| `GET` | `/` | 重定向管理页面 |
| `GET` | `/v1/models` | 获取可用模型 |
| `POST` | `/v1/chat/completions` | Chat Completions 代理 |
| `POST` | `/v1/responses` | Responses 代理 |
| `POST` | `/v1/embeddings` | Embeddings 代理 |
| `POST` | `/v1/images/generations` | Images 代理 |
| `POST` | `/v1/messages` | Anthropic Messages 代理 |
| `POST` | `/v1/messages/count_tokens` | Anthropic Token Counting 代理 |
| `GET` | `/admin/api/overview` | 总览 |
| `GET` | `/admin/api/dashboard/summary` | 仪表盘摘要 |
| `GET` | `/admin/api/dashboard/usage` | 仪表盘用量序列 |
| `GET` | `/admin/api/dashboard/activity` | 最近活动 |
| `GET` | `/admin/api/search` | 全局搜索 |
| `GET` | `/admin/api/socks-proxies` | 代理列表 |
| `GET` | `/admin/api/socks-proxies/{id}/detail` | 代理详情 |
| `POST` | `/admin/api/socks-proxies` | 创建代理 |
| `PUT` | `/admin/api/socks-proxies/{id}` | 更新代理 |
| `DELETE` | `/admin/api/socks-proxies/{id}` | 删除代理 |
| `GET` | `/admin/api/backends` | 后端列表 |
| `GET` | `/admin/api/backends/export` | 导出后端 |
| `GET` | `/admin/api/backends/{id}/detail` | 后端详情 |
| `POST` | `/admin/api/backends` | 创建后端 |
| `POST` | `/admin/api/backends/console/sync-summary` | 记录全局同步摘要 |
| `POST` | `/admin/api/backends/{id}/console/sync` | 同步后端控制台 |
| `POST` | `/admin/api/backends/{id}/console/cookie/sync` | 从 Chrome CDP 同步控制台登录凭据 |
| `POST` | `/admin/api/backends/{id}/console/checkin` | 执行签到工作流 |
| `POST` | `/admin/api/backends/import` | 导入后端 |
| `PUT` | `/admin/api/backends/{id}` | 更新后端 |
| `DELETE` | `/admin/api/backends/{id}` | 删除后端 |
| `GET` | `/admin/api/workflows` | 工作流列表 |
| `POST` | `/admin/api/workflows` | 创建工作流 |
| `GET` | `/admin/api/workflows/{id}` | 获取工作流 |
| `PUT` | `/admin/api/workflows/{id}` | 更新工作流 |
| `DELETE` | `/admin/api/workflows/{id}` | 删除工作流 |
| `POST` | `/admin/api/workflows/{id}/execute` | 在指定后端执行工作流 |
| `GET` | `/admin/api/workflows/{id}/results/{backend_id}` | 获取最近成功结果 |
| `GET` | `/admin/api/client-keys` | 客户端密钥列表 |
| `GET` | `/admin/api/client-keys/{id}/detail` | 客户端密钥详情 |
| `POST` | `/admin/api/client-keys` | 创建客户端密钥 |
| `PUT` | `/admin/api/client-keys/{id}` | 更新客户端密钥 |
| `DELETE` | `/admin/api/client-keys/{id}` | 删除客户端密钥 |
| `GET` | `/admin/api/events` | 事件列表 |
| `GET` | `/admin/api/events/summary` | 事件摘要 |
| `GET` | `/admin/api/events/{id}` | 事件详情 |
| `DELETE` | `/admin/api/events` | 清空事件 |
| `GET` | `/admin/api/usage-logs` | 用量日志列表 |
| `GET` | `/admin/api/usage-logs/stats` | 用量日志统计 |
| `GET` | `/admin/api/backend-hourly-model-stats` | 后端小时模型统计 |
| `GET` | `/admin/api/usage-logs/{id}` | 用量日志详情 |
| `GET` | `/admin/api/usage-log-options` | 用量日志过滤选项 |
| `DELETE` | `/admin/api/usage-logs` | 清理用量日志 |
| `GET` | `/admin/api/config` | 获取配置 |
| `PUT` | `/admin/api/config` | 更新配置 |
| `POST` | `/admin/api/config/reload` | 重新加载配置 |
