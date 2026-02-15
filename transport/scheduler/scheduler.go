package scheduler

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"emby-bot-new/internal/application/registration"
	"emby-bot-new/internal/config"

	"gopkg.in/telebot.v3"
)

type UserRepository interface {
	FindByTelegramID(ctx context.Context, telegramID int64) (*registration.Account, error)
	ListRegistered(ctx context.Context, limit, offset int) ([]registration.Account, error)
	ResetRegistration(ctx context.Context, telegramID int64) error
	UpdateLastPlayedAt(ctx context.Context, telegramID int64, at *time.Time) error
	SetPendingDays(ctx context.Context, telegramID int64, pendingDays int) error
	SetPendingExpiresAt(ctx context.Context, telegramID int64, expiresAt *time.Time) error

	// 审计事件（用于违规汇总/日报统计）。
	CreateAuditEvent(ctx context.Context, e registration.AuditEvent) error
	ListAuditEvents(ctx context.Context, from, to time.Time, categories []string) ([]registration.AuditEvent, error)
}

type InviteCodeRepository interface {
	ListExpiredReservationsByPrefix(ctx context.Context, prefix string, reservedBefore time.Time, limit int) ([]registration.InviteCodeReservation, error)
	ClearReservationIfStillUnused(ctx context.Context, code string, reservedByTelegramID int64) (bool, error)
}

type EmbyClient interface {
	DeleteUser(ctx context.Context, embyUserID string) error
	GetAllDevices(ctx context.Context) ([]map[string]any, error)
	GetSessions(ctx context.Context) ([]map[string]any, error)
	GetPlaybackHistory(ctx context.Context, userID string, limit int) ([]map[string]any, error)
	// GetActivityLogEntriesRaw 获取 Emby 活动日志（管理员接口），返回 Items 原始结构。
	// 用于更准确地同步“最后一次播放/活动时间”。
	GetActivityLogEntriesRaw(ctx context.Context, startIndex, limit int, minDate *time.Time) ([]map[string]any, error)
}

type Scheduler struct {
	// cfg/gov 为启动时的配置快照（来自环境变量）。
	// 动态开关（社区模式/注册设置等）来自数据库 settings。
	cfg config.SchedulerConfig
	gov config.GovernanceConfig

	// bot 用于向管理员/用户推送通知（最佳努力；失败不应阻塞主流程）。
	bot *telebot.Bot
	// repo/settings/emby 为任务运行期依赖的外部能力（DB/Emby）。
	repo                        UserRepository
	codes                       InviteCodeRepository
	inviteReservationTTL        time.Duration
	inviteReservationGCInterval time.Duration
	// settings 用于读取社区模式/不活跃时长等全局配置（来自数据库）。
	settings registration.SettingsRepository
	emby     EmbyClient

	// notifyIDs 为需要接收“后台任务告警/汇总”的 TGID（Owner + Admins 去重）。
	notifyIDs []int64

	// startOnce/stopOnce 确保 Start/Stop 幂等，避免重复启动任务或重复 cancel 导致竞态。
	startOnce sync.Once
	stopOnce  sync.Once
	cancel    context.CancelFunc
	baseCtx   context.Context
	wg        sync.WaitGroup

	// dyn 保存“可动态开关”的任务 cancel 函数（仅影响当前进程生命周期）。
	dynMu sync.Mutex
	dyn   map[string]context.CancelFunc

	// dailyDeletionReportMsgIDs 用于把“每日删号统计”固定到同一条消息上更新，避免每天刷屏。
	// key 为管理员 TGID，value 为消息 ID。
	dailyDeletionReportMu     sync.Mutex
	dailyDeletionReportMsgIDs map[int64]int
}

const (
	userInviteCodePrefix = "UI"
)

const (
	// 审计事件分类：用于汇总统计与通知展示（见 registration.AuditEvent）。
	auditCategoryWeb      = "web"
	auditCategoryExpired  = "expired"
	auditCategoryInactive = "inactive"
	auditCategoryManual   = "manual"
	auditCategoryOther    = "other"

	// 审计事件动作：warn 表示警告/提醒；revoke 表示执行删号/撤销资格等强制动作。
	auditActionWarn   = "warn"
	auditActionRevoke = "revoke"
)

// DetectionStats 用于记录一次检测任务的执行结果（用于管理员手动触发时展示）。
type DetectionStats struct {
	ScannedUsers int
	RevokedUsers int
}

func New(bot *telebot.Bot, repo UserRepository, settings registration.SettingsRepository, codes InviteCodeRepository, emby EmbyClient, cfg config.SchedulerConfig, gov config.GovernanceConfig, inviteCfg config.InviteConfig, ownerID int64, adminIDs []int64) *Scheduler {
	// 把 owner + admins 去重后作为通知接收人（避免同一人收到多次推送）。
	ids := make(map[int64]struct{}, len(adminIDs)+1)
	if ownerID != 0 {
		ids[ownerID] = struct{}{}
	}
	for _, id := range adminIDs {
		if id != 0 {
			ids[id] = struct{}{}
		}
	}
	notify := make([]int64, 0, len(ids))
	for id := range ids {
		notify = append(notify, id)
	}

	ttl := inviteCfg.ReservationTTL
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	gcInterval := inviteCfg.ReservationGCInterval
	if gcInterval <= 0 {
		gcInterval = 10 * time.Minute
	}

	return &Scheduler{
		cfg:                         cfg,
		gov:                         gov,
		bot:                         bot,
		repo:                        repo,
		codes:                       codes,
		inviteReservationTTL:        ttl,
		inviteReservationGCInterval: gcInterval,
		settings:                    settings,
		emby:                        emby,
		notifyIDs:                   notify,
		dyn:                         make(map[string]context.CancelFunc),
		dailyDeletionReportMsgIDs:   make(map[int64]int),
	}
}

// RunWebCheck 手动执行一次 Web 客户端检测。
func (s *Scheduler) RunWebCheck(ctx context.Context) DetectionStats {
	return s.runWebClientCheck(ctx)
}

// WebClientScheduleEnabled 表示 Web 检测的定时任务是否开启（运行时可切换）。
func (s *Scheduler) WebClientScheduleEnabled() bool {
	s.dynMu.Lock()
	defer s.dynMu.Unlock()
	_, ok := s.dyn["webclient"]
	return ok
}

// ExpiredScheduleEnabled 表示到期检测的定时任务是否开启（运行时可切换）。
func (s *Scheduler) ExpiredScheduleEnabled() bool {
	s.dynMu.Lock()
	defer s.dynMu.Unlock()
	_, ok := s.dyn["expired"]
	return ok
}

// InactiveScheduleEnabled 表示不活跃检测的定时任务是否开启（运行时可切换）。
func (s *Scheduler) InactiveScheduleEnabled() bool {
	s.dynMu.Lock()
	defer s.dynMu.Unlock()
	_, ok := s.dyn["inactive"]
	return ok
}

func (s *Scheduler) WebClientInterval() time.Duration {
	return s.cfg.WebClientInterval
}

func (s *Scheduler) ExpiredInterval() time.Duration {
	return s.cfg.ExpiredInterval
}

func (s *Scheduler) InactiveInterval() time.Duration {
	return s.cfg.InactiveInterval
}

// SetWebClientScheduleEnabled 用于运行时开启/关闭 Web 检测定时任务。
// 注意：仅影响当前进程生命周期；重启后仍以 .env 配置为准。
func (s *Scheduler) SetWebClientScheduleEnabled(enabled bool) bool {
	s.setDynamicJobEnabled("webclient", enabled, s.cfg.WebClientInterval, s.checkWebClient, false)
	return s.WebClientScheduleEnabled()
}

// SetExpiredScheduleEnabled 用于运行时开启/关闭“到期检测”定时任务。
// - 该任务通常不提供“单独执行”按钮，因此开启时会立即执行一次。
func (s *Scheduler) SetExpiredScheduleEnabled(enabled bool) bool {
	s.setDynamicJobEnabled("expired", enabled, s.cfg.ExpiredInterval, s.checkExpired, enabled)
	return s.ExpiredScheduleEnabled()
}

// SetInactiveScheduleEnabled 用于运行时开启/关闭“不活跃检测”定时任务。
// - 该任务通常不提供“单独执行”按钮，因此开启时会立即执行一次。
func (s *Scheduler) SetInactiveScheduleEnabled(enabled bool) bool {
	s.setDynamicJobEnabled("inactive", enabled, s.cfg.InactiveInterval, s.checkInactive, enabled)
	return s.InactiveScheduleEnabled()
}

func (s *Scheduler) Start(parent context.Context) {
	s.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(parent)
		s.cancel = cancel
		s.baseCtx = ctx

		// 先根据“社区模式”调整到期/不活跃检测的默认开关（若未设置模式则回退到 .env）。
		expiredEnabled := s.cfg.ExpiredEnabled
		inactiveEnabled := s.cfg.InactiveEnabled
		if s.settings != nil {
			if settings, err := s.settings.Get(ctx); err == nil {
				switch strings.TrimSpace(strings.ToLower(settings.ServiceMode)) {
				case "charity":
					// 公益：只清理“已过期且不活跃”的用户
					expiredEnabled = false
					inactiveEnabled = true
				case "public":
					// 公费：到期 + 不活跃都清理
					expiredEnabled = true
					inactiveEnabled = true
				case "private":
					// 私服：只清理到期用户
					expiredEnabled = true
					inactiveEnabled = false
				}
			}
		}

		if s.cfg.ExpiredInterval > 0 {
			s.setDynamicJobEnabled("expired", expiredEnabled, s.cfg.ExpiredInterval, s.checkExpired, true)
		}
		if s.cfg.InactiveInterval > 0 {
			s.setDynamicJobEnabled("inactive", inactiveEnabled, s.cfg.InactiveInterval, s.checkInactive, true)
		}

		if s.cfg.GroupEnabled && s.cfg.GroupInterval > 0 {
			s.startTicker(ctx, "group", s.cfg.GroupInterval, s.checkGroupMembers)
		}
		if s.cfg.WebClientEnabled && s.cfg.WebClientInterval > 0 {
			s.setDynamicJobEnabled("webclient", true, s.cfg.WebClientInterval, s.checkWebClient, true)
		}
		if s.cfg.PlaybackEnabled && s.cfg.PlaybackInterval > 0 {
			s.startTicker(ctx, "playback", s.cfg.PlaybackInterval, s.syncPlayback)
		}

		// 每小时违规汇总（仅当存在违规时才推送）。
		s.startTicker(ctx, "violations", time.Hour, s.pushViolationsHourly)
		// 每天 20:00 推送一次今日删号统计。
		s.startDailyDeletionReport(ctx)
		// 定向邀请的资格回收：12 小时内未注册则回收并返还给邀请者，避免“占坑”。
		if s.codes != nil {
			s.startTicker(ctx, "invite_reservation_gc", s.inviteReservationGCInterval, s.expireInviteReservations)
		}
	})
}

func (s *Scheduler) setDynamicJobEnabled(name string, enabled bool, interval time.Duration, job func(context.Context), immediate bool) {
	if interval <= 0 {
		return
	}
	if job == nil {
		return
	}

	s.dynMu.Lock()
	defer s.dynMu.Unlock()

	// 已存在且需要关闭
	if !enabled {
		if cancel, ok := s.dyn[name]; ok {
			cancel()
			delete(s.dyn, name)
		}
		return
	}

	// 已存在且需要开启（已开启）
	if _, ok := s.dyn[name]; ok {
		return
	}

	// Scheduler 尚未 Start（没有 baseCtx）时，无法开启动态任务。
	if s.baseCtx == nil {
		return
	}

	jobCtx, cancel := context.WithCancel(s.baseCtx)
	s.dyn[name] = cancel

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		run := func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("【任务】调度器异常：任务=%s 结果=panic 原因=%v", name, r)
				}
			}()

			timeout := s.cfg.JobTimeout
			if timeout <= 0 {
				timeout = 2 * time.Minute
			}
			runCtx, cancelRun := context.WithTimeout(jobCtx, timeout)
			defer cancelRun()
			job(runCtx)
		}

		// 启动时是否立即执行：保持与旧行为兼容（程序启动时会先跑一次），
		// 但通过“面板开关”开启时，建议使用“立即执行”按钮来跑一次。
		if immediate {
			run()
		}

		t := time.NewTicker(interval)
		defer t.Stop()

		for {
			select {
			case <-jobCtx.Done():
				return
			case <-t.C:
				run()
			}
		}
	}()
}

func (s *Scheduler) syncPlayback(ctx context.Context) {
	if s.repo == nil || s.emby == nil {
		return
	}
	// 优先用 Emby 活动日志（停止/暂停播放）来同步 last_played_at，时间更准确。
	// 注意：ActivityLog 是全局列表，这里一次拉取后按 EmbyUserID（UserId）归并，确保只针对数据库用户且不误匹配。
	activity, err := s.emby.GetActivityLogEntriesRaw(ctx, 0, 200, nil)
	if err == nil && len(activity) > 0 {
		lastByUserID := make(map[string]time.Time)
		for _, it := range activity {
			name := strings.TrimSpace(stringFromMap(it, "Name"))
			if name == "" {
				continue
			}
			// 需求：以“最后一次播放结束或暂停”为准，不使用“开始播放”时间。
			// 仅保留“停止/暂停播放”相关事件，减少误匹配。
			if !isPlaybackEndOrPauseEvent(name) {
				continue
			}
			uid := strings.TrimSpace(firstNonEmptyString(stringFromMap(it, "UserId"), stringFromMap(it, "UserID")))
			if uid == "" {
				continue
			}

			t := parseTimeRFC3339(stringFromMap(it, "Date"))
			if t == nil {
				continue
			}
			if prev, ok := lastByUserID[uid]; !ok || t.After(prev) {
				lastByUserID[uid] = *t
			}
		}

		_ = s.forEachRegistered(ctx, func(account registration.Account) {
			if account.TelegramID == 0 || strings.TrimSpace(account.EmbyUserID) == "" {
				return
			}
			tt, ok := lastByUserID[account.EmbyUserID]
			if !ok {
				return
			}
			t := tt
			_ = s.repo.UpdateLastPlayedAt(ctx, account.TelegramID, &t)
		})
		return
	}

	// 回退：如果活动日志不可用，则使用 PlayedItems/Items（LastPlayedDate）同步。
	_ = s.forEachRegistered(ctx, func(account registration.Account) {
		if account.TelegramID == 0 || strings.TrimSpace(account.EmbyUserID) == "" {
			return
		}
		items, err := s.emby.GetPlaybackHistory(ctx, account.EmbyUserID, 20)
		if err != nil || len(items) == 0 {
			return
		}

		var lastPlayedAt *time.Time
		for _, it := range items {
			ud, ok := it["UserData"].(map[string]any)
			if !ok {
				continue
			}
			t := parseTimeRFC3339(stringFromMap(ud, "LastPlayedDate"))
			if t == nil {
				continue
			}
			if lastPlayedAt == nil || t.After(*lastPlayedAt) {
				lastPlayedAt = t
			}
		}
		if lastPlayedAt == nil {
			return
		}
		_ = s.repo.UpdateLastPlayedAt(ctx, account.TelegramID, lastPlayedAt)
	})
}

func isPlaybackEndOrPauseEvent(name string) bool {
	n := strings.TrimSpace(name)
	if n == "" {
		return false
	}
	lower := strings.ToLower(n)
	// 英文关键字（Emby/插件可能返回英文事件）
	if strings.Contains(lower, "stopped") || strings.Contains(lower, "paused") || strings.Contains(lower, "playbackstopped") || strings.Contains(lower, "playbackpaused") {
		return true
	}
	// 中文关键字（常见：已停止播放 / 已暂停播放）
	if strings.Contains(n, "停止") || strings.Contains(n, "暂停") {
		// 防止误匹配“停止订阅/暂停同步”等非播放事件：要求同时出现“播放/Play/Playback”任意一个
		if strings.Contains(n, "播放") || strings.Contains(lower, "play") || strings.Contains(lower, "playback") {
			return true
		}
	}
	return false
}

func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() {
		s.dynMu.Lock()
		for name, cancel := range s.dyn {
			cancel()
			delete(s.dyn, name)
		}
		s.dynMu.Unlock()

		if s.cancel != nil {
			s.cancel()
		}
		s.wg.Wait()
	})
}

func (s *Scheduler) startTicker(ctx context.Context, name string, interval time.Duration, job func(context.Context)) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		run := func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("【任务】调度器异常：任务=%s 结果=panic 原因=%v", name, r)
				}
			}()

			timeout := s.cfg.JobTimeout
			if timeout <= 0 {
				timeout = 2 * time.Minute
			}
			jobCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			job(jobCtx)
		}

		run()

		t := time.NewTicker(interval)
		defer t.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				run()
			}
		}
	}()
}

func (s *Scheduler) RevokeAccount(ctx context.Context, telegramID int64, reason string) error {
	if telegramID == 0 {
		return fmt.Errorf("telegram id is empty")
	}
	if s.repo == nil {
		return fmt.Errorf("repo is nil")
	}
	account, err := s.repo.FindByTelegramID(ctx, telegramID)
	if err != nil {
		return err
	}
	if account == nil || account.EmbyUserID == "" {
		return nil
	}
	return s.revokeByAccount(ctx, *account, reason, true, true)
}

func (s *Scheduler) revokeByAccount(ctx context.Context, account registration.Account, reason string, notifyUser bool, notifyAdmins bool) error {
	if account.TelegramID == 0 {
		return fmt.Errorf("telegram id is empty")
	}

	if account.EmbyUserID != "" && s.emby != nil {
		if err := s.emby.DeleteUser(ctx, account.EmbyUserID); err != nil {
			log.Printf("【任务】删除账号失败：TGID=%d 结果=失败 原因=%v", account.TelegramID, err)
		}
	}
	if s.repo != nil {
		if err := s.repo.ResetRegistration(ctx, account.TelegramID); err != nil {
			return err
		}
	}

	now := time.Now()
	category := classifyAuditCategory(reason)
	s.recordAuditEvent(ctx, category, auditActionRevoke, account, reason, now)

	if notifyUser {
		userMsg := formatRevokeMessage(account, reason, now, false)
		s.notifyUser(account.TelegramID, userMsg)
	}
	if notifyAdmins {
		adminMsg := formatRevokeMessage(account, reason, now, true)
		s.notifyAdmins(adminMsg)
	}
	return nil
}

func (s *Scheduler) checkExpired(ctx context.Context) {
	if s.repo == nil {
		return
	}
	now := time.Now()
	revoked := make([]registration.Account, 0, 32)
	failed := 0
	_ = s.forEachRegistered(ctx, func(account registration.Account) {
		if account.IsWhitelist {
			return
		}
		if account.ExpiresAt == nil {
			return
		}
		if account.ExpiresAt.After(now) {
			return
		}
		if err := s.revokeByAccount(ctx, account, "到期", true, false); err != nil {
			failed++
			log.Printf("【任务】到期回收失败：TGID=%d 结果=失败 原因=%v", account.TelegramID, err)
			return
		}
		revoked = append(revoked, account)
	})

	if len(revoked) == 0 {
		return
	}
	s.notifyAdminsChunked(formatRevokeBatchSummary("到期自动删号汇总", now, revoked, failed))
}

func (s *Scheduler) checkInactive(ctx context.Context) {
	if s.repo == nil {
		return
	}
	// 不活跃时长以最后一次播放/活动记录为准（accounts.last_played_at），
	// 且仅对“存在有效期”的用户生效：到期后才开始计算不活跃时长。
	inactiveDuration := s.getInactiveDuration(ctx)
	now := time.Now()

	_ = s.forEachRegistered(ctx, func(account registration.Account) {
		if account.IsWhitelist {
			return
		}
		// 没有 Emby 用户/没有有效期（无限期） => 不参与不活跃检测
		if account.EmbyUserID == "" || account.ExpiresAt == nil {
			return
		}
		// 未到期不计算不活跃
		if account.ExpiresAt.After(now) {
			return
		}

		// 以“最后一次播放/活动时间”为起点（更符合用户侧看到的“活动状况”）。
		// 如果没有任何播放记录，则从“到期时间”开始计算（避免永远无法清理）。
		base := account.LastPlayedAt
		if base == nil {
			base = account.ExpiresAt
		}
		if base == nil {
			return
		}
		// 当 inactiveDuration=0 时，表示“到期即视为不活跃”，会立刻清理。
		if inactiveDuration > 0 && now.Sub(*base) < inactiveDuration {
			return
		}

		if err := s.revokeByAccount(ctx, account, "不活跃", true, true); err != nil {
			log.Printf("【任务】不活跃回收失败：TGID=%d 结果=失败 原因=%v", account.TelegramID, err)
		}
	})
}

func (s *Scheduler) checkGroupMembers(ctx context.Context) {
	if !s.gov.Enabled || (!s.gov.RequireGroup && !s.gov.RequireChannel) {
		return
	}
	if s.bot == nil || s.repo == nil {
		return
	}
	if s.gov.RequireGroup && len(s.gov.GroupIDs) == 0 {
		return
	}
	if s.gov.RequireChannel && s.channelRecipient() == nil {
		return
	}

	now := time.Now()
	_ = s.forEachRegistered(ctx, func(account registration.Account) {
		if account.ExpiresAt == nil {
			return
		}

		inGroup, inChannel := s.checkJoin(account.TelegramID)
		if inGroup && inChannel {
			return
		}

		shouldRevoke := s.gov.Strict || account.ExpiresAt.Before(now)
		if !shouldRevoke {
			return
		}

		reason := "未满足群组/频道要求"
		if !inGroup && inChannel {
			reason = "不在群组"
		} else if inGroup && !inChannel {
			reason = "未关注频道"
		}
		if err := s.revokeByAccount(ctx, account, reason, true, true); err != nil {
			log.Printf("【任务】入群校验回收失败：TGID=%d 结果=失败 原因=%v", account.TelegramID, err)
		}
	})
}

func (s *Scheduler) checkWebClient(ctx context.Context) {
	_ = s.runWebClientCheck(ctx)
}

func (s *Scheduler) runWebClientCheck(ctx context.Context) DetectionStats {
	if s.repo == nil || s.emby == nil {
		return DetectionStats{}
	}

	// 仅针对数据库中的“已注册用户”做检测：先把目标用户列表从 DB 拉出来。
	accounts := make([]registration.Account, 0, 256)
	_ = s.forEachRegistered(ctx, func(account registration.Account) {
		// 仅针对数据库中的已注册用户；同时避免无 EmbyUserID 的异常数据。
		if account.EmbyUserID == "" {
			return
		}
		accounts = append(accounts, account)
	})
	registered := make(map[string]struct{}, len(accounts))
	for _, a := range accounts {
		registered[a.EmbyUserID] = struct{}{}
	}

	allDevices, err := s.emby.GetAllDevices(ctx)
	if err != nil {
		log.Printf("【任务】获取设备列表失败：结果=失败 原因=%v", err)
		return DetectionStats{}
	}
	devicesByUser := make(map[string][]map[string]any)
	for _, d := range allDevices {
		if d == nil {
			continue
		}
		uid := strings.TrimSpace(firstNonEmptyString(
			stringFromMap(d, "LastUserId"),
			stringFromMap(d, "UserId"),
			stringFromMap(d, "LastUserID"),
			stringFromMap(d, "UserID"),
		))
		if uid == "" {
			continue
		}
		if _, ok := registered[uid]; !ok {
			// 只检查数据库中的用户
			continue
		}
		devicesByUser[uid] = append(devicesByUser[uid], d)
	}

	sessionsByUser := make(map[string][]map[string]any)
	if s.bot != nil {
		if sessions, err := s.emby.GetSessions(ctx); err == nil {
			for _, sess := range sessions {
				if sess == nil {
					continue
				}
				uid := strings.TrimSpace(firstNonEmptyString(
					stringFromMap(sess, "UserId"),
					stringFromMap(sess, "UserID"),
				))
				if uid == "" {
					continue
				}
				if _, ok := registered[uid]; !ok {
					// 只检查数据库中的用户
					continue
				}
				sessionsByUser[uid] = append(sessionsByUser[uid], sess)
			}
		}
	}

	stats := DetectionStats{}
	for _, account := range accounts {
		stats.ScannedUsers++
		if !detectWebClient(devicesByUser[account.EmbyUserID], sessionsByUser[account.EmbyUserID]) {
			continue
		}
		// 需求：检测到 Emby Web/Web 直接判定违规并封号，并私信告知原因。
		stats.RevokedUsers++
		_ = s.revokeByAccount(ctx, account, "违规：使用 Emby Web/Web 客户端", true, true)
	}
	return stats
}

func detectWebClient(devices []map[string]any, sessions []map[string]any) bool {
	for _, d := range devices {
		name := strings.ToLower(strings.TrimSpace(stringFromMap(d, "Name")))
		app := strings.ToLower(strings.TrimSpace(stringFromMap(d, "AppName")))
		if app == "emby web" || strings.Contains(app, "emby web") {
			return true
		}
		if strings.Contains(app, "web") {
			return true
		}
		if strings.Contains(name, "chrome") || strings.Contains(name, "firefox") || strings.Contains(name, "safari") || strings.Contains(name, "edge") || strings.Contains(name, "opera") {
			return true
		}
	}

	for _, sess := range sessions {
		client := strings.ToLower(strings.TrimSpace(firstNonEmptyString(
			stringFromMap(sess, "Client"),
			stringFromMap(sess, "ClientName"),
			stringFromMap(sess, "Product"),
		)))
		device := strings.ToLower(strings.TrimSpace(stringFromMap(sess, "DeviceName")))
		if strings.Contains(client, "web") {
			return true
		}
		if strings.Contains(device, "chrome") || strings.Contains(device, "firefox") || strings.Contains(device, "safari") || strings.Contains(device, "edge") {
			return true
		}
	}
	return false
}

func (s *Scheduler) forEachRegistered(ctx context.Context, fn func(registration.Account)) error {
	const limit = 200
	for offset := 0; ; offset += limit {
		users, err := s.repo.ListRegistered(ctx, limit, offset)
		if err != nil {
			return err
		}
		if len(users) == 0 {
			return nil
		}
		for _, u := range users {
			fn(u)
		}
		if len(users) < limit {
			return nil
		}
	}
}

func (s *Scheduler) getInactiveDuration(ctx context.Context) time.Duration {
	raw := ""
	if s.settings != nil {
		if settings, err := s.settings.Get(ctx); err == nil {
			raw = strings.TrimSpace(settings.InactiveDuration)
		}
	}
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("COMM_INACTIVE_DURATION"))
	}
	if raw == "" {
		raw = "720h"
	}

	d, err := parseFlexibleDuration(raw)
	if err != nil {
		log.Printf("【任务】不活跃时长配置无效：值=%q 原因=%v", raw, err)
		return 0
	}
	return d
}

func parseFlexibleDuration(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	// 支持 30d 这种“天”单位（Go 原生不支持 d）
	if strings.HasSuffix(strings.ToLower(raw), "d") {
		v := strings.TrimSpace(strings.TrimSuffix(strings.ToLower(raw), "d"))
		days, err := strconv.Atoi(v)
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(raw)
}

func (s *Scheduler) notifyUser(telegramID int64, msg string) {
	if s.bot == nil || telegramID == 0 {
		return
	}
	_, _ = s.bot.Send(&telebot.User{ID: telegramID}, msg)
}

func (s *Scheduler) notifyAdmins(msg string) {
	if s.bot == nil || msg == "" {
		return
	}
	for _, id := range s.notifyIDs {
		if id == 0 {
			continue
		}
		_, _ = s.bot.Send(&telebot.User{ID: id}, msg)
	}
}

func (s *Scheduler) notifyAdminsEditDailyDeletionReport(msg string) {
	if s == nil || s.bot == nil || strings.TrimSpace(msg) == "" {
		return
	}

	for _, id := range s.notifyIDs {
		if id == 0 {
			continue
		}

		// 若已有“日报锚点消息”，优先编辑更新；失败则退回到发送新消息。
		var messageID int
		s.dailyDeletionReportMu.Lock()
		messageID = s.dailyDeletionReportMsgIDs[id]
		s.dailyDeletionReportMu.Unlock()

		if messageID > 0 {
			m := &telebot.Message{ID: messageID, Chat: &telebot.Chat{ID: id}}
			if _, err := s.bot.Edit(m, msg); err == nil {
				continue
			}
		}

		sent, err := s.bot.Send(&telebot.User{ID: id}, msg)
		if err != nil || sent == nil {
			continue
		}
		s.dailyDeletionReportMu.Lock()
		s.dailyDeletionReportMsgIDs[id] = sent.ID
		s.dailyDeletionReportMu.Unlock()
	}
}

func (s *Scheduler) notifyAdminsChunked(msg string) {
	if msg == "" {
		return
	}
	const maxRunes = 3500
	for _, part := range splitTextByLines(msg, maxRunes) {
		s.notifyAdmins(part)
	}
}

func (s *Scheduler) expireInviteReservations(ctx context.Context) {
	if s == nil || s.codes == nil || s.bot == nil || s.repo == nil {
		return
	}

	cutoff := time.Now().Add(-s.inviteReservationTTL)
	const batchSize = 200
	const maxBatches = 10

	for batch := 0; batch < maxBatches; batch++ {
		rows, err := s.codes.ListExpiredReservationsByPrefix(ctx, userInviteCodePrefix, cutoff, batchSize)
		if err != nil || len(rows) == 0 {
			return
		}

		clearedAny := false
		for _, it := range rows {
			if strings.TrimSpace(it.Code) == "" || it.CreatorTelegramID == 0 || it.ReservedByTelegramID == 0 || it.ReservedAt.IsZero() {
				continue
			}

			cleared, err := s.codes.ClearReservationIfStillUnused(ctx, it.Code, it.ReservedByTelegramID)
			if err != nil || !cleared {
				continue
			}
			clearedAny = true

			// 若对方已兑换但未注册，会存在 pending_days；这里一并清空，避免“过期后仍可注册”。
			_ = s.repo.SetPendingDays(ctx, it.ReservedByTelegramID, 0)
			_ = s.repo.SetPendingExpiresAt(ctx, it.ReservedByTelegramID, nil)

			botUsername := ""
			if s.bot.Me != nil {
				botUsername = strings.TrimSpace(s.bot.Me.Username)
			}
			link := ""
			if botUsername != "" {
				link = fmt.Sprintf("https://t.me/%s?start=%s", botUsername, strings.TrimSpace(it.Code))
			}

			expiredAt := it.ReservedAt.Add(s.inviteReservationTTL).Local().Format("2006-01-02 15:04:05")
			msg := strings.Join([]string{
				"⏳ 邀请资格已回收并返还",
				"",
				fmt.Sprintf("目标 TGID：`%d`", it.ReservedByTelegramID),
				fmt.Sprintf("邀请码：`%s`", it.Code),
				fmt.Sprintf("过期时间：`%s`（12 小时未注册）", expiredAt),
				"",
				"你可以再次邀请对方：",
				"- 发送：`/Harem " + fmt.Sprint(it.ReservedByTelegramID) + "`（会再次为对方预留资格）",
				"- 或把这个链接/命令转发给对方：",
				link,
				"`/start " + it.Code + "`",
			}, "\n")
			msg += "\n\n下一次邀请：现在（本次未注册，不进入冷却）"

			_, _ = s.bot.Send(&telebot.User{ID: it.CreatorTelegramID}, msg, telebot.ModeMarkdown)
			log.Printf("【邀请】资格回收：创建者=%d 目标=%d code=%s", it.CreatorTelegramID, it.ReservedByTelegramID, it.Code)
		}

		if !clearedAny {
			return
		}
	}
}

func splitTextByLines(msg string, maxRunes int) []string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return nil
	}
	if maxRunes <= 0 || utf8.RuneCountInString(msg) <= maxRunes {
		return []string{msg}
	}

	lines := strings.Split(msg, "\n")
	parts := make([]string, 0, 4)

	cur := ""
	curRunes := 0
	flush := func() {
		if strings.TrimSpace(cur) == "" {
			cur = ""
			curRunes = 0
			return
		}
		parts = append(parts, strings.TrimSpace(cur))
		cur = ""
		curRunes = 0
	}

	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		for utf8.RuneCountInString(line) > maxRunes {
			flush()
			r := []rune(line)
			parts = append(parts, strings.TrimSpace(string(r[:maxRunes])))
			line = strings.TrimSpace(string(r[maxRunes:]))
		}
		lineRunes := utf8.RuneCountInString(line)
		need := lineRunes
		if cur != "" {
			need++ // 额外预留一个换行符
		}
		if cur != "" && curRunes+need > maxRunes {
			flush()
		}
		if cur == "" {
			cur = line
			curRunes = lineRunes
			continue
		}
		cur += "\n" + line
		curRunes += 1 + lineRunes
	}
	flush()
	return parts
}

func formatRevokeBatchSummary(title string, at time.Time, accounts []registration.Account, failed int) string {
	if len(accounts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("【任务】")
	b.WriteString(strings.TrimSpace(title))
	b.WriteString("\n")
	b.WriteString("时间：")
	b.WriteString(at.Format("2006-01-02 15:04:05"))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("本次删除：%d", len(accounts)))
	if failed > 0 {
		b.WriteString(fmt.Sprintf("（失败：%d）", failed))
	}
	b.WriteString("\n")
	b.WriteString("列表：\n")

	for i, a := range accounts {
		embyName := strings.TrimSpace(a.EmbyUsername)
		if embyName == "" {
			embyName = "-"
		}
		tgName := strings.TrimSpace(a.TelegramUsername)
		if tgName == "" {
			tgName = "-"
		}
		b.WriteString(fmt.Sprintf("%d) TGID=%d | TG=%s | Emby=%s\n", i+1, a.TelegramID, tgName, embyName))
	}
	return strings.TrimSpace(b.String())
}

func (s *Scheduler) recordAuditEvent(ctx context.Context, category, action string, account registration.Account, reason string, at time.Time) {
	if s == nil || s.repo == nil {
		return
	}
	_ = s.repo.CreateAuditEvent(ctx, registration.AuditEvent{
		Category:     strings.TrimSpace(category),
		Action:       strings.TrimSpace(action),
		TelegramID:   account.TelegramID,
		EmbyUsername: strings.TrimSpace(account.EmbyUsername),
		Reason:       strings.TrimSpace(reason),
		CreatedAt:    at,
	})
}

func classifyAuditCategory(reason string) string {
	r := strings.ToLower(strings.TrimSpace(reason))
	switch {
	case strings.Contains(r, "web"):
		return auditCategoryWeb
	case strings.Contains(reason, "到期") || strings.Contains(reason, "过期"):
		return auditCategoryExpired
	case strings.Contains(reason, "不活跃"):
		return auditCategoryInactive
	case strings.Contains(r, "remove") || strings.Contains(reason, "移除") || strings.Contains(reason, "手动"):
		return auditCategoryManual
	default:
		return auditCategoryOther
	}
}

func formatRevokeMessage(account registration.Account, reason string, at time.Time, forAdmin bool) string {
	var b strings.Builder
	if forAdmin {
		b.WriteString("🚫 违规/删号通知（同步）\n")
	} else {
		b.WriteString("🚫 账号已被回收\n")
	}
	b.WriteString("——————————————\n")
	b.WriteString(fmt.Sprintf("👤 TGID：%d\n", account.TelegramID))
	if strings.TrimSpace(account.EmbyUsername) != "" {
		b.WriteString(fmt.Sprintf("🎬 Emby 用户名：%s\n", account.EmbyUsername))
	} else {
		b.WriteString("🎬 Emby 用户名：-\n")
	}
	b.WriteString(fmt.Sprintf("🕒 删号时间：%s\n", at.Local().Format("2006-01-02 15:04:05")))
	b.WriteString(fmt.Sprintf("📌 删号原因：%s\n", strings.TrimSpace(reason)))
	return b.String()
}

func (s *Scheduler) pushViolationsHourly(ctx context.Context) {
	if s == nil || s.repo == nil || s.bot == nil {
		return
	}
	now := time.Now()
	from := now.Add(-1 * time.Hour)
	events, err := s.repo.ListAuditEvents(ctx, from, now, []string{auditCategoryWeb})
	if err != nil {
		return
	}
	if len(events) == 0 {
		return
	}

	// 统计 + 去重（同一用户保留最新事件）。
	webWarn, webRevoke := 0, 0
	latest := make(map[int64]registration.AuditEvent)
	for _, e := range events {
		if e.TelegramID == 0 {
			continue
		}
		switch e.Category {
		case auditCategoryWeb:
			if e.Action == auditActionRevoke {
				webRevoke++
			} else if e.Action == auditActionWarn {
				webWarn++
			}
		}
		cur, ok := latest[e.TelegramID]
		if !ok || e.CreatedAt.After(cur.CreatedAt) {
			latest[e.TelegramID] = e
		}
	}
	if webWarn+webRevoke == 0 {
		return
	}

	// 排序输出（按时间倒序）。
	items := make([]registration.AuditEvent, 0, len(latest))
	for _, e := range latest {
		items = append(items, e)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })

	maxLen := 0
	for _, e := range items {
		l := len(strconv.FormatInt(e.TelegramID, 10))
		if l > maxLen {
			maxLen = l
		}
	}
	if maxLen < 1 {
		maxLen = 1
	}

	var b strings.Builder
	b.WriteString("🚨 近 1 小时违规汇总\n")
	b.WriteString("——————————————\n")
	b.WriteString(fmt.Sprintf("时间范围：%s - %s\n", from.Local().Format("15:04"), now.Local().Format("15:04")))
	b.WriteString(fmt.Sprintf("🌐 Web：警告 %d，封号 %d\n", webWarn, webRevoke))
	b.WriteString("——————————————\n")
	b.WriteString("明细（每人仅展示最新一次）：\n")
	for _, e := range items {
		action := "未知"
		if e.Action == auditActionWarn {
			action = "警告"
		} else if e.Action == auditActionRevoke {
			action = "封号"
		}
		emby := strings.TrimSpace(e.EmbyUsername)
		if emby == "" {
			emby = "-"
		}
		b.WriteString(fmt.Sprintf("TGID：%*d丨Emby用户名：%s丨处理：%s丨时间：%s\n", maxLen, e.TelegramID, emby, action, e.CreatedAt.Local().Format("15:04")))
		if strings.TrimSpace(e.Reason) != "" {
			b.WriteString("原因：" + strings.TrimSpace(e.Reason) + "\n")
		}
	}
	s.notifyAdmins(b.String())
}

func (s *Scheduler) startDailyDeletionReport(ctx context.Context) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			now := time.Now()
			next := time.Date(now.Year(), now.Month(), now.Day(), 20, 0, 0, 0, now.Location())
			if !now.Before(next) {
				next = next.Add(24 * time.Hour)
			}
			timer := time.NewTimer(time.Until(next))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				s.pushDailyDeletionReport(ctx)
			}
		}
	}()
}

func (s *Scheduler) pushDailyDeletionReport(ctx context.Context) {
	if s == nil || s.repo == nil || s.bot == nil {
		return
	}
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	events, err := s.repo.ListAuditEvents(ctx, start, now, nil)
	if err != nil {
		return
	}

	web, inactive, expired, manual, other := 0, 0, 0, 0, 0
	for _, e := range events {
		if e.Action != auditActionRevoke {
			continue
		}
		switch e.Category {
		case auditCategoryWeb:
			web++
		case auditCategoryInactive:
			inactive++
		case auditCategoryExpired:
			expired++
		case auditCategoryManual:
			manual++
		default:
			other++
		}
	}
	total := web + inactive + expired + manual + other

	var b strings.Builder
	b.WriteString(fmt.Sprintf("📊 今日删号统计（%s）\n", now.Local().Format("2006-01-02")))
	b.WriteString("——————————————\n")
	b.WriteString(fmt.Sprintf("🚫 Web 违规：%d 人\n", web))
	b.WriteString(fmt.Sprintf("🕒 不活跃删号：%d 人\n", inactive))
	b.WriteString(fmt.Sprintf("⌛ 过期删号：%d 人\n", expired))
	if manual > 0 {
		b.WriteString(fmt.Sprintf("🗑 手动删除：%d 人\n", manual))
	}
	if other > 0 {
		b.WriteString(fmt.Sprintf("📎 其它原因：%d 人\n", other))
	}
	b.WriteString("——————————————\n")
	b.WriteString(fmt.Sprintf("合计：%d 人\n", total))
	s.notifyAdminsEditDailyDeletionReport(b.String())
}

func (s *Scheduler) checkJoin(telegramID int64) (inGroup bool, inChannel bool) {
	inGroup = true
	inChannel = true

	if s.bot == nil || telegramID == 0 {
		return inGroup, inChannel
	}

	if s.gov.RequireGroup && len(s.gov.GroupIDs) > 0 {
		inGroup = s.isUserInAnyGroup(telegramID)
	}
	if s.gov.RequireChannel && s.channelRecipient() != nil {
		inChannel = s.isUserInChannel(telegramID)
	}
	return inGroup, inChannel
}

func (s *Scheduler) isUserInAnyGroup(telegramID int64) bool {
	if s.bot == nil || telegramID == 0 {
		return true
	}
	if len(s.gov.GroupIDs) == 0 {
		return true
	}

	anySuccess := false
	for _, gid := range s.gov.GroupIDs {
		member, err := s.bot.ChatMemberOf(&telebot.Chat{ID: gid}, &telebot.User{ID: telegramID})
		if err != nil {
			continue
		}
		anySuccess = true
		switch member.Role {
		case "creator", "administrator", "member", "restricted":
			return true
		}
	}
	if !anySuccess {
		return true
	}
	return false
}

func (s *Scheduler) isUserInChannel(telegramID int64) bool {
	if s.bot == nil || telegramID == 0 {
		return true
	}
	chat := s.channelRecipient()
	if chat == nil {
		return true
	}

	member, err := s.bot.ChatMemberOf(chat, &telebot.User{ID: telegramID})
	if err != nil {
		return true
	}
	switch member.Role {
	case "creator", "administrator", "member":
		return true
	default:
		return false
	}
}

type chatRecipient string

func (c chatRecipient) Recipient() string { return string(c) }

func (s *Scheduler) channelRecipient() telebot.Recipient {
	if s.gov.ChannelID != 0 {
		return &telebot.Chat{ID: s.gov.ChannelID}
	}
	u := strings.TrimSpace(s.gov.ChannelUsername)
	u = strings.TrimPrefix(u, "@")
	if u != "" {
		return chatRecipient("@" + u)
	}
	return nil
}

func stringFromMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func parseTimeRFC3339(v string) *time.Time {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil
	}
	return &t
}
