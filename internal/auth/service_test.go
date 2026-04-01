package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/misterchenleiya/meeting/internal/storage/sqlite"
)

func TestRequestLoginCodeAllowsUnregisteredEmail(t *testing.T) {
	t.Parallel()

	store := openTestAuthStore(t)
	service := NewService(store, nil)

	delivery, err := service.RequestLoginCode(context.Background(), "new-user@example.com")
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

	delivery, err := service.RequestLoginCode(ctx, "auto-register@example.com")
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
