# Emby_Bot_Go — 快速上手

必备环境（最低要求）
- Go 1.21（看 `go.mod`）
- Telegram Bot Token（去 @BotFather 拿）
- 如果要存数据：MySQL（项目用 GORM）
- 可选：Docker / docker-compose（想省心用容器）


2. 在项目根建个 `.env`（开发用），把常用环境变量写上：

```text
TELEGRAM_TOKEN=你的_bot_token
MYSQL_DSN=user:pass@tcp(127.0.0.1:3306)/emby_bot?charset=utf8mb4&parseTime=True&loc=Local
也可以使用
MYSQL_HOST=
MYSQL_PORT=3306
MYSQL_USER=
MYSQL_PASSWORD=
MYSQL_DATABASE=

EMBY_URL=http://your-emby-host:8096
EMBY_API_KEY=your_emby_api_key

#注意：配置参数在.env中直接修改
```

3. 使用教程
将项目下载并解压某个文件夹（以/root/Bot举例）


```bash

cd /root/Bot
```

```bash
docker-compose up --build -d
```

配置说明
- 环境变量由 `config/config.go`、`config/dotenv.go` 读取。
- Telegram 相关逻辑在 `transport/telegram`。
- Emby 调用在 `infrastructure/emby`。
- 数据模型和仓库在 `infrastructure/persistence/gormdb`。

开发快速指南
- 新增/改命令：看 `transport/telegram/router`。
- 业务逻辑：`application`、`account`、`admin` 等包。
- 数据访问：`infrastructure/persistence/gormdb/repo`。
- Emby API：`infrastructure/emby/client.go`。
