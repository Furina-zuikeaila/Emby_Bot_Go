package account

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"emby-bot-new/internal/application/registration"
)

type fakeRepo struct {
	account *registration.Account

	updatedTelegramID int64
	updatedEmbyUserID string
	updateErr         error
}

func (r *fakeRepo) FindByTelegramID(_ context.Context, telegramID int64) (*registration.Account, error) {
	if r.account == nil || telegramID != r.account.TelegramID {
		return &registration.Account{TelegramID: telegramID}, nil
	}
	cp := *r.account
	return &cp, nil
}
func (r *fakeRepo) UpdateExpiresAt(context.Context, int64, *time.Time) error { return nil }
func (r *fakeRepo) DeleteByTelegramID(context.Context, int64) (*registration.Account, error) {
	return nil, errors.New("not implemented")
}
func (r *fakeRepo) UpdateEmbyUserID(_ context.Context, telegramID int64, embyUserID string) error {
	r.updatedTelegramID = telegramID
	r.updatedEmbyUserID = embyUserID
	return r.updateErr
}

type fakeCodes struct{}

func (f fakeCodes) ReserveForUser(context.Context, string, int64) (*registration.InviteCode, error) {
	return nil, errors.New("not implemented")
}
func (f fakeCodes) ConfirmUsage(context.Context, string, int64) error {
	return errors.New("not implemented")
}
func (f fakeCodes) ClearUserReservations(context.Context, int64) error { return nil }

type fakeEmby struct {
	updateErr  error
	createID   string
	createErr  error
	deletedIDs []string
}

func (e *fakeEmby) UpdateUserPassword(context.Context, string, string) error { return e.updateErr }
func (e *fakeEmby) DeleteUser(_ context.Context, embyUserID string) error {
	e.deletedIDs = append(e.deletedIDs, embyUserID)
	return nil
}
func (e *fakeEmby) GetUser(context.Context, string) (map[string]any, error) {
	return nil, errors.New("not implemented")
}
func (e *fakeEmby) UpdateUserPolicy(context.Context, string, map[string]any) error {
	return errors.New("not implemented")
}
func (e *fakeEmby) GetLibraries(context.Context) ([]map[string]any, error) {
	return nil, errors.New("not implemented")
}
func (e *fakeEmby) GetSessions(context.Context) ([]map[string]any, error) {
	return nil, errors.New("not implemented")
}
func (e *fakeEmby) GetActiveSessionsCount(context.Context) (int, error) {
	return 0, errors.New("not implemented")
}
func (e *fakeEmby) GetPlaybackHistory(context.Context, string, int) ([]map[string]any, error) {
	return nil, errors.New("not implemented")
}
func (e *fakeEmby) GetActivityLogEntries(context.Context, int, int, *time.Time) ([]ActivityLogEntry, error) {
	return nil, errors.New("not implemented")
}
func (e *fakeEmby) CreateUser(context.Context, string, string) (string, error) {
	return e.createID, e.createErr
}

func mustSecureCodeHash(t *testing.T, code string) (saltHex string, hashHex string) {
	t.Helper()
	salt := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	sum := sha256.Sum256(append(append([]byte{}, salt...), []byte(":"+code)...))
	return hex.EncodeToString(salt), hex.EncodeToString(sum[:])
}

func TestResetPassword_RecreateWhenEmbyUserMissing(t *testing.T) {
	acc := &registration.Account{
		TelegramID:   1,
		EmbyUserID:   "old",
		EmbyUsername: "u",
	}
	acc.SecureCodeSalt, acc.SecureCodeHash = mustSecureCodeHash(t, "1234")

	repo := &fakeRepo{account: acc}
	emby := &fakeEmby{updateErr: errors.New("emby api failed: status=404"), createID: "new"}

	s := NewService(repo, fakeCodes{}, emby, Options{PasswordLength: 12})

	gotAcc, cred, err := s.ResetPassword(context.Background(), 1, "1234", "newpass123")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if gotAcc.EmbyUserID != "new" {
		t.Fatalf("expected new emby user id, got %q", gotAcc.EmbyUserID)
	}
	if repo.updatedEmbyUserID != "new" {
		t.Fatalf("expected repo updated to new, got %q", repo.updatedEmbyUserID)
	}
	if cred.Username != "u" || cred.Password != "newpass123" {
		t.Fatalf("unexpected cred: %+v", cred)
	}
}
