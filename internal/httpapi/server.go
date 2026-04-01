package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/misterchenleiya/meeting/internal/auth"
	"github.com/misterchenleiya/meeting/internal/meeting"
	"github.com/misterchenleiya/meeting/internal/signaling"
	"github.com/misterchenleiya/meeting/internal/storage/sqlite"
)

type Server struct {
	logger    *slog.Logger
	auth      *auth.Service
	meetings  *meeting.Service
	store     *sqlite.Store
	signaling *signaling.Hub
	mux       *http.ServeMux
}

func NewServer(logger *slog.Logger, authService *auth.Service, meetings *meeting.Service, store *sqlite.Store, signalingHub *signaling.Hub) *Server {
	server := &Server{
		logger:    logger,
		auth:      authService,
		meetings:  meetings,
		store:     store,
		signaling: signalingHub,
		mux:       http.NewServeMux(),
	}
	server.registerRoutes()
	return server
}

func (s *Server) Routes() http.Handler {
	return withJSONHeaders(s.mux)
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("POST /api/client-logs", s.handleClientLogs)
	s.mux.HandleFunc("POST /api/auth/register/code", s.handleRegisterCode)
	s.mux.HandleFunc("POST /api/auth/register/verify", s.handleRegisterVerify)
	s.mux.HandleFunc("POST /api/auth/login/code", s.handleLoginCode)
	s.mux.HandleFunc("POST /api/auth/login/verify", s.handleLoginVerify)
	s.mux.HandleFunc("POST /api/auth/login/password", s.handlePasswordLogin)
	s.mux.HandleFunc("POST /api/auth/wechat/mini/login", s.handleWechatMiniProgramLogin)
	s.mux.HandleFunc("GET /api/auth/me", s.handleMe)
	s.mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	s.mux.HandleFunc("POST /api/meetings", s.handleCreateMeeting)
	s.mux.HandleFunc("GET /api/meetings/{meetingID}", s.handleGetMeeting)
	s.mux.HandleFunc("GET /api/meetings/{meetingID}/minutes", s.handleGetMeetingMinutes)
	s.mux.HandleFunc("POST /api/meetings/{meetingID}/join", s.handleJoinMeeting)
	s.mux.HandleFunc("POST /api/meetings/{meetingID}/participants/{participantID}/leave", s.handleLeaveMeeting)
	s.mux.HandleFunc("POST /api/meetings/{meetingID}/participants/{participantID}/nickname", s.handleUpdateNickname)
	s.mux.HandleFunc("POST /api/meetings/{meetingID}/participants/{participantID}/capabilities/{capability}/grant", s.handleGrantCapability)
	s.mux.HandleFunc("POST /api/meetings/{meetingID}/participants/{participantID}/audit", s.handleAuditReport)
	s.mux.HandleFunc("POST /api/meetings/{meetingID}/end", s.handleEndMeeting)
	s.mux.HandleFunc("PUT /api/users/{userID}/preferences", s.handleSaveUserPreference)
	s.mux.HandleFunc("GET /ws/meetings/{meetingID}", s.handleWebSocketPlaceholder)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "meeting-api",
	})
}

func (s *Server) handleClientLogs(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)

	var request struct {
		Logs []map[string]any `json:"logs"`
	}

	if !decodeJSON(w, r, &request) {
		return
	}

	if len(request.Logs) == 0 {
		writeError(w, http.StatusBadRequest, "logs are required")
		return
	}

	if len(request.Logs) > 20 {
		writeError(w, http.StatusBadRequest, "too many logs in one request")
		return
	}

	for index, rawEntry := range request.Logs {
		entry, err := normalizeClientLogEntry(rawEntry)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid client log at index %d", index))
			return
		}

		attrs := append([]slog.Attr{
			slog.String("source", "frontend"),
			slog.String("clientTime", entry.clientTime),
			slog.String("clientIP", clientIP(r)),
			slog.String("userAgent", r.UserAgent()),
		}, entry.attrs...)

		s.logger.LogAttrs(r.Context(), entry.level, entry.message, attrs...)
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":   "accepted",
		"accepted": len(request.Logs),
	})
}

func (s *Server) handleCreateMeeting(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Title        string `json:"title"`
		Password     string `json:"password"`
		HostUserID   string `json:"hostUserId"`
		HostNickname string `json:"hostNickname"`
		DeviceType   string `json:"deviceType"`
	}

	if !decodeJSON(w, r, &request) {
		return
	}

	if request.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	currentUser, ok := s.requireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	meetingValue, host, err := s.meetings.CreateMeeting(r.Context(), meeting.CreateMeetingInput{
		Title:        request.Title,
		Password:     request.Password,
		HostUserID:   currentUser.ID,
		HostNickname: currentUser.Nickname,
		DeviceType:   request.DeviceType,
		IPAddress:    clientIP(r),
	})
	if err != nil {
		s.logger.Error("create meeting failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create meeting")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"meeting": meetingValue,
		"host":    host,
	})
}

func (s *Server) handleGetMeeting(w http.ResponseWriter, r *http.Request) {
	meetingID := r.PathValue("meetingID")

	meetingValue, found := s.meetings.GetMeeting(meetingID)
	if !found {
		writeError(w, http.StatusNotFound, "meeting not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"meeting": meetingValue,
	})
}

func (s *Server) handleGetMeetingMinutes(w http.ResponseWriter, r *http.Request) {
	meetingID := r.PathValue("meetingID")
	participantID := r.URL.Query().Get("participantId")
	if participantID == "" {
		writeError(w, http.StatusBadRequest, "participantId is required")
		return
	}

	meetingValue, found := s.meetings.GetMeeting(meetingID)
	if !found {
		writeError(w, http.StatusNotFound, "meeting not found")
		return
	}

	if _, ok := meetingValue.Participants[participantID]; !ok {
		writeError(w, http.StatusNotFound, "participant not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"meetingId":         meetingValue.ID,
		"title":             meetingValue.Title,
		"chatMessages":      meetingValue.ChatMessages,
		"whiteboardActions": meetingValue.WhiteboardActions,
		"temporaryMinutes":  meetingValue.TemporaryMinutes,
		"activeReadyCheck":  meetingValue.ActiveReadyCheck,
	})
}

func (s *Server) handleJoinMeeting(w http.ResponseWriter, r *http.Request) {
	meetingID := r.PathValue("meetingID")

	var request struct {
		Password                 string `json:"password"`
		UserID                   string `json:"userId"`
		Nickname                 string `json:"nickname"`
		DeviceType               string `json:"deviceType"`
		IsAnonymous              bool   `json:"isAnonymous"`
		RequestCameraEnabled     *bool  `json:"requestCameraEnabled"`
		RequestMicrophoneEnabled *bool  `json:"requestMicrophoneEnabled"`
	}

	if !decodeJSON(w, r, &request) {
		return
	}

	if strings.TrimSpace(request.Nickname) == "" {
		writeError(w, http.StatusBadRequest, "nickname is required")
		return
	}

	if request.UserID == "" && s.auth != nil {
		if currentUser, _, err := s.currentAuthenticatedUser(r); err == nil {
			request.UserID = currentUser.ID
			request.Nickname = strings.TrimSpace(request.Nickname)
			request.IsAnonymous = false
		}
	}

	meetingValue, participant, err := s.meetings.JoinMeeting(r.Context(), meeting.JoinMeetingInput{
		MeetingID:                meetingID,
		Password:                 request.Password,
		UserID:                   request.UserID,
		Nickname:                 strings.TrimSpace(request.Nickname),
		DeviceType:               request.DeviceType,
		IPAddress:                clientIP(r),
		IsAnonymous:              request.IsAnonymous,
		RequestCameraEnabled:     request.RequestCameraEnabled,
		RequestMicrophoneEnabled: request.RequestMicrophoneEnabled,
	})
	if err != nil {
		s.writeMeetingError(w, err)
		return
	}

	if s.signaling != nil {
		s.signaling.NotifyParticipantJoined(meetingID, participant)
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"meeting":     meetingValue,
		"participant": participant,
	})
}

func (s *Server) handleLeaveMeeting(w http.ResponseWriter, r *http.Request) {
	meetingID := r.PathValue("meetingID")
	participantID := r.PathValue("participantID")

	var request struct {
		DeviceType string `json:"deviceType"`
	}

	if !decodeOptionalJSON(r, &request) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.meetings.LeaveMeeting(r.Context(), meetingID, participantID, request.DeviceType, clientIP(r)); err != nil {
		s.writeMeetingError(w, err)
		return
	}

	if s.signaling != nil {
		s.signaling.NotifyParticipantLeft(meetingID, participantID)
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "left",
	})
}

func (s *Server) handleUpdateNickname(w http.ResponseWriter, r *http.Request) {
	meetingID := r.PathValue("meetingID")
	participantID := r.PathValue("participantID")

	var request struct {
		Nickname string `json:"nickname"`
	}

	if !decodeJSON(w, r, &request) {
		return
	}

	if strings.TrimSpace(request.Nickname) == "" {
		writeError(w, http.StatusBadRequest, "nickname is required")
		return
	}

	updatedParticipant, systemMessage, previousNickname, err := s.meetings.UpdateNickname(r.Context(), meeting.UpdateNicknameInput{
		MeetingID:     meetingID,
		ParticipantID: participantID,
		Nickname:      strings.TrimSpace(request.Nickname),
	})
	if err != nil {
		s.writeMeetingError(w, err)
		return
	}

	if s.signaling != nil && (systemMessage != nil || previousNickname != updatedParticipant.Nickname) {
		s.signaling.NotifyNicknameUpdated(meetingID, updatedParticipant, previousNickname, systemMessage)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"participant":      updatedParticipant,
		"previousNickname": previousNickname,
		"systemMessage":    systemMessage,
	})
}

func (s *Server) handleGrantCapability(w http.ResponseWriter, r *http.Request) {
	meetingID := r.PathValue("meetingID")
	participantID := r.PathValue("participantID")
	capabilityValue := meeting.Capability(r.PathValue("capability"))

	var request struct {
		HostParticipantID string `json:"hostParticipantId"`
	}

	if !decodeJSON(w, r, &request) {
		return
	}

	if request.HostParticipantID == "" {
		writeError(w, http.StatusBadRequest, "hostParticipantId is required")
		return
	}

	if err := s.meetings.GrantCapability(r.Context(), meeting.GrantCapabilityInput{
		MeetingID:     meetingID,
		HostID:        request.HostParticipantID,
		ParticipantID: participantID,
		Capability:    capabilityValue,
	}); err != nil {
		s.writeMeetingError(w, err)
		return
	}

	if s.signaling != nil {
		s.signaling.NotifyCapabilityGranted(meetingID, request.HostParticipantID, participantID, capabilityValue)
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":     "granted",
		"capability": string(capabilityValue),
	})
}

func (s *Server) handleAuditReport(w http.ResponseWriter, r *http.Request) {
	meetingID := r.PathValue("meetingID")
	participantID := r.PathValue("participantID")

	var request struct {
		UserID           string         `json:"userId"`
		ParticipantRole  meeting.Role   `json:"participantRole"`
		DeviceType       string         `json:"deviceType"`
		LatencyMS        int64          `json:"latencyMs"`
		PacketLossRate   float64        `json:"packetLossRate"`
		AverageFPS       float64        `json:"averageFps"`
		AverageBitrateKB int64          `json:"averageBitrateKbps"`
		Details          map[string]any `json:"details"`
	}

	if !decodeJSON(w, r, &request) {
		return
	}

	if err := s.meetings.RecordAuditReport(r.Context(), meeting.AuditReportInput{
		MeetingID:        meetingID,
		ParticipantID:    participantID,
		UserID:           request.UserID,
		ParticipantRole:  request.ParticipantRole,
		DeviceType:       request.DeviceType,
		IPAddress:        clientIP(r),
		LatencyMS:        request.LatencyMS,
		PacketLossRate:   request.PacketLossRate,
		AverageFPS:       request.AverageFPS,
		AverageBitrateKB: request.AverageBitrateKB,
		Details:          request.Details,
	}); err != nil {
		s.logger.Error("record audit report failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to record audit report")
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status": "accepted",
	})
}

func (s *Server) handleEndMeeting(w http.ResponseWriter, r *http.Request) {
	meetingID := r.PathValue("meetingID")

	var request struct {
		HostParticipantID string `json:"hostParticipantId"`
		DeviceType        string `json:"deviceType"`
	}

	if !decodeJSON(w, r, &request) {
		return
	}

	if request.HostParticipantID == "" {
		writeError(w, http.StatusBadRequest, "hostParticipantId is required")
		return
	}

	if err := s.meetings.EndMeeting(r.Context(), meetingID, request.HostParticipantID, request.DeviceType, clientIP(r)); err != nil {
		s.writeMeetingError(w, err)
		return
	}

	if s.signaling != nil {
		s.signaling.NotifyMeetingEnded(meetingID, request.HostParticipantID)
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ended",
	})
}

func (s *Server) handleSaveUserPreference(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")

	var request struct {
		DefaultCameraEnabled     bool `json:"defaultCameraEnabled"`
		DefaultMicrophoneEnabled bool `json:"defaultMicrophoneEnabled"`
	}

	if !decodeJSON(w, r, &request) {
		return
	}

	if userID == "" {
		writeError(w, http.StatusBadRequest, "userID is required")
		return
	}

	currentUser, ok := s.requireAuthenticatedUser(w, r)
	if !ok {
		return
	}
	if currentUser.ID != userID {
		writeError(w, http.StatusForbidden, "userID does not match current session")
		return
	}

	if err := s.meetings.SaveUserPreference(r.Context(), meeting.UpdatePreferenceInput{
		UserID:                   userID,
		DefaultCameraEnabled:     request.DefaultCameraEnabled,
		DefaultMicrophoneEnabled: request.DefaultMicrophoneEnabled,
	}); err != nil {
		s.logger.Error("save user preference failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save user preference")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "saved",
	})
}

func (s *Server) handleWebSocketPlaceholder(w http.ResponseWriter, r *http.Request) {
	meetingID := r.PathValue("meetingID")
	participantID := r.URL.Query().Get("participantId")

	if s.signaling == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{
			"error":     "websocket signaling is not available",
			"meetingId": meetingID,
		})
		return
	}

	if err := s.signaling.ServeWS(w, r, meetingID, participantID); err != nil {
		s.writeMeetingError(w, err)
	}
}

func (s *Server) writeMeetingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, meeting.ErrMeetingNotFound), errors.Is(err, meeting.ErrParticipantNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, meeting.ErrMeetingPassword):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, meeting.ErrUnauthorized):
		writeError(w, http.StatusForbidden, err.Error())
	default:
		s.logger.Error("meeting operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "meeting operation failed")
	}
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	return r.RemoteAddr
}

func withJSONHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isWebSocketRequest(r) {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Request-Received-At", time.Now().UTC().Format(time.RFC3339Nano))
		next.ServeHTTP(w, r)
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func decodeOptionalJSON(r *http.Request, target any) bool {
	if r.Body == nil {
		return true
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, context.Canceled) {
			return false
		}
		if err.Error() == "EOF" {
			return true
		}
		return false
	}

	return true
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, statusCode int, message string) {
	writeJSON(w, statusCode, map[string]string{
		"error": message,
	})
}

func isWebSocketRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

type normalizedClientLogEntry struct {
	level      slog.Level
	message    string
	clientTime string
	attrs      []slog.Attr
}

func normalizeClientLogEntry(raw map[string]any) (normalizedClientLogEntry, error) {
	levelValue, ok := raw["level"].(string)
	if !ok {
		return normalizedClientLogEntry{}, errors.New("level is required")
	}

	level, err := parseClientLogLevel(levelValue)
	if err != nil {
		return normalizedClientLogEntry{}, err
	}

	message, ok := raw["message"].(string)
	if !ok || strings.TrimSpace(message) == "" {
		return normalizedClientLogEntry{}, errors.New("message is required")
	}

	clientTime, ok := raw["time"].(string)
	if !ok || clientTime == "" {
		return normalizedClientLogEntry{}, errors.New("time is required")
	}

	if _, err := time.Parse(time.RFC3339Nano, clientTime); err != nil {
		return normalizedClientLogEntry{}, fmt.Errorf("invalid time: %w", err)
	}

	attrs := make([]slog.Attr, 0, len(raw))
	keys := make([]string, 0, len(raw))
	for key := range raw {
		switch key {
		case "level", "message", "time":
			continue
		default:
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	for _, key := range keys {
		attrs = append(attrs, slog.Any(key, raw[key]))
	}

	return normalizedClientLogEntry{
		level:      level,
		message:    strings.TrimSpace(message),
		clientTime: clientTime,
		attrs:      attrs,
	}, nil
}

func parseClientLogLevel(value string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unsupported level %q", value)
	}
}
