# HTTP JSON 工作流配置规范

本文档定义 HTTP JSON 工作流配置的纯语义规则。它不约束具体编程语言、存储方式、HTTP 客户端或表达式执行器的实现。

本文中的“必须”“不得”“应当”“可以”分别表示强制要求、禁止要求、推荐要求和可选能力。

## 1. 设计目标

HTTP 工作流用于顺序执行一组 HTTP 请求，从 JSON 响应中提取值，将值保存为 alias，并在后续请求或最终输出中引用。

规范遵循以下原则：

- 所有 step 使用同一组字段和同一套执行规则。
- JSON 查询、投影、过滤、关联、聚合和重组统一使用 jq 表达式。
- alias 是不可细分赋值的完整 JSON 值；不存在数组专用或对象专用的写入动作。
- `{{...}}` 只负责引用已有 alias，不承担查询、计算、循环或关联语义。
- 最终输出由固定模板生成，并由宿主定义的 JSON Schema 校验。
- 工作流只描述有限、顺序的 HTTP JSON 请求，不描述定时调度、重试、并发、分支或任意循环。

分页可以由宿主生成多个静态 step，或由将来的工作流编排规范描述，不在本规范中增加分页专用字段。

## 2. 配置结构

工作流的最小结构如下：

```json
{
  "spec": "http-workflow/v1",
  "id": "example-workflow",
  "name": "示例工作流",
  "steps": [
    {
      "id": "get_profile",
      "name": "获取用户信息",
      "request": {
        "method": "GET",
        "path": "/api/profile",
        "query": {},
        "headers": {}
      },
      "expect": "$response.status >= 200 and $response.status < 300",
      "extract": [
        {
          "alias": "user_id",
          "expression": ".data.id | tostring"
        }
      ]
    }
  ],
  "output": {
    "user_id": "{{user_id}}"
  }
}
```

### 2.1 顶层字段

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| `spec` | string | 是 | 规范版本；本规范固定为 `http-workflow/v1` |
| `id` | string | 是 | 工作流稳定标识 |
| `name` | string | 是 | 展示名称，不参与执行 |
| `steps` | array | 是 | 按数组顺序执行的 step；可以为空 |
| `output` | JSON value | 是 | 递归 alias 模板；求值后必须符合宿主输出 Schema |

未知字段必须被拒绝。配置读取方不得根据未知字段猜测行为。

### 2.2 Step 字段

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| `id` | string | 是 | 当前工作流内唯一的稳定标识 |
| `name` | string | 是 | 展示名称，不参与执行 |
| `request` | object | 是 | HTTP 请求模板 |
| `expect` | string | 否 | jq 布尔表达式；省略时只接受 `200..299` |
| `extract` | array | 否 | 有序的 alias 赋值列表；省略等价于空数组 |

所有 step 具有完全相同的字段语义。不得根据 `step.id` 或 `step.name` 启用隐式行为。

### 2.3 Request 字段

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| `method` | string | 是 | HTTP method，执行前转为大写 |
| `path` | string | 是 | 相对 URL path 模板，必须以 `/` 开头且不得包含 scheme、authority、fragment 或 query |
| `query` | object | 否 | Query 参数模板，省略等价于 `{}` |
| `headers` | object | 否 | 请求头模板，省略等价于 `{}` |
| `body` | JSON value | 否 | JSON body 模板；字段省略表示不发送 body，字段存在时序列化其值 |

基础 URL、认证信息、代理、超时和受保护请求头由宿主执行环境提供，不属于工作流语义。

## 3. 标识符

工作流 ID 和 step ID 必须匹配：

```text
^[a-z][a-z0-9_-]{0,63}$
```

Alias 必须匹配：

```text
^[A-Za-z_][A-Za-z0-9_]{0,63}$
```

Alias 区分大小写。下列名称保留，不得作为 alias：

```text
response request runtime vars
```

## 4. 执行模型

一次运行拥有一个 alias store，初始值为宿主显式提供的输入 alias；未提供时为空对象。

执行过程如下：

1. 按 `steps` 数组顺序处理 step。
2. 使用当前 alias store 渲染 `request`。
3. 发送 HTTP 请求并读取完整响应。
4. 将非空响应体解析为 JSON。
5. 求值 `expect`。
6. 按顺序求值 `extract` 中的赋值。
7. 当前 step 的所有赋值成功后，一次性提交到 alias store。
8. 所有 step 成功后，使用 alias store 渲染 `output`。
9. 使用宿主输出 Schema 校验结果；校验成功后才允许持久化。

Step 是 alias store 的事务边界。当前 step 中较早的赋值对后续赋值可见，但任一赋值失败时，当前 step 的所有赋值都必须回滚。为避免修改只读输入，每次表达式求值都会得到一个新的 `$vars` 快照；它包含此前已成功求值的临时赋值，但表达式对 `$vars` 产生的任何 jq 内部更新都不会反向修改 alias store。

不同 step 可以对同一个 alias 再次赋值。再次赋值表示用新的完整 JSON 值替换旧值，不执行隐式合并、追加或按 ID 更新。需要合并时，应在 jq 表达式中构造合并后的完整值。

请求发送后不得回滚其外部副作用。因此需要副作用的请求应设计为幂等请求，或由上游提供幂等键。

## 5. Alias 模板

`request.path`、`request.query`、`request.headers`、`request.body` 和顶层 `output` 都是递归 alias 模板。

### 5.1 引用格式

Alias 引用使用以下格式：

```text
{{alias}}
{{alias#/json/pointer}}
```

第二种形式使用 RFC 6901 JSON Pointer 读取 alias 内部值：

```text
{{profile#/user/id}}
{{keys#/0/id}}
{{object#/a~1b/~0name}}
```

其中 `~1` 表示 `/`，`~0` 表示 `~`。

模板引用不支持通配符、过滤器、数组投影或计算。需要这些能力时，先通过 jq 表达式生成新的 alias，再引用该 alias。

### 5.2 整值引用

当一个字符串的全部内容只有一个 alias 引用时，引用结果保留原始 JSON 类型：

```json
{
  "ids": "{{key_ids}}",
  "profile": "{{profile}}",
  "enabled": "{{enabled}}"
}
```

如果 `key_ids` 是数组、`profile` 是对象、`enabled` 是布尔值，则渲染结果仍分别为数组、对象和布尔值，不得先转成字符串再解析。

引用值必须被深拷贝。模板求值不得修改 alias store 中的原值。

### 5.3 字符串插值

引用也可以嵌入普通字符串：

```json
{
  "Authorization": "Bearer {{access_token}}",
  "X-User": "user-{{user_id}}"
}
```

嵌入字符串时只允许 string、number 和 boolean。number 使用合法 JSON 数字文本，boolean 使用 `true` 或 `false`。null、array、object 或不存在的值都会导致模板错误。

模板替换本身不执行 URL 编码或 Header 编码。Query 在模板求值完成后由 HTTP 编码规则统一编码。

Path 中的 alias 也不会被自动编码。需要把任意字符串安全地放入单个 path segment 时，应先在 extract 表达式中使用 jq 的 `@uri` 生成已编码 alias，再将它用于 path。

要在模板字符串中输出一个可被识别为引用的字面量，配置中使用 `\\{{alias}}`；模板求值结果为字面量 `{{alias}}`。

### 5.4 对象键

对象键也可以包含字符串插值，但结果必须是非空字符串。同一个对象中的两个键在渲染后发生冲突时，模板求值失败，不得采用“后者覆盖前者”。

### 5.5 引用错误

以下情况必须导致模板求值失败：

- Alias 不存在。
- JSON Pointer 不存在或格式非法。
- 嵌入字符串的值不是允许的标量。
- 渲染后的对象出现重复键。
- 渲染结果包含非 JSON 值。

JSON null 是存在的值，不等同于 alias 或 JSON Pointer 不存在。

## 6. HTTP 请求语义

### 6.1 Path

`path` 只描述 URL path。Query 必须写入 `query`，从而避免 path 插值、URL 编码和参数重复之间的歧义。

Path 渲染完成后必须仍以 `/` 开头。宿主必须拒绝绝对 URL、协议相对 URL和包含 fragment 的结果。

### 6.2 Query

`query` 的每个渲染后值按以下规则编码：

| JSON 类型 | 编码规则 |
| --- | --- |
| string | 一个参数值 |
| number | 一个合法 JSON 数字文本 |
| boolean | `true` 或 `false` |
| array | 按数组顺序生成多个同名参数；元素只能是 string、number 或 boolean |
| null | 省略该参数 |
| object | 错误 |

参数名和值都必须按照标准 URL query percent-encoding 编码。对象成员顺序不影响语义；同名数组参数的值顺序必须保留。

### 6.3 Headers

Header 名不区分大小写。渲染后按以下规则处理：

| JSON 类型 | 编码规则 |
| --- | --- |
| string | 一个 Header 值 |
| number | 一个合法 JSON 数字文本 |
| boolean | `true` 或 `false` |
| array | 按数组顺序生成多个同名 Header；元素只能是 string、number 或 boolean |
| null | 省略该 Header |
| object | 错误 |

同一请求中大小写不同但语义相同的 Header 名视为冲突。宿主可以定义一组受保护 Header；工作流配置这些 Header 时必须在请求发送前失败。

当 `body` 字段存在且未显式配置 `Content-Type` 时，使用 `application/json`。未显式配置 `Accept` 时，使用 `application/json`。

### 6.4 Body

`body` 字段不存在时不发送请求 body。`body` 字段存在时，将渲染后的值序列化并发送，因此 `"body": null` 明确表示发送 JSON null。字段存在性与字段值不同，不得把存在且为 null 的 body 当成省略。

JSON body 可以是 object、array、string、number、boolean 或 null。它必须直接进行一次 JSON 序列化。不得要求用户把整个 body 写成经过转义的 JSON 字符串，也不得对序列化结果进行第二次模板替换。

## 7. HTTP 响应语义

非空响应体必须符合 RFC 8259 JSON。响应根节点可以是任意 JSON 值，不要求必须为 object。

解析器必须拒绝：

- 非法 UTF-8。
- 重复的对象键。
- 非法数字，包括 NaN 和 Infinity。
- 响应尾部除 JSON 空白外的其他数据。

空响应体使用 JSON null 作为 jq 输入，同时通过 `$response.has_body` 与真正的 JSON null 区分。

表达式环境中的 `$response` 具有固定结构：

```json
{
  "status": 200,
  "headers": {
    "content-type": ["application/json"]
  },
  "has_body": true,
  "body": {},
  "text": "{}"
}
```

- `status` 是 HTTP 状态码。
- `headers` 的名称统一转为小写，每个值始终为字符串数组。
- `has_body` 表示网络响应是否包含非空 body。
- `body` 是解析后的 JSON；空响应体时为 null。
- `text` 是解析前的 UTF-8 响应文本，用于审计，不应用于手工截取 JSON。

## 8. jq 表达式

`expect` 和 `extract[].expression` 遵循 jq 1.7 语义。不得再叠加自定义 JSONPath、字段映射、数组 action 或 join DSL。

### 8.1 求值上下文

每次 jq 求值具有以下输入和只读变量：

| 名称 | 值 |
| --- | --- |
| `.` | 当前解析后的响应 body；空响应体时为 null |
| `$response` | 第 7 节定义的响应对象 |
| `$request` | 完成模板求值后实际发送的 request 对象 |
| `$vars` | 当前可见 alias store 的快照 |
| `$runtime` | 宿主提供的运行元数据 |

`$runtime` 至少包含：

```json
{
  "workflow_id": "example-workflow",
  "started_at": "2026-08-10T00:00:00Z",
  "started_at_ms": 1786320000000
}
```

表达式不得读取环境变量、文件、网络、进程状态或其他外部可变状态。当前时间必须从 `$runtime` 获取，不得在表达式中再次读取时钟。

表达式必须是确定且无外部副作用的 jq 子集。必须禁用 `input`、`inputs`、`env`、`$ENV`、`now`、`debug`、`stderr`、`halt`、`halt_error`、模块导入以及其他依赖外部状态或改变进程行为的能力。`error` 可以用于显式终止当前表达式。

### 8.2 结果物化

jq filter 可能产生零个、一个或多个结果。Alias 赋值和 `expect` 都要求表达式恰好产生一个结果：

- 零个结果：错误。
- 一个结果：使用该 JSON 值。
- 多个结果：错误；如果需要数组，表达式必须显式使用 `[ ... ]` 收集结果。

结果必须是合法 JSON 值。jq error、非有限数字或其他不可物化值都会导致当前 step 失败。

JSON number 的计算必须避免实现相关的静默截断。实现至少应完整保留 IEEE 754 binary64 能精确表达的整数范围，即 `[-9007199254740991, 9007199254740991]`。超出实现精确范围且未以字符串表示的数字必须报错，不能舍入后继续执行。金额是否允许小数、保留多少位以及舍入方式属于最终输出 Schema 的业务约束。

### 8.3 Expect

省略 `expect` 等价于：

```jq
$response.status >= 200 and $response.status < 300
```

`expect` 必须恰好返回 JSON boolean `true` 才算成功。false、null、number、string、array 和 object 都算失败。`expect` 失败时不得执行 `extract`。

带业务状态码的响应可以写为：

```jq
$response.status >= 200
and $response.status < 300
and ((.code? // 0) == 0)
```

## 9. Extract 与 Alias 赋值

每个 extract 只有两个字段：

| 字段 | 类型 | 必填 | 语义 |
| --- | --- | --- | --- |
| `alias` | string | 是 | 被赋值的 alias 名称 |
| `expression` | string | 是 | 产生完整 alias 值的 jq 表达式 |

未知字段必须被拒绝。

Extract 按数组顺序执行。每次赋值完成后，后续表达式看到的 `$vars` 都包含这个新值：

```json
"extract": [
  {
    "alias": "items",
    "expression": ".data.items"
  },
  {
    "alias": "item_ids",
    "expression": "$vars.items | map(.id)"
  }
]
```

Alias 只能整体赋值。以下需求都通过 jq 返回新的完整值实现：

- 追加数组：`$vars.items + .data.items`
- 浅层合并对象：`$vars.profile + .data.profile`
- 递归合并对象：`$vars.profile * .data.profile`
- 更新嵌套路径：`$vars.profile | setpath(["user", "name"]; .data.name)`
- 删除字段：`$vars.profile | delpaths([["secret"]])`

规范没有 `type`、`required`、`default`、`fields`、`iterate`、`action`、`match` 或 `merge` 等 extract 专用关键字。这些行为由 jq 表达式和最终输出 Schema 表达。

## 10. JSON 解析模式

以下示例中，`.` 均表示当前响应 body。

### 10.1 标量、缺失值与类型转换

```jq
.data.id
.data.id | tostring
.data.balance | tonumber
.data.enabled | if type == "boolean" then . else error("enabled must be boolean") end
```

jq 的 `//` 会把 false 和 null 都视为需要回退。需要保留 false 时应显式检查对象是否包含字段：

```jq
.data as $data
| if $data | has("enabled") then $data.enabled else true end
```

JSON null 是普通值：

```jq
if .data.value == null then "fallback" else .data.value end
```

### 10.2 特殊字段名与动态键

字段名包含点、斜杠、空格或其他特殊字符时使用方括号：

```jq
.data["field.with.dot"]
.data["a/b"]
.data[$vars.dynamic_key]
```

不得通过拼接类似 `.data.{{key}}` 的表达式实现动态访问。

### 10.3 数组选择、过滤和投影

```jq
[.data.items[] | .id]
[.data.items[] | select(.enabled == true)]
[.data.items[] | {id: (.id | tostring), name, key, group: (.group.name // "default")}]
```

数组下标和切片：

```jq
.data.items[0]
.data.items[-1]
.data.items[2:5]
```

数组去重、排序和分组：

```jq
.data.items | unique_by(.id)
.data.items | sort_by(.created_at)
.data.items | sort_by(.group) | group_by(.group)
```

### 10.4 嵌套数组与扁平化

收集所有订单中的商品 ID：

```jq
[.data.orders[]?.items[]?.id]
```

只展开一层嵌套数组：

```jq
.data.matrix | flatten(1)
```

递归展开所有数组层级：

```jq
.data.matrix | flatten
```

保留父子关系时应显式构造对象：

```jq
[
  .data.orders[] as $order
  | $order.items[]
  | {order_id: $order.id, item_id: .id}
]
```

### 10.5 对象转数组与数组转对象

将动态 key 的对象转换为数组：

```jq
.data.stats
| to_entries
| map({
    id: .key,
    total_cost: (.value.total_actual_cost // 0)
  })
```

将数组按 ID 转成对象索引：

```jq
.data.items
| map({key: (.id | tostring), value: .})
| from_entries
```

重命名或过滤对象字段：

```jq
.data.user | {user_id: .id, username: (.email // .username)}
.data.user | with_entries(select(.key != "secret"))
```

### 10.6 按 ID 关联数组和对象

假设 `$vars.api_keys_base` 是 Key 数组，当前响应中的 `.data.stats` 是以 Key ID 为动态键的对象：

```jq
$vars.api_keys_base
| map(
    . as $key
    | $key + {
        total_cost: (
          $response.body.data.stats[($key.id | tostring)].total_actual_cost // 0
        )
      }
  )
```

两个数组可以先建立索引再关联：

```jq
INDEX(.data.usage[]; (.id | tostring)) as $usage
| $vars.api_keys_base
| map(
    . as $key
    | $key + {
        total_cost: ($usage[($key.id | tostring)].total_cost // 0)
      }
  )
```

这取代动态 path、`action: update`、`match` 和 step 专用 join 字段。

### 10.7 聚合

```jq
[.data.items[].total_cost] | add // 0
.data.items | map(.total_cost) | min
.data.items | map(.total_cost) | max
.data.items | length
.data.items | any(.enabled)
.data.items | all(.enabled)
```

按分组汇总：

```jq
.data.items
| sort_by(.group)
| group_by(.group)
| map({group: .[0].group, total_cost: (map(.total_cost) | add // 0)})
```

### 10.8 最小值和并列结果

为每个模型选择价格最低的所有 group：

```jq
$vars.model_rows
| map(
    . as $model
    | [
        $model.groups[]
        | {
            name: ., 
            ratio: ($vars.group_ratios[.] // 1)
          }
      ] as $groups
    | ($groups | map(.ratio) | min) as $min_ratio
    | {
        name: $model.name,
        cheapest_groups: [$groups[] | select(.ratio == $min_ratio) | .name],
        in_price: $model.in_price,
        out_price: $model.out_price
      }
  )
```

`argmin`、`ties` 等不需要成为配置关键字；它们是普通 JSON 变换。

### 10.9 递归搜索

当 JSON 层级不固定时可以使用递归下降，但必须明确筛选类型：

```jq
[.. | objects | .id? | select(. != null)]
```

已知结构时应优先使用确定路径。递归下降可能意外读取同名但语义不同的字段。

### 10.10 JSON 字符串字段

上游字段本身是 JSON 编码字符串时使用 `fromjson`：

```jq
.data.payload | fromjson
```

将 JSON 值编码为字符串时使用 `tojson`：

```jq
.data.payload | tojson
```

不得通过字符串截取、正则表达式或表达式求值来解析 JSON。

### 10.11 可选数据和错误

可选访问使用 `?`：

```jq
.data.profile?.email?
```

由于可选访问可能产生零个结果，Alias 赋值时通常需要显式提供一个值：

```jq
.data.profile?.email? // null
```

必填字段可以显式报错：

```jq
.data.id
| if . == null then error("data.id is required") else . end
```

谨慎使用 `try ... catch`。捕获错误后必须返回业务上明确的 JSON 值，不能静默丢弃数据。

## 11. 最终输出

所有 step 成功后，递归渲染顶层 `output`。输出不得直接读取最后一个 HTTP 响应，只能引用 alias，因此输出不依赖 step 排列之外的隐式状态。

sub2api 签到工作流的固定输出 Schema 为：

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "required": ["user_id", "username", "balance", "used_balance", "api_keys", "models"],
  "properties": {
    "user_id": { "type": "string" },
    "username": { "type": "string" },
    "balance": { "type": "number" },
    "used_balance": { "type": "number", "minimum": 0 },
    "api_keys": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["id", "name", "key", "group", "total_cost"],
        "properties": {
          "id": { "type": "string" },
          "name": { "type": "string" },
          "key": { "type": "string" },
          "group": { "type": "string" },
          "total_cost": { "type": "number", "minimum": 0 }
        }
      }
    },
    "models": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["name", "cheapest_groups", "in_price", "out_price"],
        "properties": {
          "name": { "type": "string" },
          "cheapest_groups": {
            "type": "array",
            "items": { "type": "string" },
            "uniqueItems": true
          },
          "in_price": { "type": "number", "minimum": 0 },
          "out_price": { "type": "number", "minimum": 0 }
        }
      }
    }
  }
}
```

约束如下：

- 顶层以及数组元素都不得包含未声明字段。
- `user_id`、`username`、Key 的 `id`、`name`、`key`、`group` 和模型 `name` 必须是字符串。
- `balance`、`used_balance`、`total_cost`、`in_price` 和 `out_price` 必须是有限 JSON number；JSON Schema 验证前还必须执行第 8.2 节的有限数检查。
- `api_keys` 和 `models` 必须始终是数组；无数据时使用空数组。
- `cheapest_groups` 必须始终是字符串数组；不存在可用分组时使用空数组。
- 同一个输出中，非空 Key ID 应当唯一，模型 name 应当唯一，单个模型中的 group 应当唯一。
- `total_cost`、`in_price` 和 `out_price` 不得小于零。

输出配置通常只负责字段装配：

```json
"output": {
  "user_id": "{{user_id}}",
  "username": "{{username}}",
  "balance": "{{balance}}",
  "used_balance": "{{used_balance}}",
  "api_keys": "{{api_keys}}",
  "models": "{{models}}"
}
```

签到时间、运行状态、工作流版本、错误日志和 HTTP 审计记录属于运行元数据，不属于上述业务输出。

## 12. 完整示例

以下示例用于展示配置语义。具体 endpoint 和响应字段必须按目标平台调整。

```json
{
  "spec": "http-workflow/v1",
  "id": "sub2api-default-checkin-profile",
  "name": "sub2api 默认签到",
  "steps": [
    {
      "id": "get_me",
      "name": "获取用户信息",
      "request": {
        "method": "GET",
        "path": "/api/v1/auth/me",
        "query": {},
        "headers": {}
      },
      "expect": "$response.status >= 200 and $response.status < 300 and ((.code? // 0) == 0)",
      "extract": [
        { "alias": "user_id", "expression": ".data.id | tostring" },
        { "alias": "username", "expression": ".data.email // .data.username // \"\"" },
        { "alias": "balance", "expression": "(.data.free_balance // .data.balance // 0) | tonumber" }
      ]
    },
    {
      "id": "get_keys",
      "name": "获取 API Key 列表",
      "request": {
        "method": "GET",
        "path": "/api/v1/keys",
        "query": {
          "page": 1,
          "page_size": 100,
          "scope": "personal"
        },
        "headers": {}
      },
      "extract": [
        {
          "alias": "api_keys_base",
          "expression": "[.data.items[] | {id: (.id | tostring), name: (.name // \"\"), key: (.key // \"\"), group: (.group.name // .group // \"default\")}]"
        },
        {
          "alias": "key_ids",
          "expression": "$vars.api_keys_base | map(.id)"
        }
      ]
    },
    {
      "id": "get_key_usage",
      "name": "获取 API Key 用量",
      "request": {
        "method": "POST",
        "path": "/api/v1/usage/dashboard/api-keys-usage",
        "query": {},
        "headers": {},
        "body": {
          "api_key_ids": "{{key_ids}}"
        }
      },
      "extract": [
        {
          "alias": "api_keys",
          "expression": "$vars.api_keys_base | map(. as $key | $key + {total_cost: (($response.body.data.stats[($key.id | tostring)].total_actual_cost // 0) | tonumber)})"
        },
        {
          "alias": "used_balance",
          "expression": "$vars.api_keys | map(.total_cost) | add // 0"
        }
      ]
    },
    {
      "id": "get_models",
      "name": "获取模型定价",
      "request": {
        "method": "GET",
        "path": "/api/v1/models",
        "query": {},
        "headers": {}
      },
      "extract": [
        {
          "alias": "group_ratios",
          "expression": ".group_ratio // {}"
        },
        {
          "alias": "model_rows",
          "expression": "[.data[] | {name: (.model_name // .name), groups: (.enable_groups // []), in_price: ((.input_price // 0) | tonumber), out_price: ((.output_price // 0) | tonumber)}]"
        },
        {
          "alias": "models",
          "expression": "$vars.model_rows | map(. as $model | [$model.groups[] | {name: ., ratio: ($vars.group_ratios[.] // 1)}] as $groups | ($groups | map(.ratio) | min // null) as $min_ratio | {name: $model.name, cheapest_groups: (if $min_ratio == null then [] else [$groups[] | select(.ratio == $min_ratio) | .name] end), in_price: $model.in_price, out_price: $model.out_price})"
        }
      ]
    }
  ],
  "output": {
    "user_id": "{{user_id}}",
    "username": "{{username}}",
    "balance": "{{balance}}",
    "used_balance": "{{used_balance}}",
    "api_keys": "{{api_keys}}",
    "models": "{{models}}"
  }
}
```

## 13. 失败与持久化

以下任一情况都会使本次工作流失败：

- 配置不符合本规范。
- Alias 模板求值失败。
- HTTP 传输失败。
- 非空响应体不是合法 JSON。
- `expect` 未返回唯一的 true。
- Extract 表达式失败、没有结果或产生多个未收集结果。
- Alias 结果不是合法 JSON 值。
- 最终 output 模板求值失败。
- 最终结果不符合宿主输出 Schema。

失败时不得持久化部分业务输出。宿主可以持久化独立的运行日志和 HTTP 审计记录，但必须对 Authorization、Cookie、API Key、请求 body 和响应 body 中的敏感字段进行脱敏。

只有全部 step 和最终 Schema 校验都成功后，才能原子替换上一次业务输出。运行期辅助 alias，例如 `key_ids`、`model_rows` 和 `group_ratios`，默认不属于业务输出，不应作为业务快照持久化。

## 14. 版本兼容

`spec` 决定完整语义，不允许通过字段组合猜测版本。

同一 major 版本内可以增加不改变现有配置含义的 jq 示例和说明，但不得增加会被旧读取方静默忽略的配置字段。新增字段、改变默认值、改变模板规则或改变 jq 求值上下文时，必须发布新的规范版本。

读取方必须拒绝不认识的 `spec` 和未知配置字段，以避免工作流看似执行成功但实际丢失语义。
