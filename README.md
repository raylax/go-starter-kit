# Go Starter Kit

面向业务 REST API 的 Go 脚手架：Chi + Huma、PostgreSQL + pgx/sqlc、Goose、slog、OpenTelemetry。按业务模块组织的单体应用，提供项目管理和任务管理两组完整 CRUD API、资源所有权校验、测试和容器构建。

## 快速启动

需要 Go **1.27.1**、Docker（含 Compose）和 Make。依赖及开发工具版本记录在 `go.mod` / `go.sum`，不需要全局安装 sqlc 或 Goose。

```sh
cp .env.example .env
make db-up
make migrate-up
make dev
```

打开 <http://127.0.0.1:8080/docs> 查看交互式 API 文档，<http://127.0.0.1:8080/openapi.json> 获取契约。

```sh
curl -i http://127.0.0.1:8080/v1/projects \
  -H 'Authorization: Bearer local-development-token' \
  -H 'Content-Type: application/json' \
  -d '{"name":"第一个项目","description":"脚手架示例"}'

curl 'http://127.0.0.1:8080/v1/projects?limit=20&offset=0' \
  -H 'Authorization: Bearer local-development-token'
```

`.env` 由 Make 读取并导出给进程；直接运行二进制时需要自行注入环境变量。示例采用简单的 `KEY=value`，不使用 shell 引号或命令替换。数据库只映射到本机 `127.0.0.1:54329`，默认演示 API 监听 `127.0.0.1:8080`。

## 技术栈与组织方式

| 职责 | 实现 |
| --- | --- |
| HTTP 路由 | Chi v5，兼容标准 `net/http` |
| API 契约 | Huma v2，Go 类型生成 OpenAPI，运行时请求校验 |
| 数据库 | PostgreSQL 18，pgx v5 连接池 |
| 数据访问 | sqlc 生成类型安全的查询方法 |
| 迁移 | Goose，SQL 嵌入二进制，独立命令执行 |
| 认证 | JWT access token + JWKS；显式开启的本地演示 token |
| 授权 | 所有项目、任务查询均受认证 subject 限制 |
| 日志 | `log/slog` JSON，request ID、trace ID、路由模板、状态和耗时 |
| 遥测 | OpenTelemetry tracing / metrics，OTLP HTTP 导出 |
| 测试 | `testing`、`httptest`、Testcontainers PostgreSQL |
| 工程 | Go tool 固定工具版本、Make、Docker Compose、GitHub Actions |

```text
main.go                     根目录入口，serve/migrate/openapi 命令分发
internal/                   应用实现，限制外部项目导入
  app/                      配置、依赖装配、统一路由清单、启动与退出
  httpapi/                  HTTP 公共能力、认证中间件、错误与操作策略
  identity/                 原始令牌校验、JWT/JWKS、认证用户上下文
  apperror/                 业务错误分类和错误链
  pagination/               分页参数、结果和类型转换
  validation/               文本基础校验
  modules/
    project/                项目业务模型、服务、HTTP、DTO、测试
    task/                   任务业务模型、服务、HTTP、DTO、测试
  db/
    pool.go                 数据库连接池
    migrate.go              独立迁移命令
    errors.go               数据库错误识别与映射策略
    migrations/             全局有序、嵌入二进制的 SQL 迁移
    queries/                按业务拆分的 SQL 查询
    sqlc/                   统一生成的数据库访问代码，不手工修改
  platform/
    telemetry/              OpenTelemetry 初始化与关闭
  testutil/                 公共 HTTP、PostgreSQL 和 JWT 测试夹具
tests/                     仓库级测试
  integration/              应用装配、迁移链、健康检查和生命周期测试
  architecture_test.go      静态依赖边界检查
api/openapi.json            生成的 API 契约，不手工修改
docs/architecture.md        功能边界与新增模块说明
```

HTTP 层负责校验和协议映射；业务规则分别在 `project.Service`、`task.Service`；数据库行转换为业务模型，再由 HTTP 层转换为独立 API DTO。两个模块复用认证中间件，通过构造函数传依赖，不使用全局数据库、依赖注入容器或通用 CRUD repository。示例每次写入只有一个 SQL 语句；新增跨表业务时，在业务操作中明确事务边界，使用 sqlc 生成的 `Queries.WithTx(tx)`，不要在 HTTP 中间件里隐式提交事务。

详细边界见 [架构说明](docs/architecture.md)。`internal` 由 Go 工具链限制外部项目导入；架构测试继续约束内部反向依赖、业务模块互相引用以及 HTTP/DTO 直接使用数据库生成类型。公共夹具集中在 `internal/testutil`，仅供测试使用。`app` 统一装配配置和依赖，`identity` 不接收全局配置或 HTTP 请求头。

根目录支持 `go run .`、`go run . migrate up`、`go run . openapi`。离线 OpenAPI 复用类型化路由清单，不加载运行配置或初始化数据库、JWKS；运行时构造函数拒绝缺失依赖。直接运行命令需要自行注入环境，`make dev` 则自动读取 `.env`。

复用此模板时，将 `github.com/example/go-starter-kit` 替换为实际模块路径，包括 `go.mod` 和 Go 文件的导入路径，再执行 `go mod tidy` 与 `make check`。

公共规则集中在 `apperror`、`pagination`、`validation` 和 `identity` 中。业务模块保留自己的字段限制、状态、约束映射、所有者检查和 SQL 调用。列表响应复用泛型分页结构，并通过具名类型稳定 schema 名称；创建、单条和分页接口分别使用 `CreatedEndpoint`、`ItemEndpoint`、`PageEndpoint[具名分页 DTO]`，模块只提供独立 DTO 转换和创建地址；删除接口使用 `NoContentEndpoint`；新增模块无需复制错误分支、错误映射和分页循环。

## API 约定

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET | `/health/live` | 进程存活，独立于数据库 |
| GET | `/health/ready` | 数据库可用性，超时或关闭时返回 503 |
| POST | `/v1/projects` | 创建，返回 201 和 `Location` |
| GET | `/v1/projects` | 查询自己的项目，`limit` / `offset` 分页 |
| GET | `/v1/projects/{id}` | 查询自己的项目 |
| PUT | `/v1/projects/{id}` | 替换名称与描述；省略描述表示清空 |
| DELETE | `/v1/projects/{id}` | 删除，成功返回 204 |
| POST | `/v1/tasks` | 创建任务，返回 201 和 `Location` |
| GET | `/v1/tasks` | 查询自己的任务，支持状态筛选和分页 |
| GET | `/v1/tasks/{id}` | 查询自己的任务 |
| PUT | `/v1/tasks/{id}` | 替换标题、描述与状态 |
| DELETE | `/v1/tasks/{id}` | 删除任务，成功返回 204 |

所有项目、任务接口都要求 `Authorization: Bearer …`。资源不存在或属于其他用户时统一返回 404。同一用户的项目名称去除首尾空白后唯一，区分大小写；不同用户可以使用相同名称；并发冲突由数据库唯一约束保证，映射为 409。

列表按 `created_at DESC, id DESC` 排序，默认 `limit=20`，最大 100；`offset` 最大 10000，返回 `items`、`has_more`、`limit`、`offset`，空列表始终是 `[]`。这是简单偏移分页，在并发插入/删除期间不保证跨页快照一致；需要大数据量稳定翻页时应改为游标分页。

错误使用 `application/problem+json`（`status`、`title`、`detail`，校验错误附字段详情），保留真实 HTTP 状态码。每个响应携带服务端生成的 UUID v7 `X-Request-ID`。数据库内部错误记录到日志，对客户端返回通用错误。未知字段/查询参数拒绝，请求体上限 1 MiB。405 同时返回 `Allow`，列出该路径实际注册的方法。业务模块通过 `apperror` 声明业务错误，由 `httpapi.Endpoint` 统一调用 `httpapi.FromError`；`httpapi.ProtectedOperations` 集中维护认证声明、请求限制和基础错误契约。未知异常只向客户端返回通用提示，底层原因通过错误链保留用于排查。

项目和任务的新记录主键由 PostgreSQL 18 的 `uuidv7()` 生成。`00003_uuid_v7.sql` 只修改默认生成规则，已有 UUID 保持不变；Go 中生成 UUID 使用 `uuid.NewV7()`，数据库列和 DTO 仍使用 UUID 类型。

OpenAPI schema 统一使用资源名和用途：`Project` / `Task`、`ProjectCreateRequest` / `TaskCreateRequest`、`ProjectUpdateRequest` / `TaskUpdateRequest`、`ProjectListResponse` / `TaskListResponse`。公共类型为 `HealthResponse`、`ErrorResponse`、`ErrorDetail`。修改 DTO 后运行 `make generate`，使用 OpenAPI 生成 SDK 时需同步重新生成客户端。

## 任务 CRUD demo

任务是独立资源，无需先创建项目。字段包含 `title`（去除首尾空白后 1–200 字符）、`description`（最多 2000 字符）、`status`、ID 和创建/更新时间。允许重复标题，状态为 `todo`、`in_progress`、`done`，三者可互相切换。

创建时省略状态默认为 `todo`；PUT 是完整替换，必须明确提供标题和状态，省略描述表示清空。`GET /v1/tasks?status=done` 按状态筛选；省略状态查询全部，分页约定与项目相同。在 `/docs` 中可看到独立的 **Tasks** 分类。

已有数据库先运行 `make migrate-up` 应用所有待执行迁移（包括 `00003_uuid_v7.sql`），再启动或重启 API：

```sh
make migrate-up
make dev
```

在另一个终端依次操作：

```sh
# 创建，记录返回的 id
curl -i http://127.0.0.1:8080/v1/tasks \
  -H 'Authorization: Bearer local-development-token' \
  -H 'Content-Type: application/json' \
  -d '{"title":"编写文档","description":"补齐任务 API 示例"}'

TASK_ID='<创建响应中的 id>'

# 查询详情
curl "http://127.0.0.1:8080/v1/tasks/$TASK_ID" \
  -H 'Authorization: Bearer local-development-token'

# 更新
curl -X PUT "http://127.0.0.1:8080/v1/tasks/$TASK_ID" \
  -H 'Authorization: Bearer local-development-token' \
  -H 'Content-Type: application/json' \
  -d '{"title":"编写文档","description":"已经完成","status":"done"}'

# 按状态分页查询
curl 'http://127.0.0.1:8080/v1/tasks?status=done&limit=20&offset=0' \
  -H 'Authorization: Bearer local-development-token'

# 删除，成功返回 204
curl -i -X DELETE "http://127.0.0.1:8080/v1/tasks/$TASK_ID" \
  -H 'Authorization: Bearer local-development-token'
```

## 认证配置

默认 `APP_ENV=production`、`AUTH_MODE=jwt`、`DOCS_ENABLED=false`。生产启动至少提供：

```text
DATABASE_URL=postgres://...
AUTH_MODE=jwt
AUTH_ISSUER=https://identity.example.com/
AUTH_AUDIENCE=starter-api
AUTH_JWKS_URL=https://identity.example.com/.well-known/jwks.json
```

从身份提供商的配置中取得实际 issuer、API audience 和 JWKS 地址。支持 RS256 / ES256 签名的 JWT **access token**，验证签名、issuer、audience、必填过期时间、nbf、存在时的 iat 和非空 subject；JWKS 由库缓存和刷新，刷新生命周期跟随进程。issuer 必须精确匹配，包括尾部斜杠。API audience 应与前端登录客户端的 audience 区分，避免 ID token 被用作 API 凭据。

脚手架负责验证已有凭据和资源所有权，不实现登录页面、用户注册、密码管理、token 签发或 opaque token introspection。生产身份提供商需要自行配置并签发面向该 API 的 token。单个部署绑定一个 issuer，所有权使用其 `sub`；更换 issuer 前需迁移用户映射。

缺失、过期或无效凭据返回 401，并携带 Bearer 登录挑战；请求期限内无法完成认证返回 504；JWKS 刷新等认证依赖故障返回 503，后二者不发送登录挑战。首次 JWKS 加载失败会阻止启动，避免服务在无法验证凭据时宣告启动成功。

`.env.example` 显式设置 `APP_ENV=development` / `AUTH_MODE=dev`，以固定演示 token 映射到 `demo-user`，方便本地跑通业务。生产环境禁止该模式；不要把演示模式对外发布。

## 配置与运行

| 变量 | 默认值 | 用途 |
| --- | --- | --- |
| `APP_ENV` | `production` | `production` / `development` / `test` |
| `HTTP_ADDR` | `127.0.0.1:8080` | 监听地址，容器镜像内为 `0.0.0.0:8080` |
| `DATABASE_URL` | 必填 | PostgreSQL URL |
| `DB_MAX_CONNS` | `10` | 连接池最大连接数 |
| `LOG_LEVEL` | `info` | JSON 日志级别 |
| `REQUEST_TIMEOUT` | `15s` | 业务请求上下文超时，数据库调用继承此期限 |
| `SHUTDOWN_TIMEOUT` | `10s` | 请求排空、遥测关闭的各自时间预算 |
| `DOCS_ENABLED` | `false` | 暴露 `/docs` 和 `/openapi.json` |
| `AUTH_MODE` | `jwt` | 生产 JWT 或开发演示认证 |
| `DEV_AUTH_TOKEN` | 空 | 开发模式必填，至少 16 字符 |
| `DEV_AUTH_SUBJECT` | 空 | 开发模式对应的用户 subject |
| `OTEL_ENABLED` | `false` | 启用 OTLP 导出 |
| `OTEL_SERVICE_NAME` | `go-starter-kit` | 遥测服务名称 |

启动阶段收到 SIGINT/SIGTERM 会取消正在进行的依赖初始化，包括首次 JWKS 请求。启动完成后，收到停止信号会停止接收新连接并排空请求，期间保持正在进行的认证和数据库调用可用；超过预算则取消请求上下文并关闭连接。HTTP 有请求头/请求体读取、写入和空闲连接期限。业务超时通过 context 协作取消；新增外部调用也必须传递 context。默认不配置跨域；跨域浏览器客户端应添加明确的来源白名单。TLS 终止、分布式限流等按实际网关部署设置。

生产迁移作为独立发布步骤执行：`api migrate up`，API 启动不自动迁移。生产推荐单个迁移任务和独立 DDL 账号；API 使用最小 DML 权限账号。`migrate down` 仅回滚一个版本：版本 2 删除任务表及数据，版本 1 删除项目表及数据，勿用于常规生产发布。

## 遥测

```text
OTEL_ENABLED=true
OTEL_SERVICE_NAME=go-starter-kit
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
```

使用标准 OTLP HTTP `/v1/traces` 和 `/v1/metrics`，可通过 OpenTelemetry Collector 接入现有后端。支持 SDK 的标准 `OTEL_EXPORTER_OTLP_*` 环境变量，包括 endpoint、headers 和 TLS 配置。根 trace 默认采样 10%，继承父 trace 采样决定；指标独立于 trace 采样。日志始终写 JSON 到 stdout，由部署平台收集；没有重复配置 OTLP 日志导出。

默认采集 HTTP 服务端追踪和指标，span 与指标使用路由模板，避免项目 UUID 造成高基数。SQL 和业务自定义 span 按业务需要追加；目前不采集 SQL 文本和请求/响应正文。

## 开发与验证

```sh
make generate           # 从 SQL 和 Go API 类型重新生成代码/契约
make check              # 格式、go vet、race 单测、架构边界、生成一致性、漏洞扫描
make test-integration   # Docker 中新建临时 PostgreSQL，真实 HTTP + DB 测试
make build              # 编译到 .bin/api
docker build -t go-starter-kit:local .
make db-down            # 停止本地开发数据库，保留命名卷
```

修改表结构时新增迁移，不改已应用的历史迁移；修改查询后运行 `make generate`。生成文件随源文件一起提交。`make check-generated` 会重新生成并比较，漂移时失败，并留下最新生成结果供检查。

代码注释和说明文档统一使用中文，Go 标识符保持英文；生成器标记与工具指令保留固定格式。Go 代码统一使用 `any` / `...any`。sqlc 上游模板的旧写法在 `make generate` 中通过 Go AST 格式工具自动转换，`make lint` 同时检查此约定；不要绕过 Make 单独生成后提交。

集成测试覆盖项目和任务 CRUD、分页、任务状态筛选/更新、所有权隔离、输入校验、项目并发唯一性、数据库锁导致的超时、迁移 up/down/up 和健康检查；任务迁移回滚还验证已有项目数据保留。Docker 不可用时测试直接失败，不悄悄跳过。JWT 测试使用本地 JWKS 服务和真实签名；遥测测试使用本地 OTLP 接收端，验证 trace/metric 导出。它们不等于真实生产身份提供商或监控平台的联调验收。

GitHub Actions 执行相同检查、数据库集成测试和 Docker 构建。运行镜像采用非 root distroless，包含迁移与 OpenAPI 命令；交付时按环境传入配置和密钥。

生命周期集成测试运行完整服务与真实 PostgreSQL，覆盖初始化 JWKS 时取消启动，以及停止信号到达后仍能完成正在等待 JWKS 轮换的业务请求。认证回归测试区分无效 token、请求超时和远程 JWKS 故障；HTTP 测试检查 405 的 `Allow` 和两个模块一致的错误响应。

## 选型参考

- [Chi](https://github.com/go-chi/chi)
- [Huma](https://huma.rocks/)
- [sqlc](https://docs.sqlc.dev/)
- [Goose](https://pressly.github.io/goose/)
- [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/)
- [Testcontainers Go](https://golang.testcontainers.org/)
