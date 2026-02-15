// Package bootstrap 负责“装配/启动”应用：
//
// - 读取配置：从环境变量（以及可选的 .env 文件）加载运行参数。
// - 初始化基础设施：数据库连接（GORM/MySQL）、Emby HTTP 客户端、Telegram Bot 客户端。
// - 构建仓储与领域服务：把 persistence 层的 repository 组合为 application 层的 Service。
// - 统一注入依赖：把最终可运行的对象（Bot/Repo/Service/Config 等）收敛到 App 结构体中，供 cmd 入口使用。
//
// 设计要点：
// - bootstrap 层不承载业务规则；它只做“组装”和“启动前的最小校验”。
// - 对外暴露的错误信息应避免包含敏感信息（例如 DSN、API Key、URL 等）。
// - 初始化失败应尽早返回，让 cmd 入口决定如何记录日志与退出。
package bootstrap
