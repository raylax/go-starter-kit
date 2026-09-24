# Go Starter Kit

面向业务 REST API 的 Go 脚手架：Chi + Huma、PostgreSQL + pgx/sqlc、Goose、slog、OpenTelemetry。按业务模块组织的单体应用，提供项目管理和任务管理两组完整 CRUD API、自管用户、密码与第三方登录、资源所有权校验、测试和容器构建。

## 快速启动

需要 Go **1.27.1**、Docker（含 Compose）和 Make。依赖及开发工具版本记录在 `go.mod` / `go.sum`，不需要全局安装 sqlc 或 Goose。

```sh
cp .env.example .env
make db-up
make migrate-up
make dev
```

业务接口示例中的 `SESSION_TOKEN` 需设置为正常登录返回的 `tk_` 令牌，没有固定演示令牌。当前邮件为日志 mock，不提供真实邮箱验证闭环；使用注册流程需接入真实发送器。第三方协议端点要求 HTTPS；跨域部署由网关配置 CORS。

打开 <http://127.0.0.1:8080/docs> 查看交互式 API 文档，<http://127.0.0.1:8080/openapi.json> 获取契约。

```sh
curl -i http://127.0.0.1:8080/v1/projects \
  -H "Authorization: Bearer $SESSION_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"第一个项目","description":"脚手架示例"}'

curl 'http://127.0.0.1:8080/v1/projects?limit=20&offset=0' \
  -H "Authorization: Bearer $SESSION_TOKEN"
```

`.env` 由 Make 读取并导出给进程；直接运行二进制时需要自行注入环境变量。示例采用简单的 `KEY=value`，不使用 shell 引号或命令替换。数据库只映射到本机 `127.0.0.1:54329`，默认 API 监听 `127.0.0.1:8080`。

## 技术栈与组织方式

| 职责 | 实现 |
| --- | --- |
| HTTP 路由 | Chi v5，兼容标准 `net/http` |
| API 契约 | Huma v2，Go 类型生成 OpenAPI，运行时请求校验 |
| 数据库 | PostgreSQL，pgx v5 连接池 |
| 数据访问 | sqlc 生成类型安全的查询方法 |
| 迁移 | Goose，SQL 嵌入二进制，独立命令执行 |
| 认证 | 数据库会话 + `tk_` 随机令牌；密码及 OAuth 登录 |
| 授权 | 所有项目、任务查询均受认证 subject 限制 |
| 日志 | `log/slog` JSON，request ID、trace ID、路由模板、状态和耗时 |
| 遥测 | OpenTelemetry tracing / metrics，OTLP HTTP 导出 |
| 测试 | `testing`、`httptest`、Testcontainers PostgreSQL |
| 工程 | Go tool 固定工具版本、Make、Docker Compose、GitHub Actions |

```text
cmd/api/main.go             API 入口，serve/migrate/openapi 命令分发
cmd/worker/main.go          后台 Worker 入口
internal/                   应用实现，限制外部项目导入
  app/
    appfx/                  Fx 公共基础设施与生命周期
    api/                    HTTP 配置、装配、路由和生命周期
    worker/                 后台任务配置、调度和生命周期
  httpapi/                  HTTP 公共能力、认证中间件、错误与操作策略
  identity/                 随机令牌生成与格式校验、摘要、认证用户上下文
  authorization/            公共管理员授权契约
  apperror/                 业务错误分类和错误链
  pagination/               分页参数、结果和类型转换
  validation/               文本基础校验
  modules/
    account/                用户、登录账号、会话、验证流程、审计和邮件入队
    mailoutbox/             邮件租约领取、发送、重试和终态载荷清理
    project/                项目业务模型、服务、HTTP、DTO、测试
    task/                   任务业务模型、服务、HTTP、DTO、测试
  db/
    pool.go                 数据库连接池
    migrate.go              独立迁移命令
    errors.go               数据库错误识别与映射策略
    migrations/             全局有序、嵌入二进制的 SQL 迁移
    queries/                按完整表名命名的 SQL 查询
    sqlc/                   统一生成的数据库访问代码，不手工修改
  platform/
    password/               有并发上限的 Argon2id
    federation/             GitHub OAuth 适配
    mail/                   邮件发送接口与日志 mock
    telemetry/              OpenTelemetry 初始化与关闭
tests/                     仓库级测试
  integration/              独立集成测试；根部为应用装配、迁移和生命周期
    account/                 账户与认证
    project/                 项目
    task/                    任务
    mailoutbox/              邮件队列
    locker/                  PostgreSQL 锁
    testutil/                公共 HTTP、PostgreSQL 夹具
  architecture_test.go      静态依赖边界检查
api/openapi.json            生成的 API 契约，不手工修改
docs/architecture.md        功能边界与新增模块说明
```

HTTP 层负责校验和协议映射；业务规则位于各模块的 `Service`；数据库行转换为业务模型，再由 HTTP 层转换为独立 API DTO。业务模块复用认证中间件，通过构造函数传依赖，不使用全局数据库或通用 CRUD repository；Fx 仅在 app 层装配依赖。账户模块显式提交跨表事务，关键业务变更与审计写入共同提交；不在 HTTP 中间件里隐式提交事务。

详细边界见 [架构说明](docs/architecture.md)。`internal` 由 Go 工具链限制外部项目导入；架构测试继续约束内部反向依赖、业务模块互相引用以及 HTTP/DTO 直接使用数据库生成类型。公共夹具集中在 `tests/integration/testutil`，仅供测试使用。`app` 统一装配配置和依赖，`identity` 不接收全局配置或 HTTP 请求头。

API 入口支持 `go run ./cmd/api`、`go run ./cmd/api migrate up`、`go run ./cmd/api openapi`。离线 OpenAPI 复用类型化路由清单，不加载运行配置或初始化数据库或第三方服务；运行时构造函数拒绝缺失依赖。直接运行命令需要自行注入环境，`make dev` 则自动读取 `.env`。

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

错误使用 `application/problem+json`（`status`、`title`、`detail`，校验错误不回显原始字段值、路径或解析消息，避免泄露凭据），保留真实 HTTP 状态码。每个响应携带服务端生成的 UUID v4 `X-Request-ID`。数据库内部错误记录到日志，对客户端返回通用错误。未知字段/查询参数拒绝，请求体上限 1 MiB。405 同时返回 `Allow`，列出该路径实际注册的方法。业务模块通过 `apperror` 声明业务错误，由 `httpapi.Endpoint` 统一调用 `httpapi.FromError`；`httpapi.ProtectedOperations` 集中维护认证声明、请求限制和基础错误契约。未知异常只向客户端返回通用提示，底层原因通过错误链保留用于排查。

所有新记录的 UUID 由 Go 代码生成并显式传入 SQL，默认使用 UUID v4（`uuid.New()`）。`00001_init.sql` 的 UUID 主键不设置生成默认值，不依赖 PostgreSQL 的 UUID 生成功能或特定版本。生成策略保留在各资源的创建代码中，后续可按表独立切换 v7，数据库列和 DTO 仍使用 UUID 类型。开发和测试镜像当前选用 PostgreSQL 18，仅作为运行基线。

OpenAPI schema 统一使用资源名和用途：`Project` / `Task`、`ProjectCreateRequest` / `TaskCreateRequest`、`ProjectUpdateRequest` / `TaskUpdateRequest`、`ProjectListResponse` / `TaskListResponse`。公共类型为 `HealthResponse`、`ErrorResponse`、`ErrorDetail`。修改 DTO 后运行 `make generate`，使用 OpenAPI 生成 SDK 时需同步重新生成客户端。

## 任务 CRUD demo

任务是独立资源，无需先创建项目。字段包含 `title`（去除首尾空白后 1–200 字符）、`description`（最多 2000 字符）、`status`、ID 和创建/更新时间。允许重复标题，状态为 `todo`、`in_progress`、`done`，三者可互相切换。

创建时省略状态默认为 `todo`；PUT 是完整替换，必须明确提供标题和状态，省略描述表示清空。`GET /v1/tasks?status=done` 按状态筛选；省略状态查询全部，分页约定与项目相同。在 `/docs` 中可看到独立的 **Tasks** 分类。

空数据库先运行 `make migrate-up` 应用完整基线 `00001_init.sql`，再启动 API 和 Worker。已使用旧版迁移（包括拆分的 mail_outbox 迁移）的数据库不能直接套用新基线，需要单独规划数据迁移或重建开发数据库：

```sh
make migrate-up
make dev
```

在另一个终端依次操作：

```sh
# 创建，记录返回的 id
curl -i http://127.0.0.1:8080/v1/tasks \
  -H "Authorization: Bearer $SESSION_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"title":"编写文档","description":"补齐任务 API 示例"}'

TASK_ID='<创建响应中的 id>'

# 查询详情
curl "http://127.0.0.1:8080/v1/tasks/$TASK_ID" \
  -H "Authorization: Bearer $SESSION_TOKEN"

# 更新
curl -X PUT "http://127.0.0.1:8080/v1/tasks/$TASK_ID" \
  -H "Authorization: Bearer $SESSION_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"title":"编写文档","description":"已经完成","status":"done"}'

# 按状态分页查询
curl 'http://127.0.0.1:8080/v1/tasks?status=done&limit=20&offset=0' \
  -H "Authorization: Bearer $SESSION_TOKEN"

# 删除，成功返回 204
curl -i -X DELETE "http://127.0.0.1:8080/v1/tasks/$TASK_ID" \
  -H "Authorization: Bearer $SESSION_TOKEN"
```

## 认证配置

默认 `APP_ENV=production`、`DOCS_ENABLED=false`；所有环境均使用数据库会话认证。本项目管理 `users` 与 `accounts`；同一用户的多个登录账号共享业务资源。API 使用 `Authorization: Bearer tk_<随机值>`，数据库只存摘要，每次请求查询用户状态、会话有效期与认证版本。默认闲置期限 30 分钟、最长 24 小时；活跃请求的续期写入间隔取 1 分钟与闲置期限一半的较小值。退出、改密、封禁立即影响后续认证检查。

已实现邮箱注册/恢复、密码登录、GitHub OAuth 登录及显式绑定、重新认证、会话管理和用户管理。账户接口按 `Authentication`、`Profile`、`Account Security`、`Linked Accounts`、`Sessions`、`User Administration` 六组展示；20 个账户接口、前端回调协议见 [认证设计](docs/auth-design.md)。无 Cookie、刷新令牌或本地 JWT，也不暴露 JWKS。服务启动不依赖提供商在线。

API 通过 `FRONTEND_URL` 生成邮件验证链接。邮件当前由日志 mock 接受请求，不实际投递、不输出地址、正文或验证链接；注册/恢复返回 202 表示请求已受理，不保证生成邮件或实际送达。邮件通过持久化 `mail_outbox` 由 Worker 异步投递，无 SMTP 配置，发送抽象及未来适配方式见 [认证运行说明](docs/auth-operations.md)。


会话无效返回 401（`detail=session_invalid`）；验证、流程及重新认证证明失效返回 422（分别为 `verification_invalid`、`flow_invalid`、`reauthentication_invalid`），不影响有效会话；密码登录失败仍为 401，不能误当作当前会话失效。认证数据库故障返回 503，超时返回 504。应用不检查 Origin，也不生成 CORS 响应头；跨域访问由网关配置。客户端为会话设置 Authorization，不发送 Cookie。验证和 OAuth 回调使用正文 `token`；登录和流程创建响应的令牌字段也统一为 `token`。

## 配置与运行

| 变量 | 默认值 | 用途 |
| --- | --- | --- |
| `APP_ENV` | `production` | `production` / `development` / `test` |
| `HTTP_ADDR` | `127.0.0.1:8080` | 监听地址，容器镜像内为 `0.0.0.0:8080` |
| `DATABASE_URL` | 必填 | PostgreSQL URL |
| `DB_MAX_CONNS` | `10` | 连接池最大连接数 |
| `LOG_LEVEL` | `info` | JSON 日志级别 |
| `REQUEST_TIMEOUT` | `15s` | 业务请求上下文超时，数据库调用继承此期限 |
| `SHUTDOWN_TIMEOUT` | `10s` | API 排空或 Worker 停止预算；基础设施额外预留 10 秒 |
| `DOCS_ENABLED` | `false` | 暴露 `/docs` 和 `/openapi.json` |
| `FRONTEND_URL` | 必填 | 生成 `/auth/verify` 邮件链接的前端 HTTP(S) 地址 |
| `AUTH_PROVIDERS_FILE` | 空 | 可信提供商 JSON；空表示只启用密码 |
| `AUTH_SESSION_IDLE_TTL` / `AUTH_SESSION_MAX_TTL` | `30m` / `24h` | 闲置/绝对会话期限 |
| `OTEL_ENABLED` | `false` | 启用 OTLP 导出 |
| `OTEL_SERVICE_NAME` | 无 | `OTEL_ENABLED=true` 时必须显式设置非空服务名称 |

启动阶段收到 SIGINT/SIGTERM 会取消正在进行的依赖初始化，包括数据库连接。启动完成后，收到停止信号会停止接收新连接并排空请求，期间保持正在进行的认证和数据库调用可用；超过预算则取消请求上下文并关闭连接。HTTP 有请求头/请求体读取、写入和空闲连接期限。业务超时通过 context 协作取消；新增外部调用也必须传递 context。跨域部署由网关处理预检及 CORS 响应头，同源部署无需额外处理。数据库维护跨副本的认证限速计数；IP 直接使用连接对端地址，不信任客户端转发头。代理部署应配合网关限流，当前同一代理后的请求共享该 IP 桶。TLS 终止按网关部署设置。

生产迁移作为独立发布步骤执行：`api migrate up`，API 启动不自动迁移。生产推荐单个迁移任务和独立 DDL 账号；API 使用最小 DML 权限账号。当前只有一条初始化迁移，`migrate down` 会删除全部业务表及数据，勿用于常规生产发布。

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
make build              # 编译到 .bin/api 和 .bin/worker
docker build -t go-starter-kit:local .
make db-down            # 停止本地开发数据库，保留命名卷
```

修改表结构时新增迁移，不改已应用的历史迁移；查询文件按完整表名命名（如 `user_sessions.sql`、`auth_verifications.sql`）；跨表查询归入主要资源。修改查询后运行 `make generate`；生成步骤先清理旧 `.sql.go`，避免重命名后残留。生成文件随源文件一起提交。`make check-generated` 会重新生成并比较，漂移时失败，并留下最新生成结果供检查。

代码注释和说明文档统一使用中文，Go 标识符保持英文；生成器标记与工具指令保留固定格式。Go 代码统一使用 `any` / `...any`。sqlc 上游模板的旧写法在 `make generate` 中通过 Go AST 格式工具自动转换，`make lint` 同时检查此约定；不要绕过 Make 单独生成后提交。

集成测试统一位于 `tests/integration/`，通过 `make test-integration` 独立运行；业务目录只保留单元测试。集成测试覆盖项目和任务 CRUD、分页、任务状态筛选/更新、所有权隔离、输入校验、项目并发唯一性、数据库锁导致的超时、迁移 up/down/up 和健康检查；基线回滚删除全部业务表，之后重新应用基线。Docker 不可用时测试直接失败，不悄悄跳过。账户测试覆盖挑战并发、流程重放、跨用户绑定、会话撤销、邮件事务入队及审计失败回滚；mailoutbox 测试覆盖重试、租约归属与终态载荷清理；遥测测试使用本地 OTLP 接收端，验证 trace/metric 导出。它们不等于真实生产身份提供商或监控平台的联调验收。

GitHub Actions 执行相同检查、数据库集成测试和 Docker 构建。运行镜像采用非 root distroless，包含迁移与 OpenAPI 命令；交付时按环境传入配置和密钥。

生命周期集成测试运行完整服务与真实 PostgreSQL，覆盖取消启动，以及停止信号后排空正在等待数据库锁的业务请求。认证回归测试区分无效令牌、请求超时和数据库故障；HTTP 测试检查错误脱敏、405 的 `Allow` 和模块一致的错误响应。

## 选型参考

- [Chi](https://github.com/go-chi/chi)
- [Huma](https://huma.rocks/)
- [sqlc](https://docs.sqlc.dev/)
- [Goose](https://pressly.github.io/goose/)
- [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/)
- [Testcontainers Go](https://golang.testcontainers.org/)

## 独立 Worker

`make build` 同时编译 `.bin/api` 和 `.bin/worker`，可用 `make build-api`、`make build-worker` 单独构建。开发时分别运行 `make dev` 和 `make dev-worker`；生产环境分别部署两个进程。

Worker 消费持久化邮件队列，需要 `DATABASE_URL`，支持 `DB_MAX_CONNS`（默认 5）和 `MAIL_POLL_INTERVAL`（默认 1s）。共用 `.env.example` 时，显式的 `DB_MAX_CONNS=10` 会覆盖 Worker 默认值。其余配置为 `LOG_LEVEL`、`SHUTDOWN_TIMEOUT`、`OTEL_ENABLED`、`OTEL_SERVICE_NAME`；启用遥测时服务名必须显式设置。

认证临时数据已停止自动清理；过期会话及证明仍会在认证查询中失效，但记录不会自动删除。

Docker 分别构建：

```sh
docker build --target api -t go-starter-kit-api .
docker build --target worker -t go-starter-kit-worker .
```

后续 Temporal 的连接与注册在 `internal/app/worker` 装配，具体工作流放入 `internal/workflows/<业务>/`。Workflow 负责确定性编排，Activity 调用注入的业务能力；当前未引入 Temporal 依赖。

数据库不使用 `CHECK` 约束。字段长度、状态枚举、凭据组合、认证摘要和审计 JSON 规则由业务代码校验；数据库保留主键、外键、唯一约束和 `NOT NULL`。固定用户角色及初始版本由专用写入路径构造。修改基线仅影响新建数据库，已有数据库不会自动移除原约束。

## 公共 Locker

`internal/platform/locker` 提供 `TryRun`（竞争时跳过）和 `Run`（等待到获取或取消）；默认 PostgreSQL 后端在 `internal/platform/locker/postgres`，可由使用方按需装配。完整契约及后续后端要求见 [Locker 设计](docs/locker.md)。

Locker 没有内置环境变量或默认应用装配。使用方创建专用 PostgreSQL 连接池，调用 `postgres.New` 与 `locker.New`，显式传入命名空间；数据库连接必须直连或使用 session pooling，不能使用 transaction pooling。

所有锁统一根据命名空间与锁名称生成标识，不提供旧版本兼容或特殊映射。

## 命令与环境配置

命令行采用 Cobra，环境变量采用 `caarlos0/env/v11`。`api` 默认执行 `serve`，支持 `serve`、`migrate up|down`、`openapi`；`worker` 启动后台进程。两个入口均支持 `--help`，额外参数和未知选项会报错。

配置按命令加载：`serve` 加载 API 配置，`migrate` 只加载 `DATABASE_URL`，`worker` 加载数据库、邮件轮询及基础运行配置。帮助和离线 OpenAPI 不解析运行配置，也不连接数据库。运行配置通过环境变量提供，未添加重复的配置 flags。

Config 通过 env 标签声明名称、类型和默认值；`Parse` 负责转换，`Validate` 保留跨字段业务规则，`Run(ctx, cfg)` 使用显式配置。公共 `platform/configenv` 对声明非空默认值的字段拒绝显式空值，只有未设置时使用默认值；可选字符串仍允许为空。解析错误只输出字段名称及固定原因，不回显原始值。程序不自动读取 `.env`，开发环境继续由 Makefile 注入。

### Fx 依赖装配

API 与 Worker 使用 Fx v1.24.0 分别构建依赖图，公共数据库、日志和遥测在 `internal/app/appfx` 装配。业务模块保持普通构造函数，不依赖 Fx。资源在启动钩子中初始化，先启动遥测与数据库，再启动 HTTP 或邮件消费者；停止时顺序反转。

`SHUTDOWN_TIMEOUT` 限制 HTTP 排空或 Worker 停止等待，应用额外预留 10 秒关闭基础设施。启动钩子使用 Fx 默认启动期限并响应命令上下文取消；Fx 回滚使用独立上下文，并额外保留完整的停止预算，以便启动取消或超时后释放已初始化的资源。后台服务异常退出会传播到命令入口。`migrate`、`openapi` 和帮助命令不创建 Fx 应用。
