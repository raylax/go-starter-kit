# 目录结构与功能边界

本项目采用按业务模块组织的单体应用，入口为根目录 `main.go`。目录用于表达职责，依赖通过构造函数显式传入。

## 应用装配

| 文件 | 职责 |
| --- | --- |
| `main.go` | 信号处理与命令分发；默认执行 `serve` |
| `app/config.go` | 环境变量解析、默认值和启动校验 |
| `app/auth_config.go` | 开发环境限制及认证校验错误的环境变量提示 |
| `app/run.go` | 初始化遥测、数据库和认证，启动 HTTP 服务并排空请求 |
| `app/wire.go` | 构造业务服务、映射认证专用配置、检查运行时依赖 |
| `app/routes.go` | 统一路由清单及离线 OpenAPI 导出 |

`app.Config` 留在装配层；HTTP 接收 `httpapi.Config`，JWT 接收 `identity.JWTConfig`。业务服务不读取环境变量，也不访问全局连接池。

启动阶段的取消信号会取消首次 JWKS 请求。开始服务后，停止信号先停止接收新连接并排空请求；排空期间保留认证和数据库调用的上下文，超过关闭期限再强制取消。迁移由独立命令执行，启动 API 不自动更改表结构。

## HTTP 与身份认证

`httpapi` 提供 Chi/Huma 初始化、请求标识和日志、超时上下文、异常恢复、公共错误映射、405 的 `Allow`、健康检查和操作契约。模块中的 `http.go` 声明路由并将请求映射为服务参数；`dto.go` 定义独立协议类型和模型到 DTO 的转换，通用响应包装由 `httpapi` 负责。原始业务错误由路由绑定层统一映射。

`identity.Authenticator` 接收原始令牌并返回 subject。HTTP 中间件解析 `Authorization`，将认证结果放入请求上下文。JWT 校验与 JWKS 缓存刷新独立于 HTTP；固定开发令牌由应用配置限制在开发和测试环境。非法凭据、认证依赖故障、请求超时分别映射为 401、503、504。

## 业务模块

项目与任务采用相同文件职责：

| 文件或目录 | 职责 |
| --- | --- |
| `model.go` | 无 JSON 标签的 `Record`、业务状态、输入类型和业务错误 |
| `service.go` | 业务校验、所有权作用域、分页与数据库访问 |
| `http.go` | 类型化路由声明、请求参数到服务调用的适配 |
| `dto.go` | Huma 请求与响应类型、JSON/校验标签、业务模型到独立 DTO 的转换、公共响应类型别名 |
| `*_test.go` | 业务单测；带 `integration` 标签的真实 HTTP 与 PostgreSQL 测试 |

数据库行不直接成为接口返回值，业务模型也不承担接口校验标签。项目 DTO 仍名为 `Project`，任务 DTO 仍名为 `Task`，使用资源名作为 OpenAPI schema 名称。

`dto.go` 保留一个显式 `toDTO`。当前模型与 DTO 字段一致，可以使用 `Project(item)` 或 `Task(item)` 的结构转换；二者仍是独立类型，字段不再匹配时编译失败，再改为逐字段映射。不要将 DTO 定义为 `Record` 或 sqlc 类型的别名。模块响应别名复用 `httpapi.ItemOutput` 与 `CreatedOutput`，不再编写 `createdOutput`、`itemOutput`、`listOutput`。

## 数据库管理

数据库相关代码统一放在根目录 `db/`：

| 文件或目录 | 职责 |
| --- | --- |
| `db/pool.go` | 初始化和校验 PostgreSQL 连接池 |
| `db/migrate.go` | 执行嵌入二进制的版本迁移 |
| `db/errors.go` | 按业务声明的策略识别缺失记录和唯一约束错误 |
| `db/migrations/` | 全局有序的 SQL 迁移 |
| `db/queries/` | 按业务拆分查询，例如 `projects.sql`、`tasks.sql` |
| `db/sqlc/` | sqlc 统一生成的查询方法、参数和数据库模型 |

`sqlc.yaml` 只有一份生成配置，读取全部迁移与查询，生成包名为 `sqlc`。`make generate` 自动规范为 `any`，`make check-generated` 检查统一输出与接口契约的一致性；生成文件不手工修改。

`app` 使用 `db.Open()` 创建连接池，再注入业务服务。服务内部通过 `sqlc.New()` 构造查询，HTTP 和 DTO 不直接依赖生成代码。集中生成减少配置重复，也让跨表查询与事务使用同一套类型；同时，各业务服务都能访问完整的 `sqlc.Queries`，因此不再通过独立生成包隔离业务表的访问权限。业务仍应只操作职责范围内的数据，资源所有权必须由 SQL 条件保证。

SQL 在查询、更新和删除时同时限制资源 ID 与所有者。服务仍校验 subject，防止绕过 HTTP 直接调用时访问无作用域数据。暂不引入通用 CRUD repository 或依赖注入容器。跨表操作按业务定义事务边界，使用生成的 `Queries.WithTx(tx)`。

## 公共错误处理

`apperror.Error` 只保存分类和安全提示，`Wrap` 同时保留具体业务错误与底层原因。`errors.Is` 可区分项目和任务的具体错误；`errors.As` 可读取公共分类。同一种错误分类不会让不同业务错误互相相等。

模块中的 HTTP 处理函数直接返回业务错误。`httpapi.Endpoint.Bind` 调用 `FromError`：不存在、冲突、无效输入、未认证、依赖故障分别对应 404、409、422、401、503。未知错误在兜底处理中检查请求超时，超时返回 504，其余记录原因后返回通用 500。业务分类优先于底层原因，HTTP 层明确构造的 Huma 错误也保持原语义，例如健康检查失败始终返回 503。

认证中间件在处理函数之前运行，继续负责 Bearer 请求头、401 登录挑战以及认证依赖故障到 503/504 的映射。它与业务错误映射承担不同的协议职责。

`db.MapError` 根据模块的 `ErrorPolicy` 转换 `pgx.ErrNoRows` 和已声明的唯一约束。未知约束不推断业务含义，取消和超时错误保留原样；错误链中的数据库细节不会进入公开提示。删除影响零行的判断仍由业务方法负责。

路由适配器按返回形式选择：

- `CreatedEndpoint(operation, execute, toDTO, location)`：成功返回 201，先转换独立 DTO，再从 DTO 构造 `Location`。
- `ItemEndpoint(operation, execute, toDTO)`：查询和更新成功返回 200，统一包装 DTO 正文。
- `PageEndpoint[具名分页 DTO](operation, execute, toDTO)`：成功返回 200，转换分页条目并保留分页元数据，空列表输出 `[]`。
- `MapEndpoint(operation, execute, present)`：特殊响应的底层适配器，仅成功时执行自定义响应组装。
- `NoContentEndpoint(operation, execute)`：适配只返回错误的调用，默认成功返回 204 且没有正文。
- `Endpoint(operation, handle)`：保留对完整 HTTP 输出的直接控制，例如健康检查。

模块的 `Routes()` 只列出操作声明、业务调用函数、DTO 转换和创建地址。请求中的 subject、任务状态、创建响应的 `Location` 等仍由模块显式处理，公共适配器只负责所选响应形式。所有入口最终共用错误映射和离线描述；导出契约时不会执行业务调用或响应组装。

## 公共分页与校验

`pagination.Params` 集中限制分页参数，服务在查询前调用 `Validate`，再用 `FetchLimit` 多取一条记录。`Build` 截断额外记录并调用字段转换函数，`Map` 在类型转换时保留分页元数据和非 nil 空列表。

HTTP 查询标签放在 `httpapi.PageQuery`，通过 `Params()` 传入业务层。`httpapi.PageFrom` 将业务分页结果转换为响应类型。模块使用 `ProjectListResponse` 和 `TaskListResponse` 具名类型，明确资源归属与响应用途。`PageEndpoint` 的首个类型参数显式指定具名分页 DTO，编译期约束保证结构与 `httpapi.Page[DTO]` 一致，无需反射或字符串命名规则。Go 结构标签不能引用常量，标签与分页规则的一致性由测试检查。

`validation.RequiredText` 按字符数检查必填文本并去除首尾空白；`TextWithin` 保留描述字段的空白并允许空字符串。字段上限、任务状态和创建默认值仍由业务模块决定。数据库与 DTO 中的校验继续保护各自入口。

开发认证凭据和 JWT subject 复用 `identity` 的校验函数，其中 subject 上限按字节计算。配置层补充环境变量名称，认证构造函数也执行校验；生产环境禁止开发认证的规则由 `app` 内的公共函数维护。服务入口的空所有者检查继续保留。

## 测试复用

应用单测通过 `app/test_helpers_test.go` 构造默认依赖。`tests/testutil/jwtfixture` 只依赖标准库与 JWT 库，复用密钥生成、公钥编码和令牌签名，不依赖 `identity` 或应用装配，避免包内测试循环导入。签名算法、claims、密钥标识以及 JWKS 的阻塞、刷新和故障行为均由具体测试显式控制。

真实 PostgreSQL、HTTP 请求等已有夹具继续复用；业务 CRUD、所有权、迁移链和生命周期场景保留独立断言。

## Schema 命名

公开 schema 使用 PascalCase，名称表达资源与用途，不携带 Huma 的输入输出包装细节：

| 用途 | 规则 | 示例 |
| --- | --- | --- |
| 资源模型 | `<资源>` | `Project`、`Task` |
| 请求正文 | `<资源><动作>Request` | `ProjectCreateRequest`、`ProjectUpdateRequest`、`TaskCreateRequest`、`TaskUpdateRequest` |
| 列表响应 | `<资源>ListResponse` | `ProjectListResponse`、`TaskListResponse` |
| 公共响应 | `<用途>Response` | `HealthResponse`、`ErrorResponse` |
| 错误明细 | `ErrorDetail` | 公共错误的条目类型 |

业务正文使用具名 DTO，避免匿名字段派生出 `TaskCreateInputBody` 等名字。Huma 的 `Input`、`Output` 包装仍用于 Go 路由参数与响应头，不作为公开正文的命名规则。项目创建和更新目前字段相同，使用两个独立的具名请求类型；更新类型复用创建类型的底层结构，将来校验规则不同再单独定义。

`httpapi.New` 通过当前 API 的 schema registry 将 Huma 自带的 `ErrorModel` 命名为 `ErrorResponse`，其余类型沿用 DTO 名称；不修改全局错误工厂或错误响应行为。命名调整会改变 OpenAPI 的组件名与引用，使用契约生成 SDK 的客户端需重新生成类型；HTTP 路径、状态码和 JSON 字段不变。

## 离线契约

各模块的 `Routes()` 声明 Huma operation、输入输出类型和处理函数。`httpapi.Route` 支持两种用途：

- `Bind`：传入真实服务和认证中间件，注册运行时处理函数。
- `Describe`：只向独立的契约路由器注册类型信息，不持有服务依赖。

`app/routes.go` 的清单同时用于运行时和离线导出。`app.OpenAPI()` 不加载环境配置、连接 PostgreSQL 或访问 JWKS，也不返回可以对外服务的处理器。`app.NewHandler()` 要求完整运行时依赖，缺失则立即返回错误。单测比较开启和关闭在线文档时的契约与离线契约，防止生成结果与服务偏离。

## 包边界

允许的应用内依赖方向：

```text
main → app、db（迁移命令）
app → modules、httpapi、identity、db、platform
modules/<业务> 的 HTTP → httpapi、identity、pagination
modules/<业务> 的 service → db、db/sqlc、apperror、pagination、validation
modules/<业务> 的 model → apperror
modules/<业务> 的 DTO → httpapi（公共响应类型）
httpapi → identity、apperror、pagination
db → apperror、标准库与数据库依赖
platform → platform 内的基础设施
identity → 标准库与认证依赖
apperror、pagination、validation → 标准库
```

移除 `internal` 后，Go 不再限制外部模块导入这些包。此仓库是应用脚手架，不承诺这些包是稳定 SDK；`tests/architecture_test.go` 约束仓库内的依赖方向、数据库生成类型的使用位置以及模型/协议层隔离。需要对外发布 SDK 时应另建明确的公共契约。

## 新增业务模块

1. 在 `modules/<业务>/` 建立业务模型、服务、HTTP 和 DTO，保持服务不依赖协议类型；复用公共业务错误、分页和文本校验，并声明本业务的数据库错误策略。
2. 需要新表时，在 `db/migrations/` 增加下一个全局版本的迁移；查询按业务文件放入 `db/queries/`。
3. 执行 `make generate`，更新 `db/sqlc/`。同一数据库下新增业务查询无需增加 sqlc 配置或修改生成脚本。
4. 在 `app/wire.go` 增加服务依赖，在 `app/routes.go` 的清单中绑定模块路由；受保护业务必须启用认证中间件。
5. 添加模块业务测试和必要的真实数据库测试，复用 `tests/testutil`；跨模块与生命周期场景放入 `tests/integration`。
6. 执行 `make generate`、`make check`、`make test-integration` 和 `make build`。

迁移只调整目录时必须保持版本和内容不变，防止迁移历史分叉。
