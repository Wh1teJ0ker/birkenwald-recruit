# Birkenwald 招新系统：项目规划 v0.1

日期：2026-11-05。状态：v0.1.0 MVP 已实现；本文同时保留后续阶段的设计目标。当前可运行范围以 [README](../README.md) 为准。

## 0. v0.1.0 交付状态

- 已实现：Go/Gin API、PostgreSQL 初始迁移、React/Ant Design 页面、学生/管理员登录、轮次与任务创建发布、单轮单任务选择、PDF/ZIP 上传、版本化提交、异步容器插件评分、失败重试、会话与 CSRF、防并发超额与提交幂等。
- 已测试：数据库并发选题与提交、worker 租约抢占、过期 worker 结果隔离、文件校验、React 页面、真实示例插件。CI 在推送/PR 时运行 Go race 测试与前端测试。
- 后续：插件镜像 digest 固定与登记流程、校内 SSO、人工复核/录取工作流、通知、限流、附件清理、备份恢复演练和生产运维。下文的“必须完成”指总体路线图，不表示这些能力均已交付。

## 1. 名称与目标

- 产品名：**Birkenwald 招新系统**；英文名：**Birkenwald Recruit**；仓库 slug：`birkenwald-recruit`。
- 一轮招新称为 `campaign`，其中包含多个 `task`。一名学生每轮只能选择一项任务；不同轮次互不影响。
- “插件的输入为一个包，输入为分数”按上下文理解为：**输入是学生提交的作品包，输出是分数**。插件内部如何评分由插件作者定义；平台只规定传输、安全和输出协议。
- 目标是可审计的报名、提交、自动评测和人工复核流程，不将插件代码与 Web 服务编译在一起。

## 2. 角色与业务流程

| 角色 | 首版权限 |
| --- | --- |
| 学生 | 注册/登录；查看已发布任务；在开放期内选择任务；上传自己的简历和作品包；查看自己的提交、状态及分数 |
| 管理员 | 创建轮次和任务；选择已登记插件版本和配置；查看报名材料、评分与错误摘要；重试失败作业 |
| 插件维护者（部署角色） | 登记、测试和发布经过审核的插件镜像；不能直接查看所有学生材料 |

当前流程：管理员创建轮次与任务 → 配置截止时间及评测插件 → 发布 → 学生登录并选择任务 → 上传简历和作品包 → 截止前提交 → 系统创建评测作业 → worker 调用插件 → 保存分数/详情 → 管理员查看或重试失败作业。人工复核属于后续阶段。

状态建议：轮次 `draft/open/closed/archived`；任务 `draft/published/closed`；提交 `draft/submitted/evaluating/evaluated/failed`；评测作业 `queued/running/succeeded/failed`。**插件失败不等于得 0 分**，两者必须分开表示。首版选定任务后不允许学生自行换题，管理员可审计地重置；如需自行换题，再设计名额迁移事务。

## 3. 首版功能边界

### 总体路线图目标（部分尚未进入 v0.1）

1. 账户与权限：学生、管理员两种业务角色；登录会话；管理员操作审计。
2. 招新轮次与任务：标题、说明、附件、开放/截止时间、可选名额上限、发布状态、插件版本及 JSON 配置。截止时间统一存 UTC，展示按配置时区。
3. 单任务选择：数据库强制一人每轮一条选择记录；若设名额上限，不能超额。
4. 文件提交：简历独立上传（建议 PDF）；作品包作为单个 ZIP 上传；记录文件名、大小、SHA-256、对象存储 key、上传时间。作品包在截止前可替换，保留提交版本及审计信息。
5. 自动评测：提交完成后创建作业；后台评测；展示状态、分数、插件版本、错误摘要；管理员可复核并手动重新评测。
6. 基础运维：结构化日志、健康检查、数据库迁移、备份与恢复演练、请求限流和核心并发测试。

### 暂不进入首版

校内统一身份认证、邮件/短信通知、复杂多轮面试、学生自行切换题目、插件市场、管理员从浏览器直接上传任意可执行程序、实时排行榜。保留接口扩展空间，但不提前引入微服务或 Redis。

## 4. 技术栈与工程要求

| 层 | 选型与要求 |
| --- | --- |
| 语言/HTTP | Go 1.27.x；Gin 1.12.x；`net/http` 超时配置、路由分组、认证/权限与恢复已实现；请求 ID、结构化请求日志和限流待补 |
| 页面/API | React 19 + Ant Design 6 + Vite 8；Gin 提供 `/api/v1` JSON API，构建后的静态文件可由 Gin 托管 |
| 数据库 | PostgreSQL 18.x；`pgx/v5` 连接池；当前使用参数化 SQL 与初始 migration；`sqlc` 为后续可选改进 |
| 文件 | 初版使用服务端私有本地目录，数据库只保存元数据及不透明文件 key；下载需鉴权。后续可替换为 S3 兼容对象存储 |
| 后台任务 | 独立 Go worker；PostgreSQL 作业表 + `FOR UPDATE SKIP LOCKED` 领取；首版不增加 Redis/RabbitMQ |
| 插件执行 | OCI 镜像；独立受限 runner；Web 进程不能直接运行用户上传代码，也不接触宿主机容器管理接口 |
| 配置/部署 | 环境变量与配置校验、本地 PostgreSQL Compose 已实现；生产镜像、HTTPS、独立数据库账户、密钥管理和定期备份待部署 |
| 质量 | `go test`、`go test -race`、`golangci-lint`（版本固定）、migration 检查、API/数据库集成测试、并发冲突测试 |

版本基线按 2026-11-05 的官方发布信息设定：Go 1.27.3、Gin 1.12.0、PostgreSQL 18.6。项目初始化时固定具体版本；PostgreSQL 19 此时仍为候选版本，不用于首版生产。

建议目录：`cmd/api`、`cmd/worker`、`internal/auth`、`internal/campaign`、`internal/submission`、`internal/evaluation`、`internal/storage`、`internal/http`、`db/migrations`、`db/queries`、`web/templates`、`web/static`、`plugins/examples`、`docs`。保持模块化单体；API 和 worker 共用业务逻辑。

## 5. 核心数据模型与约束

| 表 | 关键字段 / 约束 |
| --- | --- |
| `users` | `id`, `email UNIQUE`, `password_hash`, `role`, `created_at`；上线前确定是否替换为校内 SSO |
| `campaigns` | `id`, `name`, `status`, `opens_at`, `closes_at`, `timezone` |
| `tasks` | `id`, `campaign_id`, `title`, `status`, `capacity NULL`, `selected_count`, `plugin_version_id`, `plugin_config`, `deadline_at` |
| `plugin_versions` | `id`, `name`, `version`, `image_ref`, `image_digest`, `config_schema`, `status`；镜像以 digest 固定 |
| `selections` | `id`, `campaign_id`, `student_id`, `task_id`, `current_submission_id`, `created_at`, `UNIQUE(campaign_id, student_id)`；通过复合外键保证 task 与 campaign 一致 |
| `files` | `id`, `owner_id`, `kind`, `object_key UNIQUE`, `sha256`, `size_bytes`, `content_type`, `created_at` |
| `submissions` | `id`, `selection_id`, `version`, `resume_file_id`, `package_file_id`, `status`, `submitted_at`, `UNIQUE(selection_id, version)` |
| `evaluation_jobs` | `id`, `submission_id`, `plugin_version_id`, `plugin_config_snapshot`, `status`, `attempt`, `lease_token`, `lease_until`, `idempotency_key UNIQUE`, `created_at` |
| `evaluation_results` | `id`, `job_id UNIQUE`, `score NUMERIC(5,2)`, `details JSONB`, `error_code`, `finished_at`；成功才允许非空分数 |
| `audit_events` | `actor_id`, `action`, `target_type`, `target_id`, `metadata`, `created_at` |

数据库是选择、状态和评分的权威来源；对象存储是文件的权威来源。上传后再以事务关联文件元数据和提交记录；未关联对象由定时清理作业回收。插件版本及配置在提交时快照到评测作业，后续管理员修改任务设置不改变历史结果。

## 6. 插件协议 v1

管理员只能从**已审核并登记**的插件版本中选择评测方式，填写符合该插件 `config_schema` 的 JSON 配置。插件作者可自由定义内部语言、评分算法和资源使用方式，但必须遵守统一协议：

1. 输入：一个学生作品 ZIP 包（只读挂载为 `/input/submission.zip`），以及标准输入中的 JSON 元数据：`protocol_version`、`job_id`、`task_id`、`submission_id`、`config`。**简历不默认传给插件**，避免不必要的个人信息暴露。
2. 输出：标准输出**仅一行 JSON**，例如 `{"protocol_version":"1","score":87.5,"details":{"tests_passed":7,"tests_total":8}}`。`score` 必须是有限数值且在 0–100 内；平台负责验证、保存和显示，`details` 限定大小。管理员可单独设置通过阈值，插件不决定录取。
3. 错误：非零退出码、超时、非法 JSON 或分数越界均记录为评测失败；保留受长度限制的诊断日志，可按策略重试，不写入 0 分。
4. 执行：当前通过已登记的镜像标签运行，禁用默认网络、只读根文件系统、非 root 用户、CPU/内存/PID/运行时长限制；Web 服务与 worker 隔离。作品包大小、压缩比、展开大小和文件路径受限。生产阶段仍需固定镜像 digest。
5. 重跑：每次执行都有独立 `job_id` 和尝试次数；同一成功作业只生成一条结果。插件可以是不同语言实现，只需遵守该协议。

不使用 Go 标准库 `plugin` 动态库机制作为扩展点：此场景需要隔离处理不可信作品包，并允许插件独立升级、跨语言实现。

## 7. 并发、一致性和安全

- **抢任务**：选择任务放在一个事务内。锁定目标 `tasks` 行，核对发布状态、轮次开放时间和可选名额，再插入 `selections` 并更新 `selected_count`；唯一约束兜底。重复点击返回已有选择；提交接口使用请求幂等键。
- **提交与截止**：以数据库时间判断截止。`submissions` 的版本递增、`selections.current_submission_id` 切换和评测作业创建在同一事务内完成；重复请求不得产生两个当前版本。每次正式提交均可评测，但只展示当前版本的结果，旧版本留作审计。作品包上传完成后才允许提交。
- **评测领取**：worker 通过 `FOR UPDATE SKIP LOCKED` 批量领取作业并设置租约；过期租约可恢复。结果写入同时检查 `job_id`、当前 `attempt` 和租约所有者令牌，防止旧 worker 在租约失效后覆盖新结果；`job_id` 唯一结果兜底，遵循“至少一次执行、幂等落库”。
- **管理员变更**：任务发布后修改插件版本或截止时间须审计；历史提交保留当时插件版本/配置快照。容量减少不得小于已选人数。
- **隐私/安全**：密码哈希（Argon2id）、安全会话 Cookie、CSRF 防护、文件类型与大小校验、私有桶、最小权限、下载鉴权、敏感字段脱敏日志、备份加密和材料保留期限。不要仅凭 MIME 或扩展名信任上传内容。

## 8. API 草案与页面

主要页面：任务列表/详情、我的报名、文件上传与提交、结果状态；管理员的轮次/任务编辑、插件配置、报名列表、评测详情。

`/api/v1/auth/*`、`GET /campaigns/:id/tasks`、`POST /campaigns/:id/selections`、`POST /files`、`POST /selections/:id/submissions`、`GET /me/submissions`、`GET /admin/evaluations`、`POST /admin/evaluations/:id/retry`。写接口统一返回资源 ID/状态，按场景使用 `401/403/409/422/429`，并支持 `Idempotency-Key`。正式编码前用 OpenAPI 固定请求、响应及错误码。

## 9. 分阶段交付与验收

| 阶段 | 交付物 | 完成标准 |
| --- | --- | --- |
| P0 基础 | Go module、Gin 服务、PostgreSQL schema/migrations、配置、Compose、CI | 一键启动；健康检查可用；迁移可回滚；基本测试通过 |
| P1 报名 | 账户权限、轮次/任务、单任务选择、学生/管理页面 | 同一学生并发选择不能产生两条记录；容量不会超额；越权访问被拒绝 |
| P2 提交 | 简历/作品包私有存储、提交版本、截止校验 | 文件与元数据对应；重复提交幂等；截止后不可提交；断点/失败上传不污染有效提交 |
| P3 评测 | 插件协议、示例插件、隔离 runner、worker、结果/重试 | 同一作业最多一条有效结果；失败显示为失败；多 worker 不重复认领有效租约 |
| P4 上线 | 审计、监控、压测、备份恢复、隐私与安全检查 | 关键并发与异常场景通过；可从备份恢复；运维手册可执行 |

建议先做 P0+P1 的纵向切片，再接文件提交与真实插件。每阶段先完成数据库约束与集成测试，再补页面；避免只在前端检查“一人一题”。

## 10. 待确认的产品决策（不阻塞架构）

1. 学生身份：先用邮箱注册，还是对接校内统一登录？默认首版邮箱注册。
2. 任务是否设置名额上限？默认可选；不设置则无限额。
3. 简历格式/大小、作品包大小、材料保留期限？默认先按 PDF/ZIP 与管理员可配置上限设计，具体数值在实现前确定。
4. 分数何时向学生公开、是否允许多次提交与多次评分？v0.1 允许截止前多次提交，每版均评测，只展示最新版本结果，并在评测成功后立即向学生公开分数；延迟公开开关留待后续。
5. 插件由谁发布和审核？默认由部署维护者登记镜像，管理员只选择版本与配置。
