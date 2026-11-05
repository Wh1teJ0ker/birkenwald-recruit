# Birkenwald 招新系统协作规范

## 项目基线

- 产品名：Birkenwald 招新系统（Birkenwald Recruit）；首版版本号：v0.1.0，版本号锁定，修改需同步 `VERSION`、Go `platform.Version` 和前端包版本。
- 后端：Go + Gin；数据库：PostgreSQL；前端：React + Ant Design + Vite。
- 运行环境：仅支持 Linux amd64，CGO 必需；API、worker 与 Go plugin 必须使用同一 Linux 架构和工具链构建。
- 一名学生在同一招新轮次只能选择一项任务。提交材料是简历 PDF 与作品 ZIP；评测插件读取作品包，返回 0–100 分或明确的失败状态。
- 以 `docs/PROJECT_PLAN.md` 说明需求与非目标；以 `README.md` 说明当前实际可运行的功能。文档不可将规划项写成已实现项。

## 开发顺序

1. 先为需求和缺陷编写或更新测试文件，明确成功、失败、权限、截止时间、并发与幂等条件。
2. 再修改实现，直到对应测试通过；不要删掉失败断言来掩盖缺陷。
3. 每次改变 API、数据约束或插件协议，同步更新相关测试和文档。
4. 提交前运行 `./scripts/check-version.sh`、`go test ./...`、`go test -race ./...`、前端测试与构建；数据库集成测试在设置 `TEST_DATABASE_URL` 时运行。CI 仅在 Linux amd64 上验证；推送与 `VERSION` 一致的 `v*` tag 会触发 Release 工作流生成 Linux 压缩包和 GitHub Release，但不自动部署。

## 工程约束

- 学生与管理员权限在 API 层校验；数据库约束为一人一题、容量和提交幂等兜底。涉及多表的状态变更必须用同一事务。
- 以数据库时间判断开放与截止。worker 使用 PostgreSQL 租约和令牌防止过期执行覆盖新结果。
- 插件遵循协议 v1：worker 通过 Go `plugin.Open` 加载任务登记的 `.so`，调用 `Evaluate(context.Context, pluginapi.Request)`；请求包含版本、作业、任务、提交、作品 ZIP 本地路径和配置。插件返回版本、0–100 分及可选详情。失败不能记为 0 分。插件与 worker 同进程、同权限，只允许加载可信插件。
- 插件源码上传与编译仅限管理员：只接受单个 `.go` 或包含 `.go` 文件的 `.zip`（非 Go 文件忽略），拒绝 cgo、第三方模块、隐藏或下划线开头的 Go 文件、路径穿越和超限归档；编译必须有超时并限制输出。编译在 API 主机执行，不是安全沙箱，只能由可信管理员上传可信源码。上传生成的 `.so` 存入 `UPLOAD_DIR/plugins`，API 与 worker 必须能访问同一路径；生产环境若只部署二进制，应使用手工登记 `.so` 的方式。
- 文件必须私有保存，上传校验大小、类型与 ZIP 风险，下载按身份授权。不得提交真实简历、密钥、`.env`、`data/`、`frontend/node_modules/` 或构建产物。
- 测试数据库必须独立于生产数据库。集成测试使用临时 schema 并在结束时清理；Go plugin 测试会现场编译示例 `.so`，需要 CGO 和 C 编译器。
- 保持小而可审查的改动；不要为了首版引入微服务、消息队列或额外缓存。
