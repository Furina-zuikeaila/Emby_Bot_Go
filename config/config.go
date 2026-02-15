package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	// AppEnv 用于区分运行环境（dev/prod 等）。
	// 主要影响安全策略与默认行为（例如 https + prod 下强制关闭 InsecureSkipVerify）。
	AppEnv string

	// Telegram 为 Telegram Bot 侧配置（Token/管理员等）。
	Telegram TelegramConfig
	// Govern 为社区治理配置（加群/加频道校验、反机器人、群内命令治理等）。
	Govern GovernanceConfig
	// Crowdfund 为众筹/赞助配置（TRC20 交易校验 + 发放 0 天邀请码）。
	Crowdfund CrowdfundConfig
	// Invite 为“用户邀请（/Harem）”相关配置。
	Invite InviteConfig
	// Emby 为 Emby 服务端连接信息。
	Emby EmbyConfig
	// DB 为数据库连接配置（支持 DSN 或分字段拼装）。
	DB DBConfig

	// HTTP 为进程附加 HTTP 服务配置（目前用于 /health）。
	HTTP HTTPConfig

	// Scheduler 为后台定时任务配置。
	Scheduler SchedulerConfig
}

type TelegramConfig struct {
	BotToken    string
	BotUsername string
	OwnerID     int64
	AdminIDs    []int64
}

type GovernanceConfig struct {
	Enabled bool

	GroupIDs            []int64
	MainGroupUsername   string
	MainGroupInviteLink string

	ChannelID         int64
	ChannelUsername   string
	ChannelInviteLink string

	RequireGroup   bool
	RequireChannel bool

	Strict        bool
	RevokeOnLeave bool
	BanOnLeave    bool

	AntiUseBot bool

	AntiChannel             bool
	AntiChannelWhitelistIDs []int64

	// GuardGroupCommands 用于开启“群内命令治理（误用防护）”：
	// - 当普通用户在群里使用 bot 命令时，机器人会删除该消息；
	// - 如果该用户已注册/已绑定 Emby，则会按治理策略执行删号并私信通知。
	//
	// 目的：
	// - 防止群内刷命令影响体验；
	// - 把 bot 命令交互尽量收敛到私聊面板与管理员操作。
	GuardGroupCommands bool
}

// CrowdfundConfig 控制“众筹支持”功能（TRC20）：
// - 用户转账后发送交易哈希（TxID）
// - 机器人校验“收款地址/合约/金额”匹配后，发放 1 枚 0 天邀请码
//
// 注意：
// - 交易校验需要访问公网 API（默认 TronScan）。
// - 建议在启用前先手动验证 API 可用性与字段配置是否正确。
type CrowdfundConfig struct {
	Enabled bool

	// TRC20Address 为收款地址（TRON Base58 地址）。
	TRC20Address string
	// TRC20Contract 为 TRC20 合约地址（例如 USDT TRC20）。
	TRC20Contract string
	// TRC20Symbol 为展示用币种符号（例如 USDT）。
	TRC20Symbol string
	// TRC20Decimals 为最小单位精度（USDT TRC20 通常为 6）。
	TRC20Decimals int

	// MinAmount 为自动发放注册码的门槛（十进制字符串），例如 "1" / "10.5"。
	// - 门槛语义：仅当实际金额 >= MinAmount 才会自动发放注册码。
	// - "0" 或空：表示只要求金额 > 0。
	MinAmount string

	// TronScanAPIBase 为 TronScan API 基址，例如 https://apilist.tronscanapi.com
	TronScanAPIBase string
	// HTTPTimeout 为请求超时。
	HTTPTimeout time.Duration
	// SearchLimit 为分页大小（用于兜底扫描 transfers 列表）。
	SearchLimit int
	// SearchMaxPages 为最多扫描页数（防止无限拉取）。
	SearchMaxPages int
}

// InviteConfig 控制“用户邀请（/Harem）”功能：
// - 用户满足条件后可生成一个邀请码并发送给目标用户；
// - 记录“谁邀请了谁”（通过 invite_codes.creator_telegram_id -> used_by_telegram_id）。
type InviteConfig struct {
	// MinAccountAgeDays 为允许发起邀请的最小账号持有天数（从注册/绑定时间起算）。
	// - 0：不限制
	MinAccountAgeDays int
	// CooldownDays 为每次邀请成功后（邀请码被使用）下一次允许邀请的冷却天数。
	// - 0：不限制
	CooldownDays int

	ReservationTTL        time.Duration
	ReservationGCInterval time.Duration
}

type EmbyConfig struct {
	BaseURL            string
	APIKey             string
	InsecureSkipVerify bool
	PublicURL          string
	// SimultaneousStreamLimit 用于限制单个 Emby 账号的“同时播放/同时会话”数量。
	// - 1：同一账号同一时间只允许 1 路播放（禁止多设备同时播放）
	// - 0：不限制（由 Emby 服务端默认策略决定）
	SimultaneousStreamLimit int
}

type DBConfig struct {
	// MySQLDSN 支持直接传入 GORM/go-sql-driver 的 DSN。
	// 为空时，会从 MYSQL_HOST/MYSQL_PORT/MYSQL_USER/MYSQL_PASSWORD/MYSQL_DATABASE/MYSQL_PARAMS 拼装。
	MySQLDSN string

	Host     string
	Port     int
	User     string
	Password string
	Database string
	Params   string
}

type HTTPConfig struct {
	HealthAddr string
}

type SchedulerConfig struct {
	JobTimeout time.Duration

	ExpiredEnabled  bool
	ExpiredInterval time.Duration

	InactiveEnabled  bool
	InactiveInterval time.Duration

	GroupEnabled  bool
	GroupInterval time.Duration

	WebClientEnabled  bool
	WebClientInterval time.Duration
	WebClientDelete   bool

	PlaybackEnabled  bool
	PlaybackInterval time.Duration
}

func Load() (Config, error) {
	_ = LoadDotenv(".env")

	minInviteDays := mustIntEnv("TG_INVITE_MIN_ACCOUNT_AGE_DAYS", 30)
	if minInviteDays < 0 {
		minInviteDays = 30
	}
	inviteCooldownDays := mustIntEnv("TG_INVITE_COOLDOWN_DAYS", 90)
	if inviteCooldownDays < 0 {
		inviteCooldownDays = 90
	}

	inviteReservationTTL := mustDurationEnv("TG_INVITE_RESERVATION_TTL", 12*time.Hour)
	if inviteReservationTTL <= 0 {
		inviteReservationTTL = 12 * time.Hour
	}
	inviteReservationGCInterval := mustDurationEnv("TG_INVITE_RESERVATION_GC_INTERVAL", 10*time.Minute)
	if inviteReservationGCInterval <= 0 {
		inviteReservationGCInterval = 10 * time.Minute
	}

	cfg := Config{
		AppEnv: getEnv("APP_ENV", "dev"),
		Telegram: TelegramConfig{
			BotToken:    strings.TrimSpace(os.Getenv("TG_BOT_TOKEN")),
			BotUsername: strings.TrimSpace(os.Getenv("TG_BOT_USERNAME")),
			OwnerID:     mustInt64Env("TG_OWNER_ID", 0),
			AdminIDs:    parseInt64CSV(os.Getenv("TG_ADMIN_IDS")),
		},
		Govern: GovernanceConfig{
			Enabled: mustBoolEnv("TG_GOV_ENABLED", false),

			GroupIDs:            parseInt64CSV(os.Getenv("TG_GROUP_IDS")),
			MainGroupUsername:   strings.TrimSpace(os.Getenv("TG_MAIN_GROUP_USERNAME")),
			MainGroupInviteLink: strings.TrimSpace(os.Getenv("TG_MAIN_GROUP_INVITE_LINK")),

			ChannelID:         mustInt64Env("TG_CHANNEL_ID", 0),
			ChannelUsername:   strings.TrimSpace(os.Getenv("TG_CHANNEL_USERNAME")),
			ChannelInviteLink: strings.TrimSpace(os.Getenv("TG_CHANNEL_INVITE_LINK")),

			RequireGroup:   mustBoolEnv("TG_REQUIRE_GROUP", false),
			RequireChannel: mustBoolEnv("TG_REQUIRE_CHANNEL", false),

			Strict:        mustBoolEnv("TG_GOV_STRICT", false),
			RevokeOnLeave: mustBoolEnv("TG_REVOKE_ON_LEAVE", false),
			BanOnLeave:    mustBoolEnv("TG_BAN_ON_LEAVE", false),

			AntiUseBot: mustBoolEnv("TG_ANTI_USE_BOT", false),

			AntiChannel:             mustBoolEnv("TG_ANTI_CHANNEL", false),
			AntiChannelWhitelistIDs: parseInt64CSV(os.Getenv("TG_ANTI_CHANNEL_WHITELIST_IDS")),

			GuardGroupCommands: mustBoolEnv("TG_GUARD_GROUP_COMMANDS", false),
		},
		Crowdfund: CrowdfundConfig{
			Enabled:         mustBoolEnv("TG_CROWDFUND_ENABLED", false),
			TRC20Address:    strings.TrimSpace(getEnv("TG_CROWDFUND_TRC20_ADDRESS", "TPUP2dSt6gHFnchFogFkCJ5HQdELZ7ykPa")),
			TRC20Contract:   strings.TrimSpace(getEnv("TG_CROWDFUND_TRC20_CONTRACT", "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t")),
			TRC20Symbol:     strings.TrimSpace(getEnv("TG_CROWDFUND_TRC20_SYMBOL", "USDT")),
			TRC20Decimals:   mustIntEnv("TG_CROWDFUND_TRC20_DECIMALS", 6),
			MinAmount:       strings.TrimSpace(getEnv("TG_CROWDFUND_MIN_AMOUNT", "1")),
			TronScanAPIBase: strings.TrimSpace(getEnv("TG_CROWDFUND_TRONSCAN_API_BASE", "https://apilist.tronscanapi.com")),
			HTTPTimeout:     mustDurationEnv("TG_CROWDFUND_HTTP_TIMEOUT", 10*time.Second),
			SearchLimit:     mustIntEnv("TG_CROWDFUND_SEARCH_LIMIT", 50),
			SearchMaxPages:  mustIntEnv("TG_CROWDFUND_SEARCH_MAX_PAGES", 10),
		},
		Invite: InviteConfig{
			MinAccountAgeDays:     minInviteDays,
			CooldownDays:          inviteCooldownDays,
			ReservationTTL:        inviteReservationTTL,
			ReservationGCInterval: inviteReservationGCInterval,
		},
		Emby: EmbyConfig{
			BaseURL:                 strings.TrimSpace(os.Getenv("EMBY_BASE_URL")),
			APIKey:                  strings.TrimSpace(os.Getenv("EMBY_API_KEY")),
			InsecureSkipVerify:      mustBoolEnv("EMBY_INSECURE_SKIP_VERIFY", false),
			PublicURL:               strings.TrimSpace(os.Getenv("EMBY_PUBLIC_URL")),
			SimultaneousStreamLimit: mustIntEnv("EMBY_SIMULTANEOUS_STREAM_LIMIT", 1),
		},
		DB: DBConfig{
			MySQLDSN: strings.TrimSpace(os.Getenv("MYSQL_DSN")),
			Host:     strings.TrimSpace(getEnv("MYSQL_HOST", defaultMySQLHost())),
			Port:     mustIntEnv("MYSQL_PORT", 3306),
			User:     strings.TrimSpace(os.Getenv("MYSQL_USER")),
			Password: strings.TrimSpace(os.Getenv("MYSQL_PASSWORD")),
			Database: strings.TrimSpace(os.Getenv("MYSQL_DATABASE")),
			Params:   strings.TrimSpace(getEnv("MYSQL_PARAMS", "charset=utf8mb4&parseTime=True&loc=Local")),
		},
		HTTP: HTTPConfig{
			HealthAddr: getEnv("HEALTH_ADDR", defaultHealthAddr()),
		},
		Scheduler: SchedulerConfig{
			JobTimeout: mustDurationEnv("SCHED_JOB_TIMEOUT", 2*time.Minute),

			ExpiredEnabled:  mustBoolEnv("SCHED_EXPIRED_ENABLED", true),
			ExpiredInterval: mustDurationEnv("SCHED_EXPIRED_INTERVAL", time.Hour),

			InactiveEnabled:  mustBoolEnv("SCHED_INACTIVE_ENABLED", false),
			InactiveInterval: mustDurationEnv("SCHED_INACTIVE_INTERVAL", 15*time.Minute),

			GroupEnabled:  mustBoolEnv("SCHED_GROUP_ENABLED", true),
			GroupInterval: mustDurationEnv("SCHED_GROUP_INTERVAL", time.Hour),

			WebClientEnabled:  mustBoolEnv("SCHED_WEBCLIENT_ENABLED", false),
			WebClientInterval: mustDurationEnv("SCHED_WEBCLIENT_INTERVAL", 15*time.Minute),
			WebClientDelete:   mustBoolEnv("SCHED_WEBCLIENT_DELETE", true),

			PlaybackEnabled:  mustBoolEnv("SCHED_PLAYBACK_ENABLED", true),
			PlaybackInterval: mustDurationEnv("SCHED_PLAYBACK_INTERVAL", 15*time.Minute),
		},
	}

	if cfg.Telegram.BotToken == "" {
		return Config{}, fmt.Errorf("missing env TG_BOT_TOKEN")
	}
	if cfg.Emby.BaseURL == "" || cfg.Emby.APIKey == "" {
		return Config{}, fmt.Errorf("missing env EMBY_BASE_URL or EMBY_API_KEY")
	}
	if u, err := url.Parse(cfg.Emby.BaseURL); err != nil {
		return Config{}, fmt.Errorf("invalid env EMBY_BASE_URL")
	} else {
		scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
		// 明确允许 http://IP:port（部分自建环境不走 HTTPS）。
		if scheme != "http" && scheme != "https" {
			return Config{}, fmt.Errorf("invalid env EMBY_BASE_URL")
		}
		// 安全加固：
		// - 对 http：TLS 配置无意义，强制关闭 InsecureSkipVerify，避免误解。
		// - 对 https 且 AppEnv=prod：即使配置了 InsecureSkipVerify 也强制关闭，降低被 MITM 的风险。
		if scheme == "http" {
			cfg.Emby.InsecureSkipVerify = false
		} else if scheme == "https" && cfg.Emby.InsecureSkipVerify && strings.EqualFold(cfg.AppEnv, "prod") {
			cfg.Emby.InsecureSkipVerify = false
		}
	}
	if cfg.Emby.PublicURL == "" {
		cfg.Emby.PublicURL = strings.TrimSpace(os.Getenv("EMBY_LINE"))
	}

	if cfg.Crowdfund.Enabled {
		if cfg.Crowdfund.TRC20Address == "" {
			return Config{}, fmt.Errorf("missing env TG_CROWDFUND_TRC20_ADDRESS")
		}
		if cfg.Crowdfund.TRC20Contract == "" {
			return Config{}, fmt.Errorf("missing env TG_CROWDFUND_TRC20_CONTRACT")
		}
		if cfg.Crowdfund.TRC20Decimals <= 0 {
			cfg.Crowdfund.TRC20Decimals = 6
		}
		if cfg.Crowdfund.SearchLimit <= 0 {
			cfg.Crowdfund.SearchLimit = 50
		}
		if cfg.Crowdfund.SearchMaxPages <= 0 {
			cfg.Crowdfund.SearchMaxPages = 10
		}
	}

	if runningInContainer() {
		if isLocalHost(cfg.DB.Host) {
			cfg.DB.Host = "host.docker.internal"
		}
		cfg.HTTP.HealthAddr = fixHealthAddrForContainer(cfg.HTTP.HealthAddr)
	}

	if cfg.DB.MySQLDSN == "" {
		cfg.DB.MySQLDSN = buildMySQLDSN(cfg.DB)
	}
	if cfg.DB.MySQLDSN == "" {
		return Config{}, fmt.Errorf("missing env MYSQL_DSN (or MYSQL_USER/MYSQL_PASSWORD/MYSQL_DATABASE for non-DSN mode)")
	}

	return cfg, nil
}

func buildMySQLDSN(db DBConfig) string {
	user := strings.TrimSpace(db.User)
	pass := strings.TrimSpace(db.Password)
	host := strings.TrimSpace(db.Host)
	name := strings.TrimSpace(db.Database)
	params := strings.TrimSpace(db.Params)

	if user == "" || host == "" || name == "" {
		return ""
	}
	if db.Port <= 0 {
		db.Port = 3306
	}

	auth := user
	if pass != "" {
		auth = auth + ":" + pass
	}

	addr := fmt.Sprintf("tcp(%s:%d)", host, db.Port)
	dsn := fmt.Sprintf("%s@%s/%s", auth, addr, name)
	if params != "" {
		dsn += "?" + params
	}
	return dsn
}

func defaultMySQLHost() string {
	if runningInContainer() {
		return "host.docker.internal"
	}
	return "127.0.0.1"
}

func defaultHealthAddr() string {
	if runningInContainer() {
		return "0.0.0.0:8801"
	}
	return "127.0.0.1:8801"
}

func fixHealthAddrForContainer(addr string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil || port == "" {
		return addr
	}
	if isLocalHost(host) {
		return net.JoinHostPort("0.0.0.0", port)
	}
	return addr
}

func isLocalHost(host string) bool {
	host = strings.TrimSpace(host)
	return host == "" || host == "127.0.0.1" || strings.EqualFold(host, "localhost")
}

func runningInContainer() bool {
	if raw := strings.TrimSpace(os.Getenv("RUNNING_IN_CONTAINER")); raw != "" {
		if v, err := strconv.ParseBool(raw); err == nil {
			return v
		}
	}

	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	// 尽力而为：通过 cgroup 特征判断（Linux/K8s）。
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		s := string(data)
		return strings.Contains(s, "docker") || strings.Contains(s, "kubepods") || strings.Contains(s, "containerd")
	}
	return false
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return def
}

func mustIntEnv(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return v
}

func mustInt64Env(key string, def int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return def
	}
	return v
}

func mustBoolEnv(key string, def bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return def
	}
	return v
}

func mustDurationEnv(key string, def time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		return def
	}
	return v
}

func parseInt64CSV(raw string) []int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			continue
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
