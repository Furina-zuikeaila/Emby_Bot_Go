package router

import (
	"context"
	"strings"
	"sync"
	"time"

	accountapp "emby-bot-new/internal/application/account"
	adminapp "emby-bot-new/internal/application/admin"
	"emby-bot-new/internal/application/invite"
	"emby-bot-new/internal/application/registration"
	"emby-bot-new/internal/config"
	"emby-bot-new/internal/transport/scheduler"

	"gopkg.in/telebot.v3"
)

type AccountRevoker interface {
	RevokeAccount(ctx context.Context, telegramID int64, reason string) error
}

type detectionRunner interface {
	RunWebCheck(ctx context.Context) scheduler.DetectionStats
}

type detectionScheduler interface {
	detectionRunner

	// 定时任务开关（运行时可切换）
	WebClientScheduleEnabled() bool
	ExpiredScheduleEnabled() bool
	InactiveScheduleEnabled() bool
	SetWebClientScheduleEnabled(enabled bool) bool
	SetExpiredScheduleEnabled(enabled bool) bool
	SetInactiveScheduleEnabled(enabled bool) bool

	// 当前定时任务间隔（来自配置）
	WebClientInterval() time.Duration
	ExpiredInterval() time.Duration
	InactiveInterval() time.Duration
}

type Router struct {
	reg      registration.Service
	regAdmin registration.AdminService
	adm      adminapp.Service
	acct     accountapp.Service

	menus *Menus
	state *convoStore
	ui    *uiStore

	ownerID int64
	admins  map[int64]struct{}

	gov           config.GovernanceConfig
	revoker       AccountRevoker
	embyPublicURL string

	crowdfund     config.CrowdfundConfig
	crowdfundRepo crowdfundReceiptRepo
	tronVerifier  trc20Verifier

	invite invite.Service

	inviteMinAccountAgeDays int
	inviteCooldownDays      int
	inviteReservationTTL    time.Duration

	leavingMu     sync.Mutex
	leavingGroups map[int64]struct{}

	groupAdminMu      sync.Mutex
	groupAdminCache   map[int64]struct{}
	groupAdminUpdated time.Time

	guardMu                   sync.Mutex
	guardGroupCommandsEnabled bool
}

type Options struct {
	OwnerID       int64
	AdminIDs      []int64
	Govern        config.GovernanceConfig
	Revoker       AccountRevoker
	EmbyPublicURL string

	Crowdfund     config.CrowdfundConfig
	CrowdfundRepo crowdfundReceiptRepo
	TronVerifier  trc20Verifier

	Invite invite.Service

	InviteMinAccountAgeDays int
	InviteCooldownDays      int
	InviteReservationTTL    time.Duration
}

func NewRouter(reg registration.Service, regAdmin registration.AdminService, adminSvc adminapp.Service, accountSvc accountapp.Service, opts Options) *Router {
	admins := make(map[int64]struct{}, len(opts.AdminIDs)+1)
	if opts.OwnerID != 0 {
		admins[opts.OwnerID] = struct{}{}
	}
	for _, id := range opts.AdminIDs {
		if id != 0 {
			admins[id] = struct{}{}
		}
	}

	inviteTTL := opts.InviteReservationTTL
	if inviteTTL <= 0 {
		inviteTTL = 12 * time.Hour
	}

	return &Router{
		reg:      reg,
		regAdmin: regAdmin,
		adm:      adminSvc,
		acct:     accountSvc,
		menus:    NewMenus(),
		// 会话状态用于“尽量编辑原消息”的导航与输入流程；这里适当延长，避免管理员点击深链跳转时 state 过期。
		state:                     newConvoStore(30 * time.Minute),
		ui:                        newUIStore(24 * time.Hour),
		ownerID:                   opts.OwnerID,
		admins:                    admins,
		gov:                       opts.Govern,
		revoker:                   opts.Revoker,
		embyPublicURL:             strings.TrimSpace(opts.EmbyPublicURL),
		crowdfund:                 opts.Crowdfund,
		crowdfundRepo:             opts.CrowdfundRepo,
		tronVerifier:              opts.TronVerifier,
		invite:                    opts.Invite,
		inviteMinAccountAgeDays:   opts.InviteMinAccountAgeDays,
		inviteCooldownDays:        opts.InviteCooldownDays,
		inviteReservationTTL:      inviteTTL,
		leavingGroups:             make(map[int64]struct{}),
		groupAdminCache:           make(map[int64]struct{}),
		guardGroupCommandsEnabled: opts.Govern.GuardGroupCommands,
	}
}

func (r *Router) Register(bot *telebot.Bot) {
	if r.shouldEnforceJoin() {
		bot.Use(r.requireJoinMiddleware)
	}
	bot.Use(r.groupCommandEphemeralMiddleware)

	bot.Handle("/start", r.handleStart)
	bot.Handle(&telebot.Btn{Unique: CbRegister}, r.handleRegister)
	bot.Handle(&telebot.Btn{Unique: CbInviteCode}, r.handleInviteCode)
	bot.Handle(&telebot.Btn{Unique: CbBind}, r.handleBind)
	bot.Handle(&telebot.Btn{Unique: CbMe}, r.handleMe)
	bot.Handle(&telebot.Btn{Unique: CbUserPanel}, r.handleUserPanel)
	bot.Handle(&telebot.Btn{Unique: CbServerInfo}, r.handleServerInfo)
	bot.Handle(&telebot.Btn{Unique: CbCrowdfund}, r.handleCrowdfund)
	bot.Handle(&telebot.Btn{Unique: CbUserInvite}, r.handleUserInvite)
	bot.Handle(&telebot.Btn{Unique: CbEmbyLibs}, r.handleEmbyLibs)
	bot.Handle(&telebot.Btn{Unique: CbToggleLib}, r.handleToggleLib)
	bot.Handle(&telebot.Btn{Unique: CbMyHistory}, r.handleMyHistory)
	bot.Handle(&telebot.Btn{Unique: CbMyHarem}, r.handleMyHarem)
	bot.Handle(&telebot.Btn{Unique: CbHaremRevokeIn}, r.handleHaremRevokeInputStart)
	bot.Handle(&telebot.Btn{Unique: CbHaremRevoke}, r.handleHaremRevokePreview)
	bot.Handle(&telebot.Btn{Unique: CbHaremConfirm}, r.handleHaremRevokeConfirm)
	bot.Handle(&telebot.Btn{Unique: CbBackMain}, r.handleBackMain)
	bot.Handle(&telebot.Btn{Unique: CbRenew}, r.handleRenewCode)
	bot.Handle(&telebot.Btn{Unique: CbResetPassword}, r.handleResetPassword)
	bot.Handle(&telebot.Btn{Unique: CbDeleteAccount}, r.handleDeleteAccount)
	bot.Handle(&telebot.Btn{Unique: CbCancel}, r.handleCancel)
	bot.Handle(&telebot.Btn{Unique: CbRecheckJoin}, r.handleRecheckJoin)
	bot.Handle(telebot.OnText, r.handleText)
	bot.Handle(telebot.OnMedia, r.handleMedia)
	bot.Handle(telebot.OnAddedToGroup, r.handleAddedToGroup)
	bot.Handle(telebot.OnUserLeft, r.handleUserLeftGroup)

	// admin
	bot.Handle("/admin", r.handleAdmin)
	bot.Handle("/users", r.handleAdminUsersCmd)
	bot.Handle("/user", r.handleAdminUserCmd)
	bot.Handle("/User", r.handleAdminUserCmd)
	bot.Handle("/ucr", r.handleAdminCreateCmd)
	bot.Handle("/urm", r.handleAdminDeleteCmd)
	bot.Handle("/resetpass", r.handleAdminResetPassCmd)
	bot.Handle("/renew", r.handleAdminRenewCmd)
	bot.Handle("/renewall", r.handleAdminRenewAllCmd)
	// 兼容更直观的管理员指令（支持在群内回复触发）
	bot.Handle("/Whitelist", r.handleWhitelistCmd)
	bot.Handle("/whitelist", r.handleWhitelistCmd)
	bot.Handle("/UnWhitelist", r.handleUnWhitelistCmd)
	bot.Handle("/unwhitelist", r.handleUnWhitelistCmd)
	bot.Handle("/License", r.handleLicenseCmd)
	bot.Handle("/license", r.handleLicenseCmd)
	bot.Handle("/Remove", r.handleRemoveCmd)
	bot.Handle("/remove", r.handleRemoveCmd)
	bot.Handle("/Harem", r.handleHaremCmd)
	bot.Handle("/harem", r.handleHaremCmd)
	bot.Handle("/CutTree", r.handleCutTreeCmd)
	bot.Handle("/cuttree", r.handleCutTreeCmd)
	bot.Handle("/Eliminate", r.handleEliminateCmd)
	bot.Handle("/eliminate", r.handleEliminateCmd)
	bot.Handle("/Disband", r.handleDisbandCmd)
	bot.Handle("/disband", r.handleDisbandCmd)
	bot.Handle("/Member", r.handleMemberCmd)
	bot.Handle("/member", r.handleMemberCmd)
	bot.Handle("/UnMember", r.handleUnMemberCmd)
	bot.Handle("/unmember", r.handleUnMemberCmd)

	bot.Handle(&telebot.Btn{Unique: CbAdminPanel}, r.handleAdmin)
	bot.Handle(&telebot.Btn{Unique: CbAdminUsers}, r.handleAdminUsersCb)
	bot.Handle(&telebot.Btn{Unique: CbAdminHelp}, r.handleAdminHelpCb)
	bot.Handle(&telebot.Btn{Unique: CbAdminReg}, r.handleAdminRegPanel)
	bot.Handle(&telebot.Btn{Unique: CbAdminKeys}, r.handleAdminKeysPanel)
	bot.Handle(&telebot.Btn{Unique: CbAdminCommunityMode}, r.handleAdminCommunityModePanel)
	bot.Handle(&telebot.Btn{Unique: CbAdminSetInactiveDuration}, r.handleAdminSetInactiveDuration)
	bot.Handle(&telebot.Btn{Unique: CbAdminDetectionTasks}, r.handleAdminDetectionTasksPanel)
	bot.Handle(&telebot.Btn{Unique: CbAdminWhitelist}, r.handleAdminWhitelistPanel)
	bot.Handle(&telebot.Btn{Unique: CbWhitelistAdd}, r.handleAdminWhitelistAdd)
	bot.Handle(&telebot.Btn{Unique: CbWhitelistRemove}, r.handleAdminWhitelistRemove)
	bot.Handle(&telebot.Btn{Unique: CbToggleWebSchedule}, r.handleToggleWebSchedule)
	bot.Handle(&telebot.Btn{Unique: CbToggleExpiredSchedule}, r.handleToggleExpiredSchedule)
	bot.Handle(&telebot.Btn{Unique: CbToggleInactiveSchedule}, r.handleToggleInactiveSchedule)
	bot.Handle(&telebot.Btn{Unique: CbToggleGroupCommandGuard}, r.handleToggleGroupCommandGuard)
	bot.Handle(&telebot.Btn{Unique: CbRunWebCheck}, r.handleRunWebCheck)
	bot.Handle(&telebot.Btn{Unique: CbRunAllChecks}, r.handleRunAllChecks)

	bot.Handle(&telebot.Btn{Unique: CbRegToggle}, r.handleRegToggle)
	bot.Handle(&telebot.Btn{Unique: CbRegSetTiming}, r.handleRegSetTiming)
	bot.Handle(&telebot.Btn{Unique: CbRegSetMaxUsers}, r.handleRegSetMaxUsers)
	bot.Handle(&telebot.Btn{Unique: CbRegSetDefaultDays}, r.handleRegSetDefaultDays)
	bot.Handle(&telebot.Btn{Unique: CbRegCreateCodes}, r.handleRegCreateCodes)
	bot.Handle(&telebot.Btn{Unique: CbRegCreateRenewCodes}, r.handleRegCreateRenewCodes)
	bot.Handle(&telebot.Btn{Unique: CbRegExport}, r.handleRegExport)
	bot.Handle(&telebot.Btn{Unique: CbRegWipe}, r.handleRegWipe)
	bot.Handle(&telebot.Btn{Unique: CbRegStats}, r.handleRegStats)
	bot.Handle(&telebot.Btn{Unique: CbRegGrant}, r.handleRegGrant)
	bot.Handle(&telebot.Btn{Unique: CbReqCommunityMode}, r.handleReqCommunityMode)
	bot.Handle(&telebot.Btn{Unique: CbSetCommunityMode}, r.handleSetCommunityMode)
}
