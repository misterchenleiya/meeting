package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/misterchenleiya/meeting/internal/auth"
)

const verificationClientCookieName = "meeting_verification_client"

type authCodeRequest struct {
	Email    string `json:"email"`
	Nickname string `json:"nickname,omitempty"`
}

type authVerifyRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type authPasswordLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authWechatMiniLoginRequest struct {
	Code string `json:"code"`
}

func (s *Server) handleRegisterCode(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusNotImplemented, "auth service is not available")
		return
	}

	var request authCodeRequest
	if !decodeJSON(w, r, &request) {
		return
	}

	clientID, shouldPersistCookie, err := ensureVerificationClientID(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to initialize verification client")
		return
	}

	delivery, err := s.auth.RequestRegisterCode(r.Context(), request.Email, request.Nickname, auth.VerificationRequestMeta{
		ClientID:  clientID,
		IPAddress: verificationRateLimitIP(r),
	})
	if err != nil {
		s.writeAuthError(w, err)
		return
	}
	if shouldPersistCookie {
		setVerificationClientCookie(w, r, clientID)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "code_sent",
		"email":        delivery.Email,
		"purpose":      delivery.Purpose,
		"debugCode":    delivery.DebugCode,
		"expiresAt":    delivery.ExpiresAt,
		"resendAfter":  delivery.ResendAfter,
		"deliveryMode": delivery.DeliveryMode,
	})
}

func (s *Server) handleRegisterVerify(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusNotImplemented, "auth service is not available")
		return
	}

	var request authVerifyRequest
	if !decodeJSON(w, r, &request) {
		return
	}

	user, err := s.auth.CompleteRegistration(r.Context(), request.Email, request.Code)
	if err != nil {
		s.writeAuthError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"status": "registered",
		"user":   user,
	})
}

func (s *Server) handleLoginCode(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusNotImplemented, "auth service is not available")
		return
	}

	var request authCodeRequest
	if !decodeJSON(w, r, &request) {
		return
	}

	clientID, shouldPersistCookie, err := ensureVerificationClientID(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to initialize verification client")
		return
	}

	delivery, err := s.auth.RequestLoginCode(r.Context(), request.Email, auth.VerificationRequestMeta{
		ClientID:  clientID,
		IPAddress: verificationRateLimitIP(r),
	})
	if err != nil {
		s.writeAuthError(w, err)
		return
	}
	if shouldPersistCookie {
		setVerificationClientCookie(w, r, clientID)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "code_sent",
		"email":        delivery.Email,
		"purpose":      delivery.Purpose,
		"debugCode":    delivery.DebugCode,
		"expiresAt":    delivery.ExpiresAt,
		"resendAfter":  delivery.ResendAfter,
		"deliveryMode": delivery.DeliveryMode,
	})
}

func (s *Server) handleLoginVerify(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusNotImplemented, "auth service is not available")
		return
	}

	var request authVerifyRequest
	if !decodeJSON(w, r, &request) {
		return
	}

	user, session, autoRegistered, err := s.auth.CompleteLogin(
		r.Context(),
		request.Email,
		request.Code,
		r.UserAgent(),
		clientIP(r),
	)
	if err != nil {
		s.writeAuthError(w, err)
		return
	}

	setAuthSessionCookie(w, r, session.Token, session.ExpiresAt)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "logged_in",
		"user":           user,
		"autoRegistered": autoRegistered,
	})
}

func (s *Server) handlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusNotImplemented, "auth service is not available")
		return
	}

	var request authPasswordLoginRequest
	if !decodeJSON(w, r, &request) {
		return
	}

	user, session, err := s.auth.CompletePasswordLogin(
		r.Context(),
		request.Email,
		request.Password,
		r.UserAgent(),
		clientIP(r),
	)
	if err != nil {
		s.writeAuthError(w, err)
		return
	}

	setAuthSessionCookie(w, r, session.Token, session.ExpiresAt)
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "logged_in",
		"user":   user,
	})
}

func (s *Server) handleWechatMiniProgramLogin(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusNotImplemented, "auth service is not available")
		return
	}

	var request authWechatMiniLoginRequest
	if !decodeJSON(w, r, &request) {
		return
	}

	user, session, autoRegistered, err := s.auth.CompleteWechatMiniProgramLogin(
		r.Context(),
		request.Code,
		r.UserAgent(),
		clientIP(r),
	)
	if err != nil {
		s.writeAuthError(w, err)
		return
	}

	setAuthSessionCookie(w, r, session.Token, session.ExpiresAt)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "logged_in",
		"user":           user,
		"sessionToken":   session.Token,
		"sessionEndsAt":  session.ExpiresAt,
		"autoRegistered": autoRegistered,
		"loginMethod":    "wechat_miniprogram",
	})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusNotImplemented, "auth service is not available")
		return
	}

	user, session, err := s.currentAuthenticatedUser(r)
	if err != nil {
		s.writeAuthError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user":          user,
		"sessionEndsAt": session.ExpiresAt,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusNotImplemented, "auth service is not available")
		return
	}

	token, found := readAuthSessionToken(r)
	if found {
		if err := s.auth.Logout(r.Context(), token); err != nil {
			s.writeAuthError(w, err)
			return
		}
	}

	clearAuthSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "logged_out",
	})
}

func (s *Server) currentAuthenticatedUser(r *http.Request) (auth.User, auth.Session, error) {
	if s.auth == nil {
		return auth.User{}, auth.Session{}, errors.New("auth service is not available")
	}

	token, found := readAuthSessionToken(r)
	if !found {
		return auth.User{}, auth.Session{}, auth.ErrSessionNotFound
	}

	return s.auth.GetCurrentUser(r.Context(), token)
}

func (s *Server) requireAuthenticatedUser(w http.ResponseWriter, r *http.Request) (auth.User, bool) {
	user, _, err := s.currentAuthenticatedUser(r)
	if err != nil {
		s.writeAuthError(w, err)
		return auth.User{}, false
	}
	return user, true
}

func (s *Server) writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrEmailRequired),
		errors.Is(err, auth.ErrEmailInvalid),
		errors.Is(err, auth.ErrNicknameRequired),
		errors.Is(err, auth.ErrVerificationCodeRequired),
		errors.Is(err, auth.ErrPasswordRequired):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, auth.ErrWechatCodeRequired):
		writeError(w, http.StatusBadRequest, "微信登录凭证缺失，请重新发起登录")
	case errors.Is(err, auth.ErrAlreadyRegistered):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, auth.ErrPasswordNotSet):
		writeError(w, http.StatusConflict, "该账号尚未设置密码，请使用邮箱验证码登录")
	case errors.Is(err, auth.ErrWechatLoginUnavailable):
		writeError(w, http.StatusServiceUnavailable, "微信小程序登录暂未配置")
	case errors.Is(err, auth.ErrNotRegistered):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, auth.ErrVerificationCodeResendTooSoon):
		writeError(w, http.StatusTooManyRequests, "验证码发送过于频繁，请 1 分钟后再试")
	case errors.Is(err, auth.ErrVerificationCodeRateLimited):
		writeError(w, http.StatusTooManyRequests, "当前网络请求过于频繁，请稍后再试")
	case errors.Is(err, auth.ErrVerificationCodeExpired),
		errors.Is(err, auth.ErrVerificationCodeInvalid),
		errors.Is(err, auth.ErrVerificationAttemptsExceeded),
		errors.Is(err, auth.ErrPasswordInvalid):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, auth.ErrWechatLoginFailed):
		s.logger.Warn("wechat mini program login failed", "error", err)
		writeError(w, http.StatusUnauthorized, "微信登录失败，请稍后重试")
	case errors.Is(err, auth.ErrSessionNotFound), errors.Is(err, auth.ErrSessionExpired):
		writeError(w, http.StatusUnauthorized, err.Error())
	default:
		s.logger.Error("auth operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "auth operation failed")
	}
}

func readAuthSessionToken(r *http.Request) (string, bool) {
	if cookie, err := r.Cookie(auth.CookieName); err == nil {
		token := strings.TrimSpace(cookie.Value)
		if token != "" {
			return token, true
		}
	}

	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		token := strings.TrimSpace(authHeader[len("Bearer "):])
		if token != "" {
			return token, true
		}
	}

	return "", false
}

func setAuthSessionCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   shouldUseSecureCookie(r),
		Expires:  expiresAt.UTC(),
		MaxAge:   int(time.Until(expiresAt).Seconds()),
	})
}

func clearAuthSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   shouldUseSecureCookie(r),
		Expires:  time.Unix(0, 0).UTC(),
		MaxAge:   -1,
	})
}

func shouldUseSecureCookie(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}

	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func readVerificationClientID(r *http.Request) (string, bool) {
	if cookie, err := r.Cookie(verificationClientCookieName); err == nil {
		value := strings.TrimSpace(cookie.Value)
		if value != "" {
			return value, true
		}
	}

	return "", false
}

func ensureVerificationClientID(r *http.Request) (string, bool, error) {
	if clientID, found := readVerificationClientID(r); found {
		return clientID, false, nil
	}

	clientID, err := newVerificationClientID()
	if err != nil {
		return "", false, err
	}

	return clientID, true, nil
}

func setVerificationClientCookie(w http.ResponseWriter, r *http.Request, clientID string) {
	const maxAgeSeconds = 180 * 24 * 60 * 60

	http.SetCookie(w, &http.Cookie{
		Name:     verificationClientCookieName,
		Value:    clientID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   shouldUseSecureCookie(r),
		MaxAge:   maxAgeSeconds,
		Expires:  time.Now().UTC().Add(180 * 24 * time.Hour),
	})
}

func newVerificationClientID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func verificationRateLimitIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}

	remoteAddr := strings.TrimSpace(r.RemoteAddr)
	if remoteAddr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}
