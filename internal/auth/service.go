package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"strings"
	"time"

	"github.com/misterchenleiya/meeting/internal/storage/sqlite"
)

const (
	CookieName = "meeting_session"

	registerPurpose = "register"
	loginPurpose    = "login"

	defaultCodeTTL           = 10 * time.Minute
	defaultCodeResendDelay   = 60 * time.Second
	defaultCodeMaxAttempts    = 5
	defaultSessionTTL         = 30 * 24 * time.Hour
	defaultUserIDPrefix       = "usr_"
	defaultVerificationPrefix = "vcode_"
)

var (
	ErrAlreadyRegistered            = errors.New("email already registered")
	ErrNotRegistered                 = errors.New("email not registered")
	ErrVerificationCodeRequired      = errors.New("verification code is required")
	ErrVerificationCodeExpired       = errors.New("verification code expired")
	ErrVerificationCodeInvalid       = errors.New("verification code invalid")
	ErrVerificationCodeResendTooSoon = errors.New("verification code request too frequent")
	ErrVerificationAttemptsExceeded  = errors.New("verification code attempts exceeded")
	ErrSessionNotFound               = errors.New("session not found")
	ErrSessionExpired                = errors.New("session expired")
	ErrNicknameRequired              = errors.New("nickname is required")
	ErrEmailRequired                 = errors.New("email is required")
	ErrEmailInvalid                  = errors.New("email is invalid")
)

type Service struct {
	store              *sqlite.Store
	codeTTL            time.Duration
	codeResendDelay    time.Duration
	maxCodeAttempts    int
	sessionTTL         time.Duration
	now                func() time.Time
}

type User struct {
	ID              string     `json:"id"`
	Email           string     `json:"email"`
	Nickname        string     `json:"nickname"`
	EmailVerifiedAt *time.Time `json:"emailVerifiedAt,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

type Session struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type CodeDelivery struct {
	Email        string    `json:"email"`
	Purpose      string    `json:"purpose"`
	DebugCode    string    `json:"debugCode,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt"`
	ResendAfter  time.Time `json:"resendAfter"`
	DeliveryMode string    `json:"deliveryMode"`
}

func NewService(store *sqlite.Store) *Service {
	return &Service{
		store:           store,
		codeTTL:         defaultCodeTTL,
		codeResendDelay: defaultCodeResendDelay,
		maxCodeAttempts: defaultCodeMaxAttempts,
		sessionTTL:      defaultSessionTTL,
		now:             time.Now,
	}
}

func (s *Service) RequestRegisterCode(ctx context.Context, email string, nickname string) (CodeDelivery, error) {
	normalizedEmail, err := normalizeEmail(email)
	if err != nil {
		return CodeDelivery{}, err
	}

	normalizedNickname := strings.TrimSpace(nickname)
	if normalizedNickname == "" {
		return CodeDelivery{}, ErrNicknameRequired
	}

	if user, found, err := s.store.GetUserByEmail(ctx, normalizedEmail); err != nil {
		return CodeDelivery{}, err
	} else if found && user.EmailVerifiedAt != nil {
		return CodeDelivery{}, ErrAlreadyRegistered
	}

	return s.issueVerificationCode(ctx, normalizedEmail, normalizedNickname, registerPurpose)
}

func (s *Service) RequestLoginCode(ctx context.Context, email string) (CodeDelivery, error) {
	normalizedEmail, err := normalizeEmail(email)
	if err != nil {
		return CodeDelivery{}, err
	}

	if user, found, err := s.store.GetUserByEmail(ctx, normalizedEmail); err != nil {
		return CodeDelivery{}, err
	} else if !found {
		return CodeDelivery{}, ErrNotRegistered
	} else if user.EmailVerifiedAt == nil {
		return CodeDelivery{}, ErrNotRegistered
	}

	return s.issueVerificationCode(ctx, normalizedEmail, "", loginPurpose)
}

func (s *Service) CompleteRegistration(ctx context.Context, email string, code string) (User, error) {
	normalizedEmail, err := normalizeEmail(email)
	if err != nil {
		return User{}, err
	}

	record, err := s.verifyLatestCode(ctx, normalizedEmail, registerPurpose, code)
	if err != nil {
		return User{}, err
	}

	if err := s.store.ConsumeVerificationCode(ctx, record.ID, s.now().UTC(), s.now().UTC()); err != nil {
		return User{}, err
	}

	now := s.now().UTC()
	existing, found, err := s.store.GetUserByEmail(ctx, normalizedEmail)
	if err != nil {
		return User{}, err
	}

	if found {
		if existing.EmailVerifiedAt != nil {
			return User{}, ErrAlreadyRegistered
		}

		if err := s.store.UpdateUserEmailVerification(ctx, existing.ID, now, record.Nickname, now); err != nil {
			return User{}, err
		}
		updated, _, err := s.store.GetUserByEmail(ctx, normalizedEmail)
		if err != nil {
			return User{}, err
		}
		return toUser(updated), nil
	}

	userID, err := generateUserID()
	if err != nil {
		return User{}, err
	}

	userRecord := sqlite.UserRecord{
		ID:       userID,
		Email:    normalizedEmail,
		Nickname: record.Nickname,
		EmailVerifiedAt: &now,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.store.CreateUser(ctx, userRecord); err != nil {
		return User{}, err
	}

	created, _, err := s.store.GetUserByEmail(ctx, normalizedEmail)
	if err != nil {
		return User{}, err
	}

	return toUser(created), nil
}

func (s *Service) CompleteLogin(ctx context.Context, email string, code string, userAgent string, ipAddress string) (User, Session, error) {
	normalizedEmail, err := normalizeEmail(email)
	if err != nil {
		return User{}, Session{}, err
	}

	userRecord, found, err := s.store.GetUserByEmail(ctx, normalizedEmail)
	if err != nil {
		return User{}, Session{}, err
	}
	if !found || userRecord.EmailVerifiedAt == nil {
		return User{}, Session{}, ErrNotRegistered
	}

	record, err := s.verifyLatestCode(ctx, normalizedEmail, loginPurpose, code)
	if err != nil {
		return User{}, Session{}, err
	}

	if err := s.store.ConsumeVerificationCode(ctx, record.ID, s.now().UTC(), s.now().UTC()); err != nil {
		return User{}, Session{}, err
	}

	token, tokenHash, err := generateSessionToken()
	if err != nil {
		return User{}, Session{}, err
	}

	now := s.now().UTC()
	session := sqlite.SessionRecord{
		TokenHash: tokenHash,
		UserID:    userRecord.ID,
		CreatedAt: now,
		ExpiresAt: now.Add(s.sessionTTL),
		UserAgent: strings.TrimSpace(userAgent),
		IPAddress: strings.TrimSpace(ipAddress),
	}

	if err := s.store.CreateSession(ctx, session); err != nil {
		return User{}, Session{}, err
	}

	return toUser(userRecord), Session{
		Token:     token,
		ExpiresAt: session.ExpiresAt,
	}, nil
}

func (s *Service) GetCurrentUser(ctx context.Context, sessionToken string) (User, Session, error) {
	normalizedToken := strings.TrimSpace(sessionToken)
	if normalizedToken == "" {
		return User{}, Session{}, ErrSessionNotFound
	}

	tokenHash := hashToken(normalizedToken)
	sessionRecord, found, err := s.store.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		return User{}, Session{}, err
	}
	if !found {
		return User{}, Session{}, ErrSessionNotFound
	}
	if sessionRecord.RevokedAt != nil {
		return User{}, Session{}, ErrSessionNotFound
	}
	if s.now().UTC().After(sessionRecord.ExpiresAt) {
		return User{}, Session{}, ErrSessionExpired
	}

	userRecord, found, err := s.store.GetUserByID(ctx, sessionRecord.UserID)
	if err != nil {
		return User{}, Session{}, err
	}
	if !found {
		return User{}, Session{}, ErrSessionNotFound
	}

	return toUser(userRecord), Session{
		Token:     normalizedToken,
		ExpiresAt: sessionRecord.ExpiresAt,
	}, nil
}

func (s *Service) Logout(ctx context.Context, sessionToken string) error {
	normalizedToken := strings.TrimSpace(sessionToken)
	if normalizedToken == "" {
		return nil
	}

	return s.store.RevokeSession(ctx, hashToken(normalizedToken), s.now().UTC())
}

func (s *Service) issueVerificationCode(ctx context.Context, email string, nickname string, purpose string) (CodeDelivery, error) {
	if latest, found, err := s.store.GetLatestVerificationCode(ctx, email, purpose); err != nil {
		return CodeDelivery{}, err
	} else if found {
		if latest.ConsumedAt == nil && s.now().UTC().Before(latest.SentAt.Add(s.codeResendDelay)) {
			return CodeDelivery{}, ErrVerificationCodeResendTooSoon
		}
	}

	code, err := generateNumericCode(6)
	if err != nil {
		return CodeDelivery{}, err
	}

	now := s.now().UTC()
	verificationID, err := randomHexID(12)
	if err != nil {
		return CodeDelivery{}, err
	}
	record := sqlite.VerificationCodeRecord{
		ID:           defaultVerificationPrefix + verificationID,
		Email:        email,
		Purpose:      purpose,
		Nickname:     nickname,
		CodeHash:     hashVerificationCode(code),
		AttemptCount: 0,
		SentAt:       now,
		ExpiresAt:    now.Add(s.codeTTL),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.store.UpsertVerificationCode(ctx, record); err != nil {
		return CodeDelivery{}, err
	}

	return CodeDelivery{
		Email:        email,
		Purpose:      purpose,
		DebugCode:    code,
		ExpiresAt:    record.ExpiresAt,
		ResendAfter:  now.Add(s.codeResendDelay),
		DeliveryMode: "debug",
	}, nil
}

func (s *Service) verifyLatestCode(ctx context.Context, email string, purpose string, rawCode string) (sqlite.VerificationCodeRecord, error) {
	code := strings.TrimSpace(rawCode)
	if code == "" {
		return sqlite.VerificationCodeRecord{}, ErrVerificationCodeRequired
	}

	record, found, err := s.store.GetLatestVerificationCode(ctx, email, purpose)
	if err != nil {
		return sqlite.VerificationCodeRecord{}, err
	}
	if !found {
		return sqlite.VerificationCodeRecord{}, ErrVerificationCodeInvalid
	}
	if record.ConsumedAt != nil {
		return sqlite.VerificationCodeRecord{}, ErrVerificationCodeInvalid
	}
	if s.now().UTC().After(record.ExpiresAt) {
		return sqlite.VerificationCodeRecord{}, ErrVerificationCodeExpired
	}
	if record.AttemptCount >= s.maxCodeAttempts {
		return sqlite.VerificationCodeRecord{}, ErrVerificationAttemptsExceeded
	}
	if hashVerificationCode(code) != record.CodeHash {
		if err := s.store.IncrementVerificationCodeAttempt(ctx, record.ID, s.now().UTC()); err != nil {
			return sqlite.VerificationCodeRecord{}, err
		}
		return sqlite.VerificationCodeRecord{}, ErrVerificationCodeInvalid
	}

	return record, nil
}

func normalizeEmail(email string) (string, error) {
	trimmed := strings.TrimSpace(strings.ToLower(email))
	if trimmed == "" {
		return "", ErrEmailRequired
	}

	addr, err := mail.ParseAddress(trimmed)
	if err != nil {
		return "", ErrEmailInvalid
	}

	if addr.Address == "" {
		return "", ErrEmailInvalid
	}

	return strings.ToLower(addr.Address), nil
}

func toUser(record sqlite.UserRecord) User {
	return User{
		ID:       record.ID,
		Email:    record.Email,
		Nickname: record.Nickname,
		EmailVerifiedAt: record.EmailVerifiedAt,
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}
}

func generateNumericCode(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("invalid code length %d", length)
	}

	max := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(length)), nil)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%0*d", length, n.Int64()), nil
}

func generateUserID() (string, error) {
	suffix, err := randomHexID(8)
	if err != nil {
		return "", err
	}
	return defaultUserIDPrefix + suffix, nil
}

func randomHexID(size int) (string, error) {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func generateSessionToken() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}

	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token), nil
}

func hashVerificationCode(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func hashToken(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}
