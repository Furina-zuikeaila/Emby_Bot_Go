package router

import (
	"sync"
	"time"
)

// convoState 表示“用户当前输入流程”的状态机状态。
//
// 在 Telegram 的交互中，用户输入往往是跨多条消息/多次点击完成的（例如先点按钮，再输入邀请码/安全码）。
// 因此需要在内存中记录用户处于哪一步，以便把下一条文本解释为对应字段。
//
// 约束：
// - 该状态是“短期”的：为了避免用户在历史消息上继续输入导致误操作，状态会在超时后自动失效。
// - 该状态是“进程内”的：重启 bot 后会丢失；用户需要重新从面板开始流程（这是可接受的）。
type convoState int

const (
	convoNone convoState = iota
	convoRegisterInput
	convoInviteCode
	convoBindUsername
	convoBindPassword
	convoBindSecureCode

	convoRenewCode
	convoResetPassword
	convoDeleteAccount
	convoUserInviteTarget
	convoHaremRevokeInput

	convoAdminSetTiming
	convoAdminSetMaxUsers
	convoAdminSetDefaultDays
	convoAdminCreateCodes
	convoAdminCreateRenewCodes
	convoAdminGrantQualification
	convoAdminSetInactiveDuration
	convoAdminWhitelistAdd
	convoAdminWhitelistRemove

	convoCrowdfundTxHash
)

// convoSession 保存一次会话状态：
// - State：用户处于哪个输入步骤
// - UpdatedAt：最后一次更新，用于超时淘汰
// - Values：跨步骤临时携带的键值（例如上一页选择的 tg_id、分页信息等）
type convoSession struct {
	State     convoState
	UpdatedAt time.Time
	Values    map[string]string
}

// convoStore 是一个轻量的、线程安全的会话缓存。
//
// 设计选择：
// - 使用 map[userID]convoSession 保存用户状态，适合单进程、小规模场景。
// - 通过互斥锁保证并发安全（telebot 的 handler 可能并发执行）。
// - 通过 timeout 做简单 TTL，避免 map 无界增长，也避免“很久之前的输入”被误解析。
type convoStore struct {
	mu       sync.Mutex
	timeout  time.Duration
	sessions map[int64]convoSession
}

// newConvoStore 创建会话存储。
// timeout<=0 时使用保守默认值，避免长期残留导致误操作。
func newConvoStore(timeout time.Duration) *convoStore {
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	return &convoStore{
		timeout:  timeout,
		sessions: make(map[int64]convoSession),
	}
}

// Set 设置/覆盖某个用户的会话状态。
// values 允许为 nil；调用方应确保写入的值不包含敏感信息（一般只存 UI 流程参数）。
func (s *convoStore) Set(userID int64, state convoState, values map[string]string) {
	if userID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[userID] = convoSession{
		State:     state,
		UpdatedAt: time.Now(),
		Values:    values,
	}
}

// Get 读取会话并执行 TTL 校验。
// 如果超时，则会自动清理并返回 (zero, false)。
func (s *convoStore) Get(userID int64) (convoSession, bool) {
	if userID == 0 {
		return convoSession{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[userID]
	if !ok {
		return convoSession{}, false
	}
	if time.Since(sess.UpdatedAt) > s.timeout {
		delete(s.sessions, userID)
		return convoSession{}, false
	}
	return sess, true
}

// Clear 清理指定用户的会话状态（通常用于“取消”按钮或流程结束）。
func (s *convoStore) Clear(userID int64) {
	if userID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, userID)
}
