package invite

import (
	"context"
	"fmt"
	"strings"
	"time"

	"emby-bot-new/internal/application/registration"
)

type Service interface {
	PrepareUserInviteCode(ctx context.Context, inviterTelegramID int64) (InviteCodeResult, error)
	// PrepareTargetedInviteCode 为指定 TGID 生成“定向邀请”：
	// - 邀请码会在数据库中预留给 inviteeTelegramID（对方私聊 /start 后无需手动拿到邀请码也能注册）；
	// - 若已存在同一邀请者->被邀请者的未使用邀请，则复用。
	PrepareTargetedInviteCode(ctx context.Context, inviterTelegramID int64, inviteeTelegramID int64) (InviteCodeResult, error)
	Descendants(ctx context.Context, rootInviterTelegramID int64) ([]int64, error)

	// ListUserInvites 列出“我邀请成功过的人”（直接邀请关系）。
	ListUserInvites(ctx context.Context, inviterTelegramID int64) ([]DirectInvite, error)
	// GetUserInvite 查询某个被邀请者是否由当前用户邀请成功。
	GetUserInvite(ctx context.Context, inviterTelegramID int64, inviteeTelegramID int64) (*DirectInvite, error)
	// RevokeUserInvite 撤回一次邀请关系（仅影响邀请码记录；注销账号由上层处理）。
	RevokeUserInvite(ctx context.Context, inviterTelegramID int64, inviteeTelegramID int64) (*DirectInvite, error)

	// LatestUserInviteUsedAt 返回该用户最近一次“邀请成功（邀请码被兑换）”的时间。
	LatestUserInviteUsedAt(ctx context.Context, inviterTelegramID int64) (*time.Time, error)
	// CleanupUnusedCodes 清理该用户创建但尚未使用的验证码（避免删号后遗留可用邀请码）。
	CleanupUnusedCodes(ctx context.Context, inviterTelegramID int64) (int64, error)
}

type service struct {
	users registration.UserRepository
	codes registration.InviteCodeRepository
	graph GraphRepository
	now   func() time.Time

	minAccountAgeDays int
	cooldownDays      int
}

type Options struct {
	Now               func() time.Time
	MinAccountAgeDays int
	CooldownDays      int
}

func NewService(users registration.UserRepository, codes registration.InviteCodeRepository, graph GraphRepository, opts Options) Service {
	nowFn := opts.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	minDays := opts.MinAccountAgeDays
	if minDays < 0 {
		minDays = DefaultMinAccountAgeDays
	}
	cooldownDays := opts.CooldownDays
	if cooldownDays < 0 {
		cooldownDays = DefaultCooldownDays
	}
	return &service{
		users:             users,
		codes:             codes,
		graph:             graph,
		now:               nowFn,
		minAccountAgeDays: minDays,
		cooldownDays:      cooldownDays,
	}
}

func (s *service) PrepareUserInviteCode(ctx context.Context, inviterTelegramID int64) (InviteCodeResult, error) {
	if inviterTelegramID == 0 {
		return InviteCodeResult{Eligible: false, Reason: "telegram id is empty"}, registration.ErrInvalidInput
	}
	if s.users == nil || s.codes == nil || s.graph == nil {
		return InviteCodeResult{Eligible: false, Reason: "service not initialized"}, fmt.Errorf("invite service not initialized")
	}

	account, err := s.users.FindByTelegramID(ctx, inviterTelegramID)
	if err != nil {
		return InviteCodeResult{Eligible: false}, err
	}
	if account == nil || strings.TrimSpace(account.EmbyUserID) == "" {
		return InviteCodeResult{
			Eligible: false,
			Reason:   "你还没有注册/绑定账号，暂时无法邀请。",
		}, nil
	}

	now := s.now()
	if !account.IsWhitelist && account.ExpiresAt != nil && now.After(*account.ExpiresAt) {
		return InviteCodeResult{
			Eligible: false,
			Reason:   "你的账号已过期，暂时无法邀请。",
		}, nil
	}

	eligibleAt := account.CreatedAt.AddDate(0, 0, s.minAccountAgeDays)
	if now.Before(eligibleAt) {
		return InviteCodeResult{
			Eligible:      false,
			Reason:        fmt.Sprintf("持有账号满 %d 天后才允许邀请。", s.minAccountAgeDays),
			NextAllowedAt: &eligibleAt,
		}, nil
	}

	// 若已有“未使用”的邀请邀请码，则直接复用（避免用户无限生成邀请码绕过冷却）。
	if code, err := s.graph.LatestUnusedCodeByCreatorPrefix(ctx, inviterTelegramID, UserInviteCodePrefix); err != nil {
		return InviteCodeResult{Eligible: false}, err
	} else if strings.TrimSpace(code) != "" {
		return InviteCodeResult{
			Eligible: true,
			Code:     strings.TrimSpace(code),
			IsNew:    false,
		}, nil
	}

	lastUsedAt, err := s.graph.LatestUsedAtByCreatorPrefix(ctx, inviterTelegramID, UserInviteCodePrefix)
	if err != nil {
		return InviteCodeResult{Eligible: false}, err
	}
	if lastUsedAt != nil && !lastUsedAt.IsZero() {
		next := lastUsedAt.AddDate(0, 0, s.cooldownDays)
		if now.Before(next) {
			return InviteCodeResult{
				Eligible:      false,
				Reason:        fmt.Sprintf("首次邀请成功后需间隔 %d 天才允许再次邀请。", s.cooldownDays),
				NextAllowedAt: &next,
			}, nil
		}
	}

	created, err := s.codes.CreateBatch(ctx, inviterTelegramID, 0, 1, UserInviteCodePrefix)
	if err != nil || len(created) == 0 || strings.TrimSpace(created[0]) == "" {
		if err == nil {
			err = fmt.Errorf("create invite code failed")
		}
		return InviteCodeResult{Eligible: false}, err
	}
	return InviteCodeResult{
		Eligible: true,
		Code:     strings.TrimSpace(created[0]),
		IsNew:    true,
	}, nil
}

func (s *service) PrepareTargetedInviteCode(ctx context.Context, inviterTelegramID int64, inviteeTelegramID int64) (InviteCodeResult, error) {
	if inviterTelegramID == 0 || inviteeTelegramID == 0 {
		return InviteCodeResult{Eligible: false, Reason: "telegram id is empty"}, registration.ErrInvalidInput
	}
	if inviterTelegramID == inviteeTelegramID {
		return InviteCodeResult{Eligible: false, Reason: "cannot invite self"}, registration.ErrInvalidInput
	}
	if s.codes == nil {
		return InviteCodeResult{Eligible: false, Reason: "service not initialized"}, fmt.Errorf("invite code repository not initialized")
	}

	// 若对方已经被该邀请者预留过邀请码（未使用），则直接复用，避免重复生成。
	if reserved, err := s.codes.GetReservedByUser(ctx, inviteeTelegramID); err == nil && reserved != nil {
		code := strings.TrimSpace(reserved.Code)
		if reserved.CreatorTelegramID == inviterTelegramID && strings.HasPrefix(strings.ToLower(code), strings.ToLower(UserInviteCodePrefix)) {
			return InviteCodeResult{Eligible: true, Code: code, IsNew: false}, nil
		}
		// 已被他人预留：不覆盖，避免抢占/混乱。
		if strings.HasPrefix(strings.ToLower(code), strings.ToLower(UserInviteCodePrefix)) {
			return InviteCodeResult{Eligible: false, Reason: "对方已有其他邀请资格，请稍后再试。"}, nil
		}
	}

	// 先按“邀请者资格”规则校验并生成一个可用邀请码（未预留）。
	res, err := s.PrepareUserInviteCode(ctx, inviterTelegramID)
	if err != nil {
		return InviteCodeResult{Eligible: false}, err
	}
	if !res.Eligible || strings.TrimSpace(res.Code) == "" {
		return res, nil
	}

	// 将邀请码预留给目标 TGID：对方私聊 /start 后即可直接注册（无需手动拿码）。
	if _, err := s.codes.ReserveForUser(ctx, res.Code, inviteeTelegramID); err != nil {
		return InviteCodeResult{Eligible: false}, err
	}
	res.IsNew = true
	return res, nil
}

func (s *service) Descendants(ctx context.Context, rootInviterTelegramID int64) ([]int64, error) {
	if rootInviterTelegramID == 0 {
		return nil, registration.ErrInvalidInput
	}
	if s.graph == nil {
		return nil, fmt.Errorf("invite graph repository not initialized")
	}

	visited := map[int64]struct{}{rootInviterTelegramID: {}}
	queue := []int64{rootInviterTelegramID}
	out := make([]int64, 0, 64)

	for len(queue) > 0 {
		// 批量拉取，减少 DB 往返。
		batch := queue
		queue = nil

		edges, err := s.graph.ListEdgesByCreatorsPrefix(ctx, batch, UserInviteCodePrefix)
		if err != nil {
			return nil, err
		}
		for _, e := range edges {
			id := e.InviteeTelegramID
			if id == 0 {
				continue
			}
			if _, ok := visited[id]; ok {
				continue
			}
			visited[id] = struct{}{}
			out = append(out, id)
			queue = append(queue, id)
		}
	}

	return out, nil
}

func (s *service) ListUserInvites(ctx context.Context, inviterTelegramID int64) ([]DirectInvite, error) {
	if inviterTelegramID == 0 {
		return nil, registration.ErrInvalidInput
	}
	if s.graph == nil {
		return nil, fmt.Errorf("invite graph repository not initialized")
	}
	return s.graph.ListDirectInvitesByCreatorPrefix(ctx, inviterTelegramID, UserInviteCodePrefix)
}

func (s *service) GetUserInvite(ctx context.Context, inviterTelegramID int64, inviteeTelegramID int64) (*DirectInvite, error) {
	if inviterTelegramID == 0 || inviteeTelegramID == 0 {
		return nil, registration.ErrInvalidInput
	}
	if s.graph == nil {
		return nil, fmt.Errorf("invite graph repository not initialized")
	}
	return s.graph.GetDirectInviteByCreatorInviteePrefix(ctx, inviterTelegramID, inviteeTelegramID, UserInviteCodePrefix)
}

func (s *service) RevokeUserInvite(ctx context.Context, inviterTelegramID int64, inviteeTelegramID int64) (*DirectInvite, error) {
	if inviterTelegramID == 0 || inviteeTelegramID == 0 {
		return nil, registration.ErrInvalidInput
	}
	if s.graph == nil {
		return nil, fmt.Errorf("invite graph repository not initialized")
	}
	return s.graph.RevokeDirectInviteByCreatorInviteePrefix(ctx, inviterTelegramID, inviteeTelegramID, UserInviteCodePrefix)
}

func (s *service) LatestUserInviteUsedAt(ctx context.Context, inviterTelegramID int64) (*time.Time, error) {
	if inviterTelegramID == 0 {
		return nil, registration.ErrInvalidInput
	}
	if s.graph == nil {
		return nil, fmt.Errorf("invite graph repository not initialized")
	}
	return s.graph.LatestUsedAtByCreatorPrefix(ctx, inviterTelegramID, UserInviteCodePrefix)
}

func (s *service) CleanupUnusedCodes(ctx context.Context, inviterTelegramID int64) (int64, error) {
	if inviterTelegramID == 0 {
		return 0, registration.ErrInvalidInput
	}
	if s.codes == nil {
		return 0, fmt.Errorf("invite code repository not initialized")
	}
	return s.codes.DeleteAllUnusedByCreator(ctx, inviterTelegramID)
}
