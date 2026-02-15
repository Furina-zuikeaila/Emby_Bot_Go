# Emby_Bot_Go — 简明大白话使用与开发说明

说明写给想用或改这个项目的人：不啰嗦、一步步来、中文大白话。

**项目简介**
- 这是一个用 Go 写的 Telegram 机器人，主要和 Emby（媒体服务器）配合做一些资源管理、通知、面板操作等功能。
- 如果你熟悉 Telegram 机器人和 Emby，这个仓库可以直接用；如果不熟，也能跟着下面步骤跑起来。

**适合谁看**
- 想把 Emby 跟 Telegram 结合的人。
- 会一点 Go、会部署到 VPS 或者会用 Docker 的人。
- 想二次开发/加功能的开发者。

**最重要的前提（环境）**
- Go 版本：go 1.21（仓库 go.mod 声明）。
- 需要一个 Telegram Bot Token（@BotFather 创建）。
- 如果用数据库：MySQL（仓库使用 GORM + mysql driver）。
- 可选：Docker / docker-compose（如果不想本地装依赖，用容器更方便）。

**仓库里大致有什么（常看文件）**
- `cmd/bot/main.go`：程序入口，启动机器人。
- `config/`：配置加载，例如 `.env` 的读取等（看 `config.go`、`dotenv.go`）。
- `transport/telegram/`：Telegram 相关路由和处理器，命令、面板都在这里。
- `infrastructure/persistence/gormdb/`：数据库模型、仓库实现（GORM）。
- `infrastructure/emby/`：Emby 客户端封装（请求 Emby API 的地方）。
- `application/`、`account/`、`admin/` 等：领域逻辑和服务实现。

如果要改功能，大概率是从 `transport/telegram/router` 和 `infrastructure/emby` 下面动手。

**快速开始（本地跑）**
1. 克隆代码到你的机器：

   git clone <仓库地址>
   cd Emby_Bot_Go

2. 把需要的环境变量准备好。一般需要：
- `TELEGRAM_TOKEN`：Telegram Bot Token
- `MYSQL_DSN`：MySQL 连接串，例如 `user:pass@tcp(host:port)/dbname?charset=utf8mb4&parseTime=True&loc=Local`
- 以及 Emby 的地址和 API Key（如果有功能依赖 Emby），这些具体字段项目里会读取 `config` 下的配置，默认从环境变量载入。

你可以直接在项目根创建一个 `.env`（开发机专用，生产不要把真实秘钥放到仓库）示例：

TELEGRAM_TOKEN=123456:ABC-DEF
MYSQL_DSN=user:pass@tcp(127.0.0.1:3306)/emby_bot?charset=utf8mb4&parseTime=True&loc=Local
EMBY_URL=http://your-emby-host:8096
EMBY_API_KEY=your_emby_api_key

3. 安装依赖并编译（Go 环境已装）

   go mod download
   go build -o emby_bot ./cmd/bot

4. 运行（开发时可直接 run）：

   go run ./cmd/bot

如果一切正常，机器人会启动并连接到 Telegram；看控制台日志确认是否成功登录。

**用 Docker / docker-compose（推荐在服务器上）**
- 仓库根有 `docker-compose.yml`（如果你想用容器，把 `.env` 放好）：

   docker-compose up --build -d

- 用 Docker 时确保 `.env` 被 docker-compose 读取，或者把环境变量写到 `docker-compose.yml` 的 environment 下。

**配置细节 / 常见配置项在哪里**
- 配置读取在 `config/config.go`、`config/dotenv.go`。如果想增加配置项，按那里现有方式添加，然后在 `.env` 里写入即可。

**数据库**
- 项目用 GORM，模型在 `infrastructure/persistence/gormdb/models`。
- 第一次运行请确保数据库和表结构存在；项目可能没有自动迁移（看代码是否调用 AutoMigrate），如果没有，你需要手动建表或在代码里加迁移。

**日志和调试**
- 项目有简单日志（查 `infrastructure/persistence/gormdb/logger`），运行时看控制台日志能快速定位问题。
- 调试 Telegram 消息处理，可以在 `transport/telegram/router` 下添加打印或断点。

**开发流程（给想改代码的你）**
1. 先在本地创建 `.env`，能跑通最关键。
2. 修改功能：通常从 `transport/telegram` 的命令处理器入手，再调整 `application` 层实现业务逻辑。
3. 涉及数据变化，去 `infrastructure/persistence/gormdb/repo` 修改/新增仓库方法。
4. 涉及 Emby API，修改 `infrastructure/emby/client.go`。
5. 写完后运行 `go test ./...`（仓库有测试文件的话会跑），再本地跑一次机器人确认功能。

**运行测试**
- 运行所有测试：

   go test ./...

（注意：如果测试依赖外部服务或 DB，可能需要先准备测试环境或 mock）

**部署建议**
- 生产环境推荐用 Docker 部署，使用 docker-compose 或者把镜像推到私有仓库再在服务器拉取。
- 不要把真实凭据放在仓库里。生产用环境变量或 Secret 管理工具。
- 建议使用 systemd 或容器编排（Docker Compose / Kubernetes）保证服务重启。

**排错小贴士（遇到问题先看这些）**
- 机器人不在线：检查 `TELEGRAM_TOKEN` 是否正确，程序启动日志里会有登录信息。
- 数据库连接失败：检查 `MYSQL_DSN`，确认能从服务器/容器访问 MySQL。
- Emby 相关功能失败：确认 `EMBY_URL` 与 `EMBY_API_KEY` 可用，直接用 curl 测试 Emby API。

**贡献指南**
- 欢迎提 Issue 或 PR。简单的改动可以直接 PR，复杂改动先开 Issue 说明设计。
- 遵守现有代码风格，写明变更点，必要时附上运行/测试步骤。

**许可证**
- 参见仓库根的 LICENSE 文件。

---

如果你愿意，我可以：
- 把项目里 `config` 的所有环境变量列成一个示例 `.env.example`；
- 或者帮你生成一个最小可运行的 `.env` 示例并演示本地运行步骤。

现在我把 README 写进仓库根，下一步是把 TODO 列表更新为“已完成 README 生成”。