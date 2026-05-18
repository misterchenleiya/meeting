package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/misterchenleiya/meeting/internal/auth"
	"github.com/misterchenleiya/meeting/internal/meeting"
	"github.com/misterchenleiya/meeting/internal/minutes"
	"github.com/misterchenleiya/meeting/internal/signaling"
	"github.com/misterchenleiya/meeting/internal/storage/sqlite"
	"github.com/misterchenleiya/meeting/internal/turnauth"
)

type Server struct {
	logger    *slog.Logger
	auth      *auth.Service
	meetings  *meeting.Service
	minutes   *minutes.Service
	store     *sqlite.Store
	signaling *signaling.Hub
	turn      *turnauth.Service
	mux       *http.ServeMux
}

func NewServer(
	logger *slog.Logger,
	authService *auth.Service,
	meetings *meeting.Service,
	minutesService *minutes.Service,
	store *sqlite.Store,
	signalingHub *signaling.Hub,
	turnService *turnauth.Service,
) *Server {
	server := &Server{
		logger:    logger,
		auth:      authService,
		meetings:  meetings,
		minutes:   minutesService,
		store:     store,
		signaling: signalingHub,
		turn:      turnService,
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
	s.mux.HandleFunc("GET /api/meetings/{meetingNumber}", s.handleGetMeeting)
	s.mux.HandleFunc("GET /api/meetings/{meetingNumber}/minutes", s.handleGetMeetingMinutes)
	s.mux.HandleFunc("POST /api/meetings/{meetingNumber}/transcription/start", s.handleStartTranscription)
	s.mux.HandleFunc("GET /api/meetings/{meetingNumber}/transcript", s.handleGetTranscript)
	s.mux.HandleFunc("POST /api/meetings/{meetingNumber}/participants/{participantID}/transcription/chunks", s.handleUploadTranscriptionChunk)
	s.mux.HandleFunc("POST /api/meetings/{meetingNumber}/minutes/jobs", s.handleCreateMinutesJob)
	s.mux.HandleFunc("GET /api/meetings/{meetingNumber}/minutes/jobs/{jobID}", s.handleGetMinutesJob)
	s.mux.HandleFunc("POST /api/meetings/{meetingNumber}/join", s.handleJoinMeeting)
	s.mux.HandleFunc("POST /api/meetings/{meetingNumber}/participants/{participantID}/ice-servers", s.handleGetICEServers)
	s.mux.HandleFunc("POST /api/meetings/{meetingNumber}/participants/{participantID}/leave", s.handleLeaveMeeting)
	s.mux.HandleFunc("POST /api/meetings/{meetingNumber}/participants/{participantID}/nickname", s.handleUpdateNickname)
	s.mux.HandleFunc("POST /api/meetings/{meetingNumber}/participants/{participantID}/capabilities/{capability}/grant", s.handleGrantCapability)
	s.mux.HandleFunc("POST /api/meetings/{meetingNumber}/participants/{participantID}/audit", s.handleAuditReport)
	s.mux.HandleFunc("POST /api/meetings/{meetingNumber}/end", s.handleEndMeeting)
	s.mux.HandleFunc("GET /api/users/me/meeting-history", s.handleMyMeetingHistory)
	s.mux.HandleFunc("GET /api/meeting-minutes/{minutesID}", s.handleGetPersistentMinutes)
	s.mux.HandleFunc("POST /api/meeting-minutes/{minutesID}/shares", s.handleShareMinutes)
	s.mux.HandleFunc("DELETE /api/meeting-minutes/{minutesID}/shares/{userID}", s.handleRevokeMinutesShare)
	s.mux.HandleFunc("PUT /api/users/{userID}/preferences", s.handleSaveUserPreference)
	s.mux.HandleFunc("GET /ws/meetings/{meetingNumber}", s.handleWebSocketPlaceholder)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "meeting-api",
	})
}

func meetingIdentifierFromPath(r *http.Request) string {
	if value := r.PathValue("meetingNumber"); value != "" {
		return value
	}
	return r.PathValue("meetingID")
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
		Title         string            `json:"title"`
		Password      string            `json:"password"`
		MeetingType   string            `json:"meetingType"`
		HostUserID    string            `json:"hostUserId"`
		HostNickname  string            `json:"hostNickname"`
		DeviceType    string            `json:"deviceType"`
		ClientProfile map[string]string `json:"clientProfile"`
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
		Title:         request.Title,
		Password:      request.Password,
		MeetingType:   meeting.MeetingType(request.MeetingType),
		HostUserID:    currentUser.ID,
		HostEmail:     currentUser.Email,
		HostNickname:  currentUser.Nickname,
		DeviceType:    request.DeviceType,
		ClientProfile: request.ClientProfile,
		IPAddress:     clientIP(r),
	})
	if err != nil {
		s.logger.Error("create meeting failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create meeting")
		return
	}

	iceServers, expiresAt, err := s.buildICEBundle(host.ID)
	if err != nil {
		s.logger.Error("build create meeting ice servers failed", "error", err, "meetingId", meetingValue.ID, "meetingNumber", meetingValue.MeetingNumber, "participantId", host.ID)
		writeError(w, http.StatusInternalServerError, "failed to build meeting ice servers")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"meeting":                meetingValue,
		"host":                   host,
		"iceServers":             iceServers,
		"iceCredentialExpiresAt": formatOptionalTimestamp(expiresAt),
	})
}

func (s *Server) handleGetMeeting(w http.ResponseWriter, r *http.Request) {
	meetingIdentifier := meetingIdentifierFromPath(r)

	meetingValue, found := s.meetings.GetMeeting(meetingIdentifier)
	if !found {
		writeError(w, http.StatusNotFound, "meeting not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"meeting": meetingValue,
	})
}

func (s *Server) handleGetMeetingMinutes(w http.ResponseWriter, r *http.Request) {
	meetingIdentifier := meetingIdentifierFromPath(r)
	participantID := r.URL.Query().Get("participantId")
	if participantID == "" {
		writeError(w, http.StatusBadRequest, "participantId is required")
		return
	}

	meetingValue, found := s.meetings.GetMeeting(meetingIdentifier)
	if !found {
		writeError(w, http.StatusNotFound, "meeting not found")
		return
	}

	if _, ok := meetingValue.Participants[participantID]; !ok {
		writeError(w, http.StatusNotFound, "participant not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"meetingNumber":     meetingValue.MeetingNumber,
		"title":             meetingValue.Title,
		"chatMessages":      meetingValue.ChatMessages,
		"whiteboardActions": meetingValue.WhiteboardActions,
		"temporaryMinutes":  meetingValue.TemporaryMinutes,
		"activeReadyCheck":  meetingValue.ActiveReadyCheck,
	})
}

func (s *Server) handleStartTranscription(w http.ResponseWriter, r *http.Request) {
	if s.minutes == nil {
		writeError(w, http.StatusNotImplemented, "minutes service is not available")
		return
	}

	meetingIdentifier := meetingIdentifierFromPath(r)
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

	meetingValue, found := s.meetings.GetMeeting(meetingIdentifier)
	if !found {
		writeError(w, http.StatusNotFound, "meeting not found")
		return
	}
	participant, ok := meetingValue.Participants[request.HostParticipantID]
	if !ok {
		writeError(w, http.StatusNotFound, "participant not found")
		return
	}
	if participant.UserID != "" {
		currentUser, _, err := s.currentAuthenticatedUser(r)
		if err != nil {
			s.writeAuthError(w, err)
			return
		}
		if currentUser.ID != participant.UserID {
			writeError(w, http.StatusForbidden, "participant does not match current session")
			return
		}
	}

	updatedMeeting, err := s.meetings.StartTranscription(r.Context(), meeting.StartTranscriptionInput{
		MeetingID:     meetingIdentifier,
		ParticipantID: request.HostParticipantID,
	})
	if err != nil {
		s.writeMeetingError(w, err)
		return
	}

	if s.signaling != nil {
		s.signaling.NotifyTranscriptionStatus(updatedMeeting.ID, updatedMeeting.Transcription)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "started",
		"transcription": updatedMeeting.Transcription,
	})
}

func (s *Server) handleGetTranscript(w http.ResponseWriter, r *http.Request) {
	if s.minutes == nil {
		writeError(w, http.StatusNotImplemented, "minutes service is not available")
		return
	}

	meetingIdentifier := meetingIdentifierFromPath(r)
	participantID := r.URL.Query().Get("participantId")
	if participantID == "" {
		writeError(w, http.StatusBadRequest, "participantId is required")
		return
	}

	meetingValue, found := s.meetings.GetMeeting(meetingIdentifier)
	if !found {
		writeError(w, http.StatusNotFound, "meeting not found")
		return
	}
	if _, ok := meetingValue.Participants[participantID]; !ok {
		writeError(w, http.StatusNotFound, "participant not found")
		return
	}

	segments, err := s.minutes.ListTranscriptSegments(r.Context(), meetingValue.ID)
	if err != nil {
		s.logger.Error("list transcript failed", "error", err, "meetingId", meetingValue.ID)
		writeError(w, http.StatusInternalServerError, "failed to list transcript")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"meetingNumber": meetingValue.MeetingNumber,
		"segments":      transcriptSegmentResponses(segments),
	})
}

func (s *Server) handleUploadTranscriptionChunk(w http.ResponseWriter, r *http.Request) {
	if s.minutes == nil {
		writeError(w, http.StatusNotImplemented, "minutes service is not available")
		return
	}

	meetingIdentifier := meetingIdentifierFromPath(r)
	participantID := r.PathValue("participantID")
	meetingValue, found := s.meetings.GetMeeting(meetingIdentifier)
	if !found {
		writeError(w, http.StatusNotFound, "meeting not found")
		return
	}
	participant, ok := meetingValue.Participants[participantID]
	if !ok {
		writeError(w, http.StatusNotFound, "participant not found")
		return
	}
	if !meetingValue.Transcription.Enabled {
		writeError(w, http.StatusConflict, "transcription is not enabled")
		return
	}
	if participant.UserID != "" {
		currentUser, _, err := s.currentAuthenticatedUser(r)
		if err != nil {
			s.writeAuthError(w, err)
			return
		}
		if currentUser.ID != participant.UserID {
			writeError(w, http.StatusForbidden, "participant does not match current session")
			return
		}
	}

	chunk, err := s.decodeTranscriptionChunk(w, r, meetingValue, participant)
	if err != nil {
		s.writeMinutesError(w, err)
		return
	}

	segment, err := s.minutes.ProcessAudioChunk(r.Context(), chunk)
	if err != nil {
		s.writeMinutesError(w, err)
		return
	}
	if segment != nil && s.signaling != nil {
		s.signaling.NotifyTranscriptionSegment(meetingValue.ID, *segment)
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":  "accepted",
		"segment": transcriptSegmentResponse(segment),
	})
}

func (s *Server) handleCreateMinutesJob(w http.ResponseWriter, r *http.Request) {
	if s.minutes == nil {
		writeError(w, http.StatusNotImplemented, "minutes service is not available")
		return
	}

	meetingIdentifier := meetingIdentifierFromPath(r)
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

	meetingValue, found := s.meetings.GetMeeting(meetingIdentifier)
	if !found {
		writeError(w, http.StatusNotFound, "meeting not found")
		return
	}
	host, ok := meetingValue.Participants[request.HostParticipantID]
	if !ok {
		writeError(w, http.StatusNotFound, "participant not found")
		return
	}
	if host.Role != meeting.RoleHost {
		writeError(w, http.StatusForbidden, "only host can create minutes job")
		return
	}
	currentUser, ok := s.requireAuthenticatedUser(w, r)
	if !ok {
		return
	}
	if host.UserID != currentUser.ID {
		writeError(w, http.StatusForbidden, "host participant does not match current session")
		return
	}

	job, created, err := s.minutes.CreateMinutesJob(r.Context(), minutes.CreateJobInput{
		MeetingID:                meetingValue.ID,
		MeetingNumber:            meetingValue.MeetingNumber,
		RequestedByUserID:        currentUser.ID,
		RequestedByParticipantID: host.ID,
	})
	if err != nil {
		s.writeMinutesError(w, err)
		return
	}
	if created && s.signaling != nil {
		s.signaling.NotifyMinutesJobCreated(meetingValue.ID, job)
	}

	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{
		"job":     minutesJobResponse(job),
		"created": created,
	})
}

func (s *Server) handleGetMinutesJob(w http.ResponseWriter, r *http.Request) {
	if s.minutes == nil {
		writeError(w, http.StatusNotImplemented, "minutes service is not available")
		return
	}
	currentUser, ok := s.requireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	jobID := r.PathValue("jobID")
	job, found, err := s.minutes.GetMinutesJob(r.Context(), jobID)
	if err != nil {
		s.writeMinutesError(w, err)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "minutes job not found")
		return
	}
	if job.RequestedByUserID != currentUser.ID {
		writeError(w, http.StatusForbidden, "minutes job does not match current session")
		return
	}

	payload := map[string]any{
		"job": minutesJobResponse(job),
	}
	if record, minutesFound, getErr := s.store.GetMeetingMinutesByJob(r.Context(), job.ID); getErr != nil {
		s.writeMinutesError(w, getErr)
		return
	} else if minutesFound {
		payload["minutes"] = meetingMinutesResponse(record)
	}

	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleJoinMeeting(w http.ResponseWriter, r *http.Request) {
	meetingIdentifier := meetingIdentifierFromPath(r)

	var request struct {
		Password                 string            `json:"password"`
		UserID                   string            `json:"userId"`
		Nickname                 string            `json:"nickname"`
		DeviceType               string            `json:"deviceType"`
		ClientProfile            map[string]string `json:"clientProfile"`
		IsAnonymous              bool              `json:"isAnonymous"`
		RequestCameraEnabled     *bool             `json:"requestCameraEnabled"`
		RequestMicrophoneEnabled *bool             `json:"requestMicrophoneEnabled"`
	}

	if !decodeJSON(w, r, &request) {
		return
	}

	if strings.TrimSpace(request.Nickname) == "" {
		writeError(w, http.StatusBadRequest, "nickname is required")
		return
	}

	if s.auth != nil {
		currentUser, _, err := s.currentAuthenticatedUser(r)
		switch {
		case err == nil:
			request.UserID = currentUser.ID
			request.Nickname = strings.TrimSpace(request.Nickname)
			request.IsAnonymous = false
		case strings.TrimSpace(request.UserID) != "":
			s.writeAuthError(w, err)
			return
		}
	}

	replacedParticipantIDs := s.activeRegisteredParticipantIDs(meetingIdentifier, request.UserID, request.IsAnonymous)
	meetingValue, participant, err := s.meetings.JoinMeeting(r.Context(), meeting.JoinMeetingInput{
		MeetingID:                meetingIdentifier,
		Password:                 request.Password,
		UserID:                   request.UserID,
		Nickname:                 strings.TrimSpace(request.Nickname),
		DeviceType:               request.DeviceType,
		ClientProfile:            request.ClientProfile,
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
		for _, replacedParticipantID := range replacedParticipantIDs {
			if replacedParticipantID == participant.ID {
				continue
			}
			s.signaling.NotifyParticipantLeft(meetingValue.ID, replacedParticipantID)
			s.signaling.DisconnectParticipant(meetingValue.ID, replacedParticipantID, "single_account_replaced")
		}
		s.signaling.NotifyParticipantJoined(meetingValue.ID, participant)
	}

	iceServers, expiresAt, err := s.buildICEBundle(participant.ID)
	if err != nil {
		s.logger.Error("build join meeting ice servers failed", "error", err, "meetingId", meetingValue.ID, "meetingNumber", meetingValue.MeetingNumber, "participantId", participant.ID)
		writeError(w, http.StatusInternalServerError, "failed to build meeting ice servers")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"meeting":                meetingValue,
		"participant":            participant,
		"iceServers":             iceServers,
		"iceCredentialExpiresAt": formatOptionalTimestamp(expiresAt),
	})
}

func (s *Server) activeRegisteredParticipantIDs(meetingIdentifier string, userID string, isAnonymous bool) []string {
	if strings.TrimSpace(userID) == "" || isAnonymous {
		return nil
	}

	meetingValue, found := s.meetings.GetMeeting(meetingIdentifier)
	if !found {
		return nil
	}

	participantIDs := make([]string, 0, 1)
	for participantID, participant := range meetingValue.Participants {
		if participant.UserID != userID || participant.LeftAt != nil || participant.Role == meeting.RoleHost {
			continue
		}
		participantIDs = append(participantIDs, participantID)
	}

	sort.Strings(participantIDs)
	return participantIDs
}

func (s *Server) handleLeaveMeeting(w http.ResponseWriter, r *http.Request) {
	meetingIdentifier := meetingIdentifierFromPath(r)
	participantID := r.PathValue("participantID")

	var request struct {
		DeviceType string `json:"deviceType"`
	}

	if !decodeOptionalJSON(r, &request) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	meetingValue, found := s.meetings.GetMeeting(meetingIdentifier)
	if !found {
		writeError(w, http.StatusNotFound, "meeting not found")
		return
	}

	if err := s.meetings.LeaveMeeting(r.Context(), meetingIdentifier, participantID, request.DeviceType, clientIP(r)); err != nil {
		s.writeMeetingError(w, err)
		return
	}

	if s.signaling != nil {
		s.signaling.NotifyParticipantLeft(meetingValue.ID, participantID)
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "left",
	})
}

func (s *Server) handleUpdateNickname(w http.ResponseWriter, r *http.Request) {
	meetingIdentifier := meetingIdentifierFromPath(r)
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

	meetingValue, found := s.meetings.GetMeeting(meetingIdentifier)
	if !found {
		writeError(w, http.StatusNotFound, "meeting not found")
		return
	}

	updatedParticipant, systemMessage, previousNickname, err := s.meetings.UpdateNickname(r.Context(), meeting.UpdateNicknameInput{
		MeetingID:     meetingIdentifier,
		ParticipantID: participantID,
		Nickname:      strings.TrimSpace(request.Nickname),
	})
	if err != nil {
		s.writeMeetingError(w, err)
		return
	}

	if s.signaling != nil && (systemMessage != nil || previousNickname != updatedParticipant.Nickname) {
		s.signaling.NotifyNicknameUpdated(meetingValue.ID, updatedParticipant, previousNickname, systemMessage)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"participant":      updatedParticipant,
		"previousNickname": previousNickname,
		"systemMessage":    systemMessage,
	})
}

func (s *Server) handleGrantCapability(w http.ResponseWriter, r *http.Request) {
	meetingIdentifier := meetingIdentifierFromPath(r)
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

	meetingValue, found := s.meetings.GetMeeting(meetingIdentifier)
	if !found {
		writeError(w, http.StatusNotFound, "meeting not found")
		return
	}

	if err := s.meetings.GrantCapability(r.Context(), meeting.GrantCapabilityInput{
		MeetingID:     meetingIdentifier,
		HostID:        request.HostParticipantID,
		ParticipantID: participantID,
		Capability:    capabilityValue,
	}); err != nil {
		s.writeMeetingError(w, err)
		return
	}

	if s.signaling != nil {
		s.signaling.NotifyCapabilityGranted(meetingValue.ID, request.HostParticipantID, participantID, capabilityValue)
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":     "granted",
		"capability": string(capabilityValue),
	})
}

func (s *Server) handleAuditReport(w http.ResponseWriter, r *http.Request) {
	meetingIdentifier := meetingIdentifierFromPath(r)
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
		MeetingID:        meetingIdentifier,
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
	meetingIdentifier := meetingIdentifierFromPath(r)

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

	meetingValue, found := s.meetings.GetMeeting(meetingIdentifier)
	if !found {
		writeError(w, http.StatusNotFound, "meeting not found")
		return
	}

	if err := s.meetings.EndMeeting(r.Context(), meetingIdentifier, request.HostParticipantID, request.DeviceType, clientIP(r)); err != nil {
		s.writeMeetingError(w, err)
		return
	}

	if s.signaling != nil {
		s.signaling.NotifyMeetingEnded(meetingValue.ID, request.HostParticipantID)
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ended",
	})
}

func (s *Server) handleMyMeetingHistory(w http.ResponseWriter, r *http.Request) {
	if s.minutes == nil {
		writeError(w, http.StatusNotImplemented, "minutes service is not available")
		return
	}
	currentUser, ok := s.requireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	records, err := s.minutes.ListUserMeetingHistory(r.Context(), currentUser.ID)
	if err != nil {
		s.writeMinutesError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"records": meetingHistoryResponses(records),
	})
}

func (s *Server) handleGetPersistentMinutes(w http.ResponseWriter, r *http.Request) {
	if s.minutes == nil {
		writeError(w, http.StatusNotImplemented, "minutes service is not available")
		return
	}
	currentUser, ok := s.requireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	record, found, err := s.minutes.GetMeetingMinutes(r.Context(), r.PathValue("minutesID"), currentUser.ID)
	if err != nil {
		s.writeMinutesError(w, err)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "meeting minutes not found")
		return
	}

	participants, err := s.store.ListRegisteredParticipantsForMeeting(r.Context(), record.MeetingID)
	if err != nil {
		s.writeMinutesError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"minutes":      meetingMinutesResponse(record),
		"participants": participantUsageResponses(participants),
	})
}

func (s *Server) handleShareMinutes(w http.ResponseWriter, r *http.Request) {
	if s.minutes == nil {
		writeError(w, http.StatusNotImplemented, "minutes service is not available")
		return
	}
	currentUser, ok := s.requireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	var request struct {
		UserID string `json:"userId"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if strings.TrimSpace(request.UserID) == "" {
		writeError(w, http.StatusBadRequest, "userId is required")
		return
	}

	if err := s.minutes.ShareMeetingMinutes(r.Context(), r.PathValue("minutesID"), currentUser.ID, request.UserID); err != nil {
		s.writeMinutesError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "shared",
	})
}

func (s *Server) handleRevokeMinutesShare(w http.ResponseWriter, r *http.Request) {
	if s.minutes == nil {
		writeError(w, http.StatusNotImplemented, "minutes service is not available")
		return
	}
	currentUser, ok := s.requireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	if err := s.minutes.RevokeMeetingMinutesShare(r.Context(), r.PathValue("minutesID"), currentUser.ID, r.PathValue("userID")); err != nil {
		s.writeMinutesError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "revoked",
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
	meetingIdentifier := meetingIdentifierFromPath(r)
	participantID := r.URL.Query().Get("participantId")

	if s.signaling == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{
			"error":         "websocket signaling is not available",
			"meetingNumber": meetingIdentifier,
		})
		return
	}

	if s.auth != nil {
		meetingValue, found := s.meetings.GetMeeting(meetingIdentifier)
		if !found {
			writeError(w, http.StatusNotFound, "meeting not found")
			return
		}
		participant, ok := meetingValue.Participants[participantID]
		if !ok {
			writeError(w, http.StatusNotFound, "participant not found")
			return
		}
		if participant.UserID != "" {
			currentUser, _, err := s.currentAuthenticatedUser(r)
			if err != nil {
				s.writeAuthError(w, err)
				return
			}
			if currentUser.ID != participant.UserID {
				writeError(w, http.StatusForbidden, "participant does not match current session")
				return
			}
		}
	}

	if err := s.signaling.ServeWS(w, r, meetingIdentifier, participantID); err != nil {
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

func (s *Server) writeMinutesError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, minutes.ErrDisabled):
		writeError(w, http.StatusServiceUnavailable, "transcription is disabled")
	case errors.Is(err, minutes.ErrInvalidAudioChunk):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, minutes.ErrQuotaExceeded):
		writeError(w, http.StatusTooManyRequests, "AI 助理额度已用完")
	case errors.Is(err, minutes.ErrUnauthorized):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, minutes.ErrMinutesNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, minutes.ErrNoTranscript):
		writeError(w, http.StatusConflict, "meeting transcript is empty")
	default:
		s.logger.Error("minutes operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "minutes operation failed")
	}
}

func (s *Server) decodeTranscriptionChunk(w http.ResponseWriter, r *http.Request, meetingValue *meeting.Meeting, participant *meeting.Participant) (minutes.ProcessAudioChunkInput, error) {
	r.Body = http.MaxBytesReader(w, r.Body, s.minutes.MaxChunkBytes()+(32<<10))
	reader, err := r.MultipartReader()
	if err != nil {
		return minutes.ProcessAudioChunkInput{}, minutes.ErrInvalidAudioChunk
	}

	fields := map[string]string{}
	var audioData []byte
	mimeType := ""
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return minutes.ProcessAudioChunkInput{}, fmt.Errorf("read multipart chunk: %w", err)
		}
		name := part.FormName()
		if name == "audio" {
			data, readErr := io.ReadAll(io.LimitReader(part, s.minutes.MaxChunkBytes()+1))
			if readErr != nil {
				return minutes.ProcessAudioChunkInput{}, fmt.Errorf("read audio chunk: %w", readErr)
			}
			if int64(len(data)) > s.minutes.MaxChunkBytes() {
				return minutes.ProcessAudioChunkInput{}, minutes.ErrInvalidAudioChunk
			}
			audioData = data
			mimeType = part.Header.Get("Content-Type")
			continue
		}

		value, readErr := io.ReadAll(io.LimitReader(part, 4096))
		if readErr != nil {
			return minutes.ProcessAudioChunkInput{}, fmt.Errorf("read multipart field: %w", readErr)
		}
		fields[name] = strings.TrimSpace(string(value))
	}

	if len(audioData) == 0 {
		return minutes.ProcessAudioChunkInput{}, minutes.ErrInvalidAudioChunk
	}

	sequence, err := parseOptionalInt64(fields["sequence"], time.Now().UnixNano())
	if err != nil {
		return minutes.ProcessAudioChunkInput{}, minutes.ErrInvalidAudioChunk
	}
	startedAt, err := parseOptionalRequestTime(fields["startedAt"])
	if err != nil {
		return minutes.ProcessAudioChunkInput{}, minutes.ErrInvalidAudioChunk
	}
	endedAt, err := parseOptionalRequestTime(fields["endedAt"])
	if err != nil {
		return minutes.ProcessAudioChunkInput{}, minutes.ErrInvalidAudioChunk
	}
	if startedAt.IsZero() {
		startedAt = time.Now().UTC().Add(-1 * time.Second)
	}
	if endedAt.IsZero() || !endedAt.After(startedAt) {
		endedAt = startedAt.Add(time.Second)
	}
	sampleRate, err := parseOptionalInt(fields["sampleRate"], 16000)
	if err != nil {
		return minutes.ProcessAudioChunkInput{}, minutes.ErrInvalidAudioChunk
	}
	if mimeType == "" {
		mimeType = fields["mimeType"]
	}

	return minutes.ProcessAudioChunkInput{
		MeetingID:     meetingValue.ID,
		MeetingNumber: meetingValue.MeetingNumber,
		ParticipantID: participant.ID,
		UserID:        participant.UserID,
		Nickname:      participant.Nickname,
		Language:      fields["language"],
		Sequence:      sequence,
		StartedAt:     startedAt,
		EndedAt:       endedAt,
		MimeType:      mimeType,
		SampleRate:    sampleRate,
		Data:          audioData,
	}, nil
}

func parseOptionalRequestTime(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC(), nil
	}
	millis, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(millis).UTC(), nil
}

func parseOptionalInt64(value string, fallback int64) (int64, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	return strconv.ParseInt(value, 10, 64)
}

func parseOptionalInt(value string, fallback int) (int, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	return strconv.Atoi(value)
}

func transcriptSegmentResponses(records []sqlite.TranscriptSegmentRecord) []map[string]any {
	responses := make([]map[string]any, 0, len(records))
	for index := range records {
		responses = append(responses, transcriptSegmentResponse(&records[index]))
	}
	return responses
}

func transcriptSegmentResponse(record *sqlite.TranscriptSegmentRecord) map[string]any {
	if record == nil {
		return nil
	}
	return map[string]any{
		"id":            record.ID,
		"meetingNumber": record.MeetingNumber,
		"participantId": record.ParticipantID,
		"userId":        record.UserID,
		"nickname":      record.Nickname,
		"language":      record.Language,
		"sequence":      record.Sequence,
		"startedAt":     record.StartedAt.UTC().Format(time.RFC3339Nano),
		"endedAt":       record.EndedAt.UTC().Format(time.RFC3339Nano),
		"text":          record.Text,
		"isFinal":       record.IsFinal,
		"asrProvider":   record.ASRProvider,
		"createdAt":     record.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func minutesJobResponse(record sqlite.MinutesJobRecord) map[string]any {
	return map[string]any{
		"id":                       record.ID,
		"meetingNumber":            record.MeetingNumber,
		"requestedByUserId":        record.RequestedByUserID,
		"requestedByParticipantId": record.RequestedByParticipantID,
		"status":                   record.Status,
		"errorMessage":             record.ErrorMessage,
		"emailError":               record.EmailError,
		"startedAt":                formatOptionalTimePointer(record.StartedAt),
		"completedAt":              formatOptionalTimePointer(record.CompletedAt),
		"createdAt":                record.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updatedAt":                record.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func meetingMinutesResponse(record sqlite.MeetingMinutesRecord) map[string]any {
	return map[string]any{
		"id":              record.ID,
		"jobId":           record.JobID,
		"meetingNumber":   record.MeetingNumber,
		"hostUserId":      record.HostUserID,
		"title":           record.Title,
		"summary":         record.Summary,
		"markdownContent": record.MarkdownContent,
		"outlineJson":     record.OutlineJSON,
		"llmProvider":     record.LLMProvider,
		"llmModel":        record.LLMModel,
		"generatedAt":     record.GeneratedAt.UTC().Format(time.RFC3339Nano),
	}
}

func meetingHistoryResponses(records []sqlite.UserMeetingHistoryRecord) []map[string]any {
	responses := make([]map[string]any, 0, len(records))
	for _, record := range records {
		responses = append(responses, map[string]any{
			"meetingId":       record.MeetingID,
			"meetingNumber":   record.MeetingNumber,
			"title":           record.Title,
			"meetingType":     record.MeetingType,
			"hostUserId":      record.HostUserID,
			"hostNickname":    record.HostNickname,
			"userRole":        record.UserRole,
			"joinedAt":        record.JoinedAt.UTC().Format(time.RFC3339Nano),
			"leftAt":          formatOptionalTimePointer(record.LeftAt),
			"createdAt":       record.CreatedAt.UTC().Format(time.RFC3339Nano),
			"endedAt":         formatOptionalTimePointer(record.EndedAt),
			"minutesId":       record.MinutesID,
			"minutesStatus":   record.MinutesStatus,
			"minutesShared":   record.MinutesShared,
			"minutesSharedAt": formatOptionalTimePointer(record.MinutesSharedAt),
			"generatedAt":     formatOptionalTimePointer(record.GeneratedAt),
		})
	}
	return responses
}

func participantUsageResponses(records []sqlite.MeetingParticipantUsageRecord) []map[string]any {
	responses := make([]map[string]any, 0, len(records))
	for _, record := range records {
		responses = append(responses, map[string]any{
			"participantId": record.ParticipantID,
			"userId":        record.UserID,
			"email":         record.Email,
			"nickname":      record.Nickname,
			"role":          record.ParticipantRole,
			"joinedAt":      record.JoinedAt.UTC().Format(time.RFC3339Nano),
			"leftAt":        formatOptionalTimePointer(record.LeftAt),
		})
	}
	return responses
}

func formatOptionalTimePointer(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
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

func formatOptionalTimestamp(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
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
