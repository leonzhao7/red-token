# HTTP 工作流系统

## 概述

HTTP 工作流是一个声明式的自动化系统，用于通过一系列 HTTP 请求从后端控制台获取数据并提取所需信息。工作流主要用于签到场景，自动化获取用户信息、配额、API 密钥和模型定价等数据。

## 工作流定义结构

### 基本格式

```json
{
  "spec": "http-workflow/v5",
  "id": "workflow-id",
  "name": "工作流名称",
  "headers": {},
  "steps": [],
  "output": {}
}
```

### 字段说明

- **spec**: 工作流规范版本，当前支持 v1-v5
  - `v1`: 基础版本，支持字符串 expect
  - `v2`: 添加结构化 expect 和 when 条件跳转
  - `v3`: 添加 foreach 循环
  - `v4`: 添加全局 headers
  - `v5`: 添加步骤级 headers

- **id**: 工作流唯一标识符，格式为 `[a-z][a-z0-9_-]{0,63}`

- **name**: 工作流可读名称

- **headers**: 全局请求头（v4+），应用于所有步骤，可被步骤级 headers 覆盖

- **steps**: 工作流步骤数组

- **output**: 输出模板，定义工作流最终返回的数据结构

## 工作流步骤

### 步骤结构

```json
{
  "id": "step-id",
  "foreach": {},
  "request": {},
  "expect": {},
  "when": {},
  "extract": []
}
```

### 请求定义 (request)

```json
{
  "method": "GET",
  "path": "/api/user",
  "headers": {},
  "body": {}
}
```

- **method**: HTTP 方法（GET、POST、PUT、DELETE 等）
- **path**: 请求路径，必须以 `/` 开头，支持模板变量
- **headers**: 步骤级请求头（v5+），覆盖全局 headers
- **body**: 请求体（可选），支持任意 JSON 值

### 响应验证 (expect)

#### 字符串形式 (v1)

```json
"expect": "$response.status >= 200 and $response.status < 300"
```

使用 jq 表达式验证响应，必须返回 `true`。

#### 结构化形式 (v2+)

```json
"expect": {
  "routes": [
    {
      "statuses": [401, 403],
      "goto": "login"
    }
  ],
  "accepted_statuses": [200, 201]
}
```

- **routes**: 状态码路由规则，匹配时跳转到指定步骤
- **accepted_statuses**: 可接受的状态码列表（除 2xx 外）

### 条件跳转 (when)

```json
"when": {
  "expression": "$vars.need_login",
  "goto": "login"
}
```

在提取别名后评估条件，如果表达式返回 `true` 则跳转到指定步骤。

### 数据提取 (extract)

```json
"extract": [
  {
    "alias": "user_id",
    "expression": "$response.body.data.id"
  },
  {
    "alias": "username",
    "expression": "$response.body.data.name"
  }
]
```

使用 jq 表达式从响应中提取数据并保存到别名变量。

### 循环迭代 (foreach)

```json
"foreach": {
  "alias": "items",
  "as": "item",
  "index_as": "index"
}
```

对别名变量中的数组进行迭代，每次迭代执行一次请求：

- **alias**: 源数组别名
- **as**: 当前元素的别名
- **index_as**: 当前索引的别名（可选）

在 foreach 步骤中：
- 请求会对数组每个元素执行一次
- extract 提取的值会聚合成数组
- 限制：最多 1000 次迭代

## 模板系统

### 变量引用

使用 `{{alias}}` 或 `{{alias#/pointer}}` 引用变量：

```json
{
  "path": "/api/users/{{user_id}}",
  "body": {
    "name": "{{username}}",
    "email": "{{user_id#/contact/email}}"
  }
}
```

### 特殊变量

- `{{runtime}}`: 运行时上下文
  - `{{runtime#/username}}`: 后端用户名
  - `{{runtime#/password}}`: 后端密码
  - `{{runtime#/user_id}}`: 控制台账户 ID
  - `{{runtime#/headers}}`: 运行时请求头
  - `{{runtime#/refresh_token}}`: 刷新令牌
  - `{{runtime#/backend_id}}`: 后端 ID
  - `{{runtime#/backend_name}}`: 后端名称
  - `{{runtime#/workflow_id}}`: 工作流 ID
  - `{{runtime#/started_at}}`: 执行开始时间
  - `{{runtime#/started_at_ms}}`: 执行开始时间戳

### JSON Pointer

使用 RFC 6901 JSON Pointer 语法访问嵌套值：
- `/field` - 访问对象字段
- `/0` - 访问数组第一个元素
- `~0` - 转义 `~`
- `~1` - 转义 `/`

### 模板规则

1. **类型保持**: `{{alias}}` 单独使用时保持原始类型
2. **字符串插值**: 与文本混合时转换为字符串
3. **转义**: 使用 `\{{` 输出字面量 `{{`
4. **null 处理**: header 值为 null 时从请求中删除该 header

## jq 表达式

### 可用变量

- `$response`: 响应信封
  - `$response.status`: HTTP 状态码
  - `$response.headers`: 响应头对象
  - `$response.has_body`: 是否有响应体
  - `$response.body`: 响应体（JSON）
  - `$response.text`: 响应文本

- `$request`: 请求信封
  - `$request.method`: HTTP 方法
  - `$request.path`: 请求路径
  - `$request.headers`: 请求头对象
  - `$request.has_body`: 是否有请求体
  - `$request.body`: 请求体

- `$vars`: 所有提取的别名变量

- `$runtime`: 运行时上下文

### 限制

- 禁用函数：`$ENV`、`debug`、`env`、`halt`、`halt_error`、`input`、`inputs`、`now`、`stderr`
- 禁用模块导入
- 执行超时：5 秒

## Output 约定

### 签到工作流 Output 格式

签到工作流必须输出以下格式的数据：

```json
{
  "user_id": "string",
  "username": "string",
  "quota": number,
  "quota_unit": "string",
  "used_quota": number,
  "today_reward": number,
  "api_keys": [
    {
      "id": "string",
      "name": "string",
      "key": "string",
      "group": "string",
      "used_quota": number
    }
  ],
  "models": [
    {
      "name": "string",
      "cheapest_groups": ["string"],
      "in_price": number,
      "out_price": number,
      "price": number,
      "price_type": 0 | 1
    }
  ],
  "refresh_token": "string",
  "console_headers": {
    "header-name": "value"
  }
}
```

#### 必填字段

- **user_id**: 用户 ID（字符串）
- **username**: 用户名（字符串）
- **quota**: 总配额（数值）
- **quota_unit**: 配额单位（字符串）
- **used_quota**: 已使用配额（非负数值）
- **today_reward**: 今日奖励（非负数值）
- **api_keys**: API 密钥数组
- **models**: 模型定价数组

#### 可选字段

- **refresh_token**: 刷新令牌，执行后更新到后端配置
- **console_headers**: 控制台请求头，执行后合并到后端配置

#### API Key 字段

- **id**: API 密钥 ID（可为空字符串）
- **name**: 密钥名称
- **key**: 密钥值（必填）
- **group**: 密钥组（默认 "default"）
- **used_quota**: 已使用配额（非负数值）

约束：
- 非空 ID 不能重复
- key 字段必须非空

#### Model 字段

- **name**: 模型名称（必填，不能重复）
- **cheapest_groups**: 最便宜的密钥组列表（数组，不能重复）
- **price_type**: 计费类型（0=按 token，1=固定价格）
- **in_price**: 输入价格（price_type=0 时至少提供一个价格字段）
- **out_price**: 输出价格
- **price**: 固定价格（price_type=1 时使用）

价格规则：
- 所有价格必须为非负数值
- `price_type=0`（按 token）：至少提供 in_price、out_price、price 之一
- `price_type=1`（固定）：使用 price 字段

### Output 验证

系统会在工作流执行后验证 output 格式：
1. 类型检查：确保字段类型正确
2. 必填检查：确保必填字段存在
3. 数值检查：确保数值有效且非负
4. 唯一性检查：确保 ID、名称不重复

验证失败会返回详细的错误路径和类型信息。

### Output 特殊处理

- **refresh_token** 和 **console_headers** 在验证前被提取并从 output 中移除
- 执行成功后，这两个字段会更新到后端配置中
- 其余 output 字段会被保存为快照，并用于更新后端的配额、模型、API 密钥信息

## 执行流程

1. **验证**: 编译工作流定义和所有 jq 表达式
2. **初始化**: 设置 HTTP 客户端、Cookie Jar、运行时变量
3. **步骤执行**:
   - 渲染请求模板
   - 发送 HTTP 请求
   - 验证响应（expect）
   - 提取数据（extract）
   - 条件判断（when）
4. **输出渲染**: 使用最终别名变量渲染 output 模板
5. **持久化**: 验证并保存 output，更新后端配置

### 控制流

- 步骤按顺序执行，除非发生跳转
- `expect.routes` 匹配时立即跳转，跳过 extract 和 when
- `when` 在 extract 之后评估
- 防止死循环：单个步骤访问次数限制为 100 次
- foreach 限制：单个步骤最多迭代 1000 次

## 安全限制

### 请求限制

- 响应体大小：默认 10 MB
- HTTP 超时：默认 30 秒
- 仅支持 http/https 协议
- Base URL 不能包含用户信息、查询参数或片段

### Header 保护

默认保护的 header（不可覆盖）：
- `Content-Length`
- `Host`
- `User-Agent`

### 数值安全

- 整数范围：`[-2^53+1, 2^53-1]`（JavaScript 安全整数）
- 禁止 NaN 和 Infinity
- JSON 解码严格模式（禁止重复键、多余值）

## 调试日志

工作流执行时会生成详细的调试日志：

```typescript
interface WorkflowDebugLog {
  time: string           // ISO 8601 时间戳
  level: string          // debug, info, warn, error
  step_id?: string       // 步骤 ID
  phase: string          // 执行阶段
  message: string        // 日志消息
  duration_ms?: number   // 持续时间（毫秒）
  details?: object       // 详细信息
}
```

### 主要阶段

- `workflow_start`: 工作流开始
- `validation`: 验证通过
- `step_start`: 步骤开始
- `request`: 请求已发送
- `response`: 响应已接收
- `expect`: 响应验证
- `extract`: 数据提取
- `foreach_iteration`: 循环迭代
- `step_complete`: 步骤完成
- `output_render`: 输出渲染
- `workflow_complete`: 工作流完成

## API 接口

### 列表工作流
```
GET /admin/api/workflows
```

### 创建工作流
```
POST /admin/api/workflows
Content-Type: application/json

{工作流定义}
```

### 获取工作流
```
GET /admin/api/workflows/{id}
```

### 更新工作流
```
PUT /admin/api/workflows/{id}
Content-Type: application/json

{工作流定义}
```

### 删除工作流
```
DELETE /admin/api/workflows/{id}
```

### 执行工作流
```
POST /admin/api/workflows/{id}/execute
Content-Type: application/json

{
  "backend_id": 123,
  "aliases": {
    "custom_var": "value"
  }
}
```

响应：
```json
{
  "workflow_id": "workflow-id",
  "backend": {
    "id": 123,
    "name": "后端名称"
  },
  "output": {},
  "aliases": {},
  "executed_at": "2024-01-01T00:00:00Z",
  "requests": [],
  "debug_logs": []
}
```

### 获取工作流结果
```
GET /admin/api/workflows/{id}/results/{backend_id}
```

## 最佳实践

1. **使用语义化的步骤 ID**: 便于调试和日志分析
2. **合理使用 foreach**: 避免过大的数组导致超时
3. **渐进式验证**: 在关键步骤添加 expect 检查
4. **提取关键数据**: 只提取需要的字段，避免传递整个响应
5. **使用 when 处理分支**: 根据响应内容决定后续流程
6. **利用全局 headers**: 避免在每个步骤重复定义通用 header
7. **保持 output 简洁**: 只包含必要的业务数据
8. **合理设置超时**: 根据后端响应时间调整客户端配置
9. **处理 refresh_token**: 在需要时更新认证令牌
10. **记录 console_headers**: 保存需要持久化的请求头

## 版本兼容性

选择合适的 spec 版本：
- 只使用基础功能 → `v1`
- 需要条件跳转 → `v2`
- 需要循环处理 → `v3`
- 需要全局 headers → `v4`
- 需要步骤级 headers → `v5`

系统会根据使用的功能自动检查 spec 版本是否兼容。
