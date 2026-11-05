# Birkenwald 招新系统

**Birkenwald Recruit · v0.1.0**。面向学生招新的可运行 MVP：管理员发布轮次和任务、选择已登记的评测插件及配置；学生每轮选择一项任务，提交简历 PDF 与作品 ZIP；独立 worker 通过 Go plugin 动态加载评测插件并展示分数或失败状态。

## 技术栈

- 后端：Go 1.27.3、Gin 1.12、pgx/v5、PostgreSQL 18.6（Compose 默认版本）。
- 前端：React 19、Ant Design 6、Vite 8。
- 评测：PostgreSQL 作业队列、Go worker、Go plugin（`plugin.Open` 加载 `.so`）。示例插件检查 ZIP 中指定文件是否存在。
- 运行环境：仅支持 **Linux amd64**，需要 CGO 和 C 编译器；API、worker 与插件必须使用同一 Linux 架构构建。
- 工程规范见 [AGENT.md](AGENT.md)；范围与后续计划见 [项目规划](docs/PROJECT_PLAN.md)。

## 本地启动

需要 Go、Node.js 20.20.2+、npm、Docker 和 Docker Compose。以下命令均从仓库根目录执行，前端命令除外。部署目标为 Linux amd64；在 macOS 等开发机上可以开发页面和普通后端逻辑，但插件必须在目标 Linux 环境中重新编译，API 与 worker 也只支持 Linux 运行。

```bash
cp .env.example .env
# 编辑 .env：设置独有且不少于 12 位的 ADMIN_PASSWORD，并确认数据库端口未被占用。
# 本地注册需要邮箱验证码：将 EMAIL_DEV_MODE 设为 true（验证码显示在页面并写入日志），
# 或配置 SMTP_HOST/SMTP_PORT/SMTP_USERNAME/SMTP_PASSWORD/SMTP_FROM 使用真实邮件。
# 启动后也可在管理台「基础设置 → 邮件与注册」中填写 SMTP，页面配置优先于环境变量。
docker compose up -d db
# 编译示例 Go 插件（需要 CGO，并与 worker 使用同一 Go 版本）
go build -buildmode=plugin -o plugins/example/example.so ./plugins/example
set -a; source .env; set +a
go run ./cmd/api
```

在另一个终端以同样方式加载 `.env`，再启动评测进程：

```bash
set -a; source .env; set +a
go run ./cmd/worker
```

开发时前端单独运行，访问 Vite 输出的本地地址；`frontend/vite.config.js` 将 `/api` 代理到 `127.0.0.1:8080`。API 启动时自动执行数据库 migration，并按 `ADMIN_EMAIL`/`ADMIN_PASSWORD` 创建首次管理员账号。

```bash
cd frontend
npm ci
npm run dev
```

要让 Gin 直接提供页面，先运行 `cd frontend && npm run build`，再从仓库根目录启动 API，访问 `http://127.0.0.1:8080/`。健康检查为 `GET /healthz`，版本接口为 `GET /api/v1/version`。

插件页的“上传并编译”要求 API 从源码仓库根目录启动，并且主机安装 Go 工具链和 C 编译器；编译生成的 `.so` 保存在 `UPLOAD_DIR/plugins`，登记的是绝对路径，因此 API 和 worker 必须能访问同一路径。只部署编译后二进制的生产环境应继续使用“登记服务器上的 .so”方式。

## 首版流程

1. 管理员登录，创建开放轮次，再创建任务，填写截止时间、可选名额、插件版本及 JSON 配置并发布。
2. 学生用邮箱验证码注册并登录，在开放轮次中选一项任务。数据库唯一约束和任务行锁防止同一学生重复选题、名额超额。
3. 学生上传简历 PDF（最多 10 MiB）与作品 ZIP（最多 50 MiB），再提交。每次提交需要 `Idempotency-Key`；截止前可提交新版本。
4. Worker 领取作业，读取任务指定的 `plugin_versions.library_path`，通过 `plugin.Open` 加载 `.so`，调用插件的 `Evaluate(ctx, Request)`；`Request` 包含作品 ZIP 的本地路径、任务 ID 和配置快照。插件返回协议 v1 结果，包含 0–100 的 `score` 及可选 `details`。管理员可重试失败作业。

示例插件源码在 `plugins/example`，任务配置可填 `{"required_files":["README.md","main.go"]}`。插件与 worker 必须使用相同的 Go 版本和依赖编译；加载失败、超时或返回非法结果时状态为失败，**不会写入 0 分**。

## 管理台

管理员登录后通过左侧导航使用六个区块：

- **概览**：轮次、任务、提交和失败评测统计，以及当前轮次摘要。
- **轮次与任务**：创建轮次与任务；开放/关闭轮次；编辑任务标题、说明、截止时间、名额和状态，发布或关闭任务。关闭后学生不能继续选题或提交，已有材料保留。
- **插件**：上传单个 `.go` 或包含 `.go` 文件的 `.zip`，由 API 主机运行 `go build -buildmode=plugin` 编译并自动登记；也可继续登记服务器上已有的 `.so` 路径。编译仅接受标准库和 `pluginapi` 导入，禁止 cgo、隐藏文件和压缩包路径穿越；上传大小、解压大小、文件数和编译时间均有限制。插件页同时提供示例插件压缩包（含 `main.go`、`README.md` 和供 agent 阅读的 `AGENTS.md`）与已编译 `.so` 下载链接。启用或停用已有版本，并可修正已登记版本的路径与 Schema。worker 在领取新评测时实时读取路径，注册新路径无需重启；替换同一个 `.so` 文件后需重启 worker（Go plugin 会缓存已加载库）。插件名称和版本标识不可变，升级请注册新版本。
- **人员**：按姓名或邮箱搜索，按角色和状态筛选；可调整学生/管理员角色、禁用或启用账号。禁用会立即失效该账号的会话；管理员不能锁定自己的账号。
- **提交与评测**：查看提交、下载材料、重试失败评测。
- **基础设置**：修改站点名称和招新年份；在「邮件与注册」中开关学生注册、配置开发模式、SMTP 主机、端口、用户名、密码和发件人；查看版本、数据库、数据统计和已启用插件。站点名称与年份会同步到页头、登录页和页脚；关闭注册后验证码和注册接口都会拒绝请求。SMTP 密码只写不读，保存后不会回显。

数据库迁移 003 会在 API 启动时自动执行，为 `users` 增加禁用状态并创建 `app_settings` 保存站点设置；迁移 004 将插件登记字段从镜像引用改为 `.so` 路径；旧记录需要部署方在插件管理中修正为实际库路径。

## 测试

```bash
./scripts/check-version.sh
go test ./...
go test -race ./...
cd frontend && npm ci && npm test && npm run build
```

数据库集成测试默认跳过；设置仅供测试的 `TEST_DATABASE_URL` 后运行。它会在该数据库中创建并清理临时 schema。Go plugin 测试会现场把 `plugins/example` 编译为 `.so` 并加载，需要 CGO、可用的 C 编译器和 Go 工具链；CI 会启用这两类测试。

## CI/CD

GitHub Actions 仅在 `ubuntu-24.04` 上运行，固定 `GOOS=linux`、`GOARCH=amd64`，执行以下门禁：

- 版本一致性：`VERSION`、`frontend/package.json`、`frontend/package-lock.json` 必须为 `0.1.0`；发布 tag 必须是 `v0.1.0`。
- 代码质量：`gofmt`、`go vet`。
- 测试：带 PostgreSQL 的 `go test -race ./...`，以及前端 `npm ci && npm test`。
- 构建：Linux API、worker 和示例 `.so`，以及前端生产构建。

当前版本锁定为 `v0.1.0`。CI 不自动部署；发布以上述检查全部通过为前提，部署仍由维护者手动执行。

推送版本 tag 会触发发布工作流：

```bash
git tag v0.1.0
git push origin v0.1.0
```

`Release` 工作流会再次校验版本、运行 Linux 测试与前端构建，生成 `birkenwald-recruit-v0.1.0-linux-amd64.tar.gz` 和 `SHA256SUMS`，并创建 GitHub Release。压缩包包含 Linux API/worker、示例 `.so`、前端静态文件、README、AGENT、版本文件、`.env.example`，以及用于运行时编译上传插件的 `pluginapi` 和最小 `go.mod`。发布 tag 必须与 `VERSION` 一致。仅运行预编译插件不需要 Go；若要在管理台上传源码编译，目标主机还需安装 Go 1.27.3 和 C 编译器。

## 安全与限制

- 生产环境必须提供 HTTPS，并将 `COOKIE_SECURE=true`；妥善保护数据库、上传目录和插件文件权限。API 与 worker 必须能访问同一持久化上传目录，worker 必须能读取登记的 `.so` 路径。运行时仅支持 Linux amd64。
- 注册验证码 10 分钟内有效，同一邮箱 60 秒内只能发送一次，最多尝试 5 次；数据库只保存 Argon2 哈希。生产环境必须保持 `EMAIL_DEV_MODE=false` 并配置 SMTP；开发模式会通过接口返回 `dev_code`，只能用于本地调试。
- 管理员可在基础设置中关闭学生注册；生产环境如需长期关闭，也应同步移除或限制验证码邮件配置。禁用账号会立即删除其会话，但不会删除历史报名和提交记录。
- 管理台填写的 SMTP 密码以明文保存在 `app_settings` 表中，接口不会返回密码；请限制数据库访问并保护备份。留空 SMTP 字段会回退到环境变量配置。
- 管理员可在插件页上传 `.go` 或 `.zip` 源码，由 API 主机编译为 `.so`。编译进程与 API 同权限且不是沙箱；上传源码只允许可信管理员和可信代码，接口会拒绝 cgo、第三方模块导入、隐藏文件、路径穿越，并限制请求、解压、文件数、输出大小和编译时间。生成的插件仍由 worker 通过 `plugin.Open` 加载，运行在 worker 进程内，拥有 worker 的用户权限以及文件、网络访问能力，**没有容器隔离**；只能加载经过审核的可信插件。插件必须与 worker 使用相同的 Go 版本和依赖编译，替换同名 `.so` 后需重启 worker。插件超时会被记录为失败，但 Go 无法强制终止仍在运行的插件代码；超时后该插件路径会在当前 worker 进程中禁用，需重启 worker 恢复，插件应响应 `context` 取消。
- 首版未包含校内 SSO、通知、录取审批、学生换题、附件清理与对象存储。规划文档中的后续功能不代表已实现。
