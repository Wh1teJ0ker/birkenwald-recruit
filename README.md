# Birkenwald 招新系统

**Birkenwald Recruit · v0.1.0**。面向学生招新的可运行 MVP：管理员发布轮次和任务、选择已登记的评测插件及配置；学生每轮选择一项任务，提交简历 PDF 与作品 ZIP；独立 worker 调用容器插件并展示分数或失败状态。

## 技术栈

- 后端：Go 1.27.3、Gin 1.12、pgx/v5、PostgreSQL 18.6（Compose 默认版本）。
- 前端：React 19、Ant Design 6、Vite 8。
- 评测：PostgreSQL 作业队列、Go worker、OCI/Docker 插件。示例插件检查 ZIP 中指定文件是否存在。
- 工程规范见 [AGENT.md](AGENT.md)；范围与后续计划见 [项目规划](docs/PROJECT_PLAN.md)。

## 本地启动

需要 Go、Node.js 20.20.2+、npm、Docker 和 Docker Compose。以下命令均从仓库根目录执行，前端命令除外。

```bash
cp .env.example .env
# 编辑 .env：设置独有且不少于 12 位的 ADMIN_PASSWORD，并确认数据库端口未被占用。
docker compose up -d db
docker build -f plugins/example/Dockerfile -t birkenwald-example:0.1.0 .
set -a; source .env; set +a
go run ./cmd/api
```

在另一个终端以同样方式加载 `.env`，再启动评测进程：

```bash
set -a; source .env; set +a
go run ./cmd/worker
```

开发时前端单独运行，访问 Vite 输出的本地地址；`frontend/vite.config.js` 将 `/api` 代理到 `127.0.0.1:8080`。API 启动时自动执行初始 migration，并按 `ADMIN_EMAIL`/`ADMIN_PASSWORD` 创建首次管理员账号。

```bash
cd frontend
npm ci
npm run dev
```

要让 Gin 直接提供页面，先运行 `cd frontend && npm run build`，再从仓库根目录启动 API，访问 `http://127.0.0.1:8080/`。健康检查为 `GET /healthz`，版本接口为 `GET /api/v1/version`。

## 首版流程

1. 管理员登录，创建开放轮次，再创建任务，填写截止时间、可选名额、插件版本及 JSON 配置并发布。
2. 学生注册登录，在开放轮次中选一项任务。数据库唯一约束和任务行锁防止同一学生重复选题、名额超额。
3. 学生上传简历 PDF（最多 10 MiB）与作品 ZIP（最多 50 MiB），再提交。每次提交需要 `Idempotency-Key`；截止前可提交新版本。
4. Worker 领取作业，向插件的标准输入发送协议 v1 JSON，并把作品 ZIP 只读挂载到 `/input/submission.zip`。插件输出一行 JSON，包含 `protocol_version`、0–100 的 `score` 及可选 `details`。管理员可重试失败作业。

示例插件的任务配置可填 `{"required_files":["README.md","main.go"]}`。插件无法运行、超时或返回非法结果时，状态为失败，**不会写入 0 分**。

## 测试

```bash
go test ./...
go test -race ./...
cd frontend && npm ci && npm test && npm run build
```

数据库集成测试默认跳过；设置仅供测试的 `TEST_DATABASE_URL` 后运行。它会在该数据库中创建并清理临时 schema。Docker 插件测试默认跳过；先构建示例镜像，再设置 `TEST_DOCKER_PLUGIN_IMAGE=birkenwald-example:0.1.0`。CI 会启用两类测试。

## 安全与限制

- 生产环境必须提供 HTTPS，并将 `COOKIE_SECURE=true`；妥善保护数据库、上传目录和 Docker 权限。API 与 worker 必须能访问同一持久化上传目录。
- 首版仅允许部署方登记插件镜像，管理员可选择版本与任务配置；不允许在网页上传可执行插件。当前示例镜像通过标签引用，正式生产部署应固定镜像 digest 并完成审核。
- 首版未包含校内 SSO、通知、录取审批、学生换题、附件清理与对象存储。规划文档中的后续功能不代表已实现。
