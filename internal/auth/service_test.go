package auth

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/misterchenleiya/meeting/internal/storage/sqlite"
)

type fakeWechatMiniProgramCodeExchanger struct {
	openID string
	err    error
}

func (f fakeWechatMiniProgramCodeExchanger) ExchangeCode(_ context.Context, _ string) (WechatMiniProgramIdentity, error) {
	if f.err != nil {
		return WechatMiniProgramIdentity{}, f.err
	}
	return WechatMiniProgramIdentity{OpenID: f.openID}, nil
}

func TestRequestLoginCodeAllowsUnregisteredEmail(t *testing.T) {
	t.Parallel()

	store := openTestAuthStore(t)
	service := NewService(store, nil)

	delivery, err := service.RequestLoginCode(context.Background(), "new-user@example.com", VerificationRequestMeta{
		ClientID:  "test-client-1",
		IPAddress: "203.0.113.10",
	})
	if err != nil {
		t.Fatalf("RequestLoginCode() error = %v", err)
	}

	if delivery.Purpose != loginPurpose {
		t.Fatalf("RequestLoginCode() purpose = %q, want %q", delivery.Purpose, loginPurpose)
	}
	if delivery.DebugCode == "" {
		t.Fatalf("RequestLoginCode() debug code is empty")
	}
	if delivery.DeliveryMode != MailerModeDebug {
		t.Fatalf("RequestLoginCode() delivery mode = %q, want %q", delivery.DeliveryMode, MailerModeDebug)
	}
}

func TestCompleteLoginAutoRegistersUnregisteredEmail(t *testing.T) {
	t.Parallel()

	store := openTestAuthStore(t)
	service := NewService(store, nil)
	ctx := context.Background()

	delivery, err := service.RequestLoginCode(ctx, "auto-register@example.com", VerificationRequestMeta{
		ClientID:  "test-client-2",
		IPAddress: "203.0.113.11",
	})
	if err != nil {
		t.Fatalf("RequestLoginCode() error = %v", err)
	}

	user, session, autoRegistered, err := service.CompleteLogin(
		ctx,
		"auto-register@example.com",
		delivery.DebugCode,
		"test-agent",
		"127.0.0.1",
	)
	if err != nil {
		t.Fatalf("CompleteLogin() error = %v", err)
	}

	if !autoRegistered {
		t.Fatalf("CompleteLogin() autoRegistered = false, want true")
	}
	if session.Token == "" {
		t.Fatalf("CompleteLogin() session token is empty")
	}
	if user.Email != "auto-register@example.com" {
		t.Fatalf("CompleteLogin() email = %q", user.Email)
	}
	if user.EmailVerifiedAt == nil {
		t.Fatalf("CompleteLogin() emailVerifiedAt is nil")
	}
	if !strings.HasPrefix(user.Nickname, "auto-register") {
		t.Fatalf("CompleteLogin() nickname = %q, want prefix %q", user.Nickname, "auto-register")
	}
}

func TestCompletePasswordLoginRejectsUserWithoutPassword(t *testing.T) {
	t.Parallel()

	store := openTestAuthStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	userRecord := sqlite.UserRecord{
		ID:              "usr_test_password",
		Email:           "no-password@example.com",
		PasswordHash:    "",
		Nickname:        "no-password",
		EmailVerifiedAt: &now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := store.CreateUser(ctx, userRecord); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	service := NewService(store, nil)
	_, _, err := service.CompletePasswordLogin(
		ctx,
		userRecord.Email,
		"secret",
		"test-agent",
		"127.0.0.1",
	)
	if err != ErrPasswordNotSet {
		t.Fatalf("CompletePasswordLogin() error = %v, want %v", err, ErrPasswordNotSet)
	}
}

func TestCompleteWechatMiniProgramLoginAutoRegisters(t *testing.T) {
	t.Parallel()

	store := openTestAuthStore(t)
	service := NewService(store, nil, WithWechatMiniProgramCodeExchanger(fakeWechatMiniProgramCodeExchanger{
		openID: "wechat-openid-001",
	}))

	user, session, autoRegistered, err := service.CompleteWechatMiniProgramLogin(
		context.Background(),
		"wx-code",
		"wechat-agent",
		"127.0.0.1",
	)
	if err != nil {
		t.Fatalf("CompleteWechatMiniProgramLogin() error = %v", err)
	}
	if !autoRegistered {
		t.Fatalf("CompleteWechatMiniProgramLogin() autoRegistered = false, want true")
	}
	if session.Token == "" {
		t.Fatalf("CompleteWechatMiniProgramLogin() session token is empty")
	}
	if user.Email != "" {
		t.Fatalf("CompleteWechatMiniProgramLogin() email = %q, want empty", user.Email)
	}
	if !strings.HasPrefix(user.Nickname, "微信用户") {
		t.Fatalf("CompleteWechatMiniProgramLogin() nickname = %q", user.Nickname)
	}
}

func TestCompleteWechatMiniProgramLoginUsesExistingUser(t *testing.T) {
	t.Parallel()

	store := openTestAuthStore(t)
	now := time.Now().UTC()
	if err := store.CreateUser(context.Background(), sqlite.UserRecord{
		ID:           "usr_wechat_existing",
		WechatOpenID: "wechat-openid-existing",
		Nickname:     "已绑定微信用户",
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	service := NewService(store, nil, WithWechatMiniProgramCodeExchanger(fakeWechatMiniProgramCodeExchanger{
		openID: "wechat-openid-existing",
	}))

	user, _, autoRegistered, err := service.CompleteWechatMiniProgramLogin(
		context.Background(),
		"wx-code",
		"wechat-agent",
		"127.0.0.1",
	)
	if err != nil {
		t.Fatalf("CompleteWechatMiniProgramLogin() error = %v", err)
	}
	if autoRegistered {
		t.Fatalf("CompleteWechatMiniProgramLogin() autoRegistered = true, want false")
	}
	if user.ID != "usr_wechat_existing" {
		t.Fatalf("CompleteWechatMiniProgramLogin() user id = %q", user.ID)
	}
}

func TestVerificationCodeClientCooldownAppliesAcrossEmails(t *testing.T) {
	t.Parallel()

	store := openTestAuthStore(t)
	service := NewService(store, nil)
	now := time.Date(2026, time.April, 2, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	meta := VerificationRequestMeta{
		ClientID:  "client-cooldown",
		IPAddress: "203.0.113.21",
	}

	if _, err := service.RequestLoginCode(context.Background(), "first@example.com", meta); err != nil {
		t.Fatalf("RequestLoginCode(first) error = %v", err)
	}

	if _, err := service.RequestLoginCode(context.Background(), "second@example.com", meta); err != ErrVerificationCodeResendTooSoon {
		t.Fatalf("RequestLoginCode(second) error = %v, want %v", err, ErrVerificationCodeResendTooSoon)
	}

	now = now.Add(61 * time.Second)
	if _, err := service.RequestLoginCode(context.Background(), "second@example.com", meta); err != nil {
		t.Fatalf("RequestLoginCode(after cooldown) error = %v", err)
	}
}

func TestVerificationCodeIPFallbackRateLimit(t *testing.T) {
	t.Parallel()

	store := openTestAuthStore(t)
	service := NewService(store, nil)
	now := time.Date(2026, time.April, 2, 11, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	for index := 0; index < defaultIPRateLimit; index++ {
		_, err := service.RequestLoginCode(context.Background(), fmt.Sprintf("user-%d@example.com", index), VerificationRequestMeta{
			ClientID:  fmt.Sprintf("client-%d", index),
			IPAddress: "198.51.100.8",
		})
		if err != nil {
			t.Fatalf("RequestLoginCode(%d) error = %v", index, err)
		}
	}

	_, err := service.RequestLoginCode(context.Background(), "overflow@example.com", VerificationRequestMeta{
		ClientID:  "client-overflow",
		IPAddress: "198.51.100.8",
	})
	if err != ErrVerificationCodeRateLimited {
		t.Fatalf("RequestLoginCode(overflow) error = %v, want %v", err, ErrVerificationCodeRateLimited)
	}

	now = now.Add(defaultIPRateWindow + time.Second)
	if _, err := service.RequestLoginCode(context.Background(), "recovered@example.com", VerificationRequestMeta{
		ClientID:  "client-recovered",
		IPAddress: "198.51.100.8",
	}); err != nil {
		t.Fatalf("RequestLoginCode(after ip window) error = %v", err)
	}
}

func openTestAuthStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(context.Background(), t.TempDir()+"/meeting.db")
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}

	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Fatalf("store.Close() error = %v", closeErr)
		}
	})

	return store
}
