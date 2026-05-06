package meeting

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/misterchenleiya/meeting/internal/storage/sqlite"
)

var (
	ErrMeetingNotFound     = errors.New("meeting not found")
	ErrMeetingPassword     = errors.New("invalid meeting password")
	ErrParticipantNotFound = errors.New("participant not found")
	ErrUnauthorized        = errors.New("unauthorized operation")
)

type PreferenceStore interface {
	GetUserByID(ctx context.Context, userID string) (sqlite.UserRecord, bool, error)
	GetUserPreference(ctx context.Context, userID string) (sqlite.UserPreference, bool, error)
	UpsertUserPreference(ctx context.Context, pref sqlite.UserPreference) error
	InsertAuditEvent(ctx context.Context, event sqlite.AuditEvent) error
	UpsertMeetingUsage(ctx context.Context, record sqlite.MeetingUsageRecord) error
	UpdateMeetingUsageEndedAt(ctx context.Context, meetingID string, endedAt time.Time, updatedAt time.Time) error
	UpsertMeetingParticipantUsage(ctx context.Context, record sqlite.MeetingParticipantUsageRecord) error
	UpdateMeetingParticipantUsageLeftAt(ctx context.Context, meetingID string, participantID string, leftAt time.Time, updatedAt time.Time) error
	UpdateMeetingParticipantUsageNickname(ctx context.Context, meetingID string, participantID string, nickname string, updatedAt time.Time) error
	UpdateMeetingParticipantUsageRole(ctx context.Context, meetingID string, participantID string, role string, updatedAt time.Time) error
}

type Service struct {
	logger   *slog.Logger
	store    PreferenceStore
	mu       sync.RWMutex
	meetings map[string]*Meeting
	numbers  map[string]string
}

type CreateMeetingInput struct {
	Title         string
	Password      string
	MeetingType   MeetingType
	HostUserID    string
	HostEmail     string
	HostNickname  string
	DeviceType    string
	ClientProfile map[string]string
	IPAddress     string
}

type JoinMeetingInput struct {
	MeetingID                string
	Password                 string
	UserID                   string
	Email                    string
	Nickname                 string
	DeviceType               string
	ClientProfile            map[string]string
	IPAddress                string
	IsAnonymous              bool
	RequestCameraEnabled     *bool
	RequestMicrophoneEnabled *bool
}

type GrantCapabilityInput struct {
	MeetingID     string
	HostID        string
	ParticipantID string
	Capability    Capability
}

type CapabilityRequestInput struct {
	MeetingID     string
	ParticipantID string
	Capability    Capability
}

type AssignAssistantInput struct {
	MeetingID     string
	HostID        string
	ParticipantID string
}

type UpdateNicknameInput struct {
	MeetingID     string
	ParticipantID string
	Nickname      string
}

type ChatMessageInput struct {
	MeetingID     string
	ParticipantID string
	Message       string
}

type WhiteboardActionInput struct {
	MeetingID     string
	ParticipantID string
	Action        WhiteboardAction
}

type ClearWhiteboardInput struct {
	MeetingID     string
	ParticipantID string
}

type StartReadyCheckInput struct {
	MeetingID      string
	ParticipantID  string
	TimeoutSeconds int
}

type RespondReadyCheckInput struct {
	MeetingID     string
	ParticipantID string
	Status        ReadyCheckStatus
}

type FinalizeReadyCheckInput struct {
	MeetingID string
	RoundID   string
}

type UpdatePreferenceInput struct {
	UserID                   string
	DefaultCameraEnabled     bool
	DefaultMicrophoneEnabled bool
}

type AuditReportInput struct {
	MeetingID        string
	ParticipantID    string
	UserID           string
	ParticipantRole  Role
	DeviceType       string
	IPAddress        string
	LatencyMS        int64
	PacketLossRate   float64
	AverageFPS       float64
	AverageBitrateKB int64
	Details          map[string]any
}

func NewService(logger *slog.Logger, store PreferenceStore) *Service {
	return &Service{
		logger:   logger,
		store:    store,
		meetings: make(map[string]*Meeting),
		numbers:  make(map[string]string),
	}
}

func (s *Service) CreateMeeting(ctx context.Context, input CreateMeetingInput) (*Meeting, *Participant, error) {
	meetingID, err := generateID(10)
	if err != nil {
		return nil, nil, err
	}

	joinCode, err := generateID(6)
	if err != nil {
		return nil, nil, err
	}

	passwordRequired := strings.TrimSpace(input.Password) != ""
	salt := ""
	passwordHash := ""
	if passwordRequired {
		salt, err = generateID(16)
		if err != nil {
			return nil, nil, err
		}
		passwordHash = hashPassword(input.Password, salt)
	}

	hostPreference, err := s.resolvePreference(ctx, input.HostUserID, false, false, nil, nil)
	if err != nil {
		return nil, nil, err
	}

	hostEmail, err := s.resolveUserEmail(ctx, input.HostUserID, input.HostEmail)
	if err != nil {
		return nil, nil, err
	}

	hostID, err := generateID(12)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().UTC()
	host := &Participant{
		ID:          hostID,
		UserID:      input.HostUserID,
		Nickname:    input.HostNickname,
		Role:        RoleHost,
		IsAnonymous: false,
		JoinedAt:    now,
		RequestedMediaPreference: MediaPreference{
			CameraEnabled:     hostPreference.CameraEnabled,
			MicrophoneEnabled: hostPreference.MicrophoneEnabled,
		},
		EffectiveMediaState: MediaPreference{},
		GrantedCapabilities: capabilitySet(allCapabilities()...),
	}

	s.mu.Lock()
	meetingNumber, err := s.generateUniqueMeetingNumberLocked()
	if err != nil {
		s.mu.Unlock()
		return nil, nil, err
	}

	meeting := &Meeting{
		ID:                meetingID,
		MeetingNumber:     meetingNumber,
		JoinCode:          joinCode,
		PasswordRequired:  passwordRequired,
		Title:             input.Title,
		MeetingType:       normalizeMeetingType(input.MeetingType),
		HostParticipantID: hostID,
		Status:            StatusActive,
		CreatedAt:         now,
		PasswordSalt:      salt,
		PasswordHash:      passwordHash,
		Participants: map[string]*Participant{
			hostID: host,
		},
	}
	addMinuteLocked(meeting, now, fmt.Sprintf("会议已创建，主持人 %s 已入会。", host.Nickname))

	s.meetings[meeting.ID] = meeting
	s.numbers[meeting.MeetingNumber] = meeting.ID
	s.mu.Unlock()

	if err := s.recordMeetingCreated(ctx, meeting, host, hostEmail, input); err != nil {
		return nil, nil, err
	}

	if err := s.insertAudit(ctx, sqlite.AuditEvent{
		MeetingID:       meeting.ID,
		MeetingNumber:   meeting.MeetingNumber,
		ParticipantID:   host.ID,
		UserID:          host.UserID,
		ParticipantRole: string(host.Role),
		EventType:       "meeting_created",
		IPAddress:       input.IPAddress,
		DeviceType:      input.DeviceType,
		DetailsJSON:     fmt.Sprintf(`{"title":%q,"joinCode":%q,"meetingType":%q}`, input.Title, joinCode, meeting.MeetingType),
		CreatedAt:       now,
	}); err != nil {
		return nil, nil, err
	}

	return copyMeeting(meeting), copyParticipant(host), nil
}

func (s *Service) JoinMeeting(ctx context.Context, input JoinMeetingInput) (*Meeting, *Participant, error) {
	participantEmail, err := s.resolveUserEmail(ctx, input.UserID, input.Email)
	if err != nil {
		return nil, nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	meeting, _, ok := s.meetingByIdentifierLocked(input.MeetingID)
	if !ok {
		return nil, nil, ErrMeetingNotFound
	}

	if meeting.Status != StatusActive {
		return nil, nil, ErrMeetingNotFound
	}

	if meeting.PasswordRequired && hashPassword(input.Password, meeting.PasswordSalt) != meeting.PasswordHash {
		return nil, nil, ErrMeetingPassword
	}

	pref, err := s.resolvePreference(
		ctx,
		input.UserID,
		input.IsAnonymous,
		false,
		input.RequestCameraEnabled,
		input.RequestMicrophoneEnabled,
	)
	if err != nil {
		return nil, nil, err
	}

	participantID, err := generateID(12)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().UTC()
	participant := &Participant{
		ID:                       participantID,
		UserID:                   input.UserID,
		Nickname:                 input.Nickname,
		Role:                     RoleParticipant,
		IsAnonymous:              input.IsAnonymous,
		JoinedAt:                 now,
		RequestedMediaPreference: pref,
		EffectiveMediaState:      MediaPreference{},
		GrantedCapabilities:      defaultParticipantCapabilities(input.UserID, input.IsAnonymous),
	}

	meeting.Participants[participant.ID] = participant
	addMinuteLocked(meeting, participant.JoinedAt, fmt.Sprintf("%s 加入会议。", participant.Nickname))

	if err := s.recordParticipantJoined(ctx, meeting.ID, participant, participantEmail, input); err != nil {
		return nil, nil, err
	}

	if err := s.insertAudit(ctx, sqlite.AuditEvent{
		MeetingID:       meeting.ID,
		MeetingNumber:   meeting.MeetingNumber,
		ParticipantID:   participant.ID,
		UserID:          participant.UserID,
		ParticipantRole: string(participant.Role),
		EventType:       "participant_joined",
		IPAddress:       input.IPAddress,
		DeviceType:      input.DeviceType,
		DetailsJSON:     fmt.Sprintf(`{"nickname":%q,"anonymous":%t}`, participant.Nickname, participant.IsAnonymous),
		CreatedAt:       now,
	}); err != nil {
		return nil, nil, err
	}

	return copyMeeting(meeting), copyParticipant(participant), nil
}

func (s *Service) LeaveMeeting(ctx context.Context, meetingID string, participantID string, deviceType string, ipAddress string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	meeting, internalMeetingID, ok := s.meetingByIdentifierLocked(meetingID)
	if !ok {
		return ErrMeetingNotFound
	}

	participant, ok := meeting.Participants[participantID]
	if !ok {
		return ErrParticipantNotFound
	}

	if participantID == meeting.HostParticipantID && len(meeting.Participants) > 1 {
		return ErrUnauthorized
	}

	now := time.Now().UTC()
	participant.LeftAt = &now
	addMinuteLocked(meeting, now, fmt.Sprintf("%s 离开会议。", participant.Nickname))

	if err := s.insertAudit(ctx, sqlite.AuditEvent{
		MeetingID:       meeting.ID,
		MeetingNumber:   meeting.MeetingNumber,
		ParticipantID:   participant.ID,
		UserID:          participant.UserID,
		ParticipantRole: string(participant.Role),
		EventType:       "participant_left",
		IPAddress:       ipAddress,
		DeviceType:      deviceType,
		CreatedAt:       now,
	}); err != nil {
		return err
	}

	if err := s.recordParticipantLeft(ctx, meeting.ID, participant.ID, now); err != nil {
		return err
	}

	delete(meeting.Participants, participantID)
	if len(meeting.Participants) == 0 {
		endMeetingLocked(meeting, now)
		if err := s.recordMeetingEnded(ctx, meeting.ID, now); err != nil {
			return err
		}
		delete(s.meetings, internalMeetingID)
		delete(s.numbers, meeting.MeetingNumber)
	}

	return nil
}

func (s *Service) EndMeeting(ctx context.Context, meetingID string, endedByParticipantID string, deviceType string, ipAddress string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	meeting, internalMeetingID, ok := s.meetingByIdentifierLocked(meetingID)
	if !ok {
		return ErrMeetingNotFound
	}

	if meeting.HostParticipantID != endedByParticipantID {
		return ErrUnauthorized
	}

	now := time.Now().UTC()
	if err := s.insertAudit(ctx, sqlite.AuditEvent{
		MeetingID:       meeting.ID,
		MeetingNumber:   meeting.MeetingNumber,
		ParticipantID:   endedByParticipantID,
		ParticipantRole: string(RoleHost),
		EventType:       "meeting_ended",
		IPAddress:       ipAddress,
		DeviceType:      deviceType,
		CreatedAt:       now,
	}); err != nil {
		return err
	}

	for _, participant := range meeting.Participants {
		if participant.LeftAt == nil {
			leftAt := now
			participant.LeftAt = &leftAt
		}
		if err := s.recordParticipantLeft(ctx, meeting.ID, participant.ID, now); err != nil {
			return err
		}
	}

	endMeetingLocked(meeting, now)
	if err := s.recordMeetingEnded(ctx, meeting.ID, now); err != nil {
		return err
	}
	delete(s.meetings, internalMeetingID)
	delete(s.numbers, meeting.MeetingNumber)

	return nil
}

func (s *Service) GrantCapability(ctx context.Context, input GrantCapabilityInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	meeting, _, ok := s.meetingByIdentifierLocked(input.MeetingID)
	if !ok {
		return ErrMeetingNotFound
	}

	if meeting.HostParticipantID != input.HostID {
		return ErrUnauthorized
	}

	participant, ok := meeting.Participants[input.ParticipantID]
	if !ok {
		return ErrParticipantNotFound
	}

	participant.GrantedCapabilities[input.Capability] = struct{}{}
	addMinuteLocked(meeting, time.Now().UTC(), fmt.Sprintf("%s 已向 %s 授权 %s。", actorLabel(meeting, input.HostID), participant.Nickname, input.Capability))

	if err := s.insertAudit(ctx, sqlite.AuditEvent{
		MeetingID:       meeting.ID,
		MeetingNumber:   meeting.MeetingNumber,
		ParticipantID:   participant.ID,
		UserID:          participant.UserID,
		ParticipantRole: string(participant.Role),
		EventType:       "capability_granted",
		DetailsJSON:     fmt.Sprintf(`{"capability":%q,"grantedBy":%q}`, input.Capability, input.HostID),
		CreatedAt:       time.Now().UTC(),
	}); err != nil {
		return err
	}

	return nil
}

func (s *Service) RequestCapability(ctx context.Context, input CapabilityRequestInput) (*ChatMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	meetingValue, actor, err := s.meetingActorLocked(input.MeetingID, input.ParticipantID)
	if err != nil {
		return nil, err
	}

	if actor.Role != RoleParticipant {
		return nil, ErrUnauthorized
	}

	if !isParticipantCapabilityRequestable(input.Capability) {
		return nil, ErrUnauthorized
	}

	if _, alreadyGranted := actor.GrantedCapabilities[input.Capability]; alreadyGranted {
		return nil, ErrUnauthorized
	}

	messageText := fmt.Sprintf(
		"@主持人，%s 申请开启%s权限。",
		actor.Nickname,
		formatCapabilityLabel(input.Capability),
	)
	systemMessage, err := s.appendSystemChatLocked(
		meetingValue,
		actor.ID,
		ChatMessageKindCapabilityRequest,
		messageText,
		&ChatMessageAction{
			Type:                ChatMessageActionTypeOpenPermissions,
			TargetParticipantID: actor.ID,
			Capability:          input.Capability,
			RequestedBy:         actor.ID,
		},
	)
	if err != nil {
		return nil, err
	}

	addMinuteLocked(meetingValue, systemMessage.SentAt, messageText)

	if err := s.insertAudit(ctx, sqlite.AuditEvent{
		MeetingID:       meetingValue.ID,
		MeetingNumber:   meetingValue.MeetingNumber,
		ParticipantID:   actor.ID,
		UserID:          actor.UserID,
		ParticipantRole: string(actor.Role),
		EventType:       "capability_requested",
		DetailsJSON:     fmt.Sprintf(`{"capability":%q}`, input.Capability),
		CreatedAt:       systemMessage.SentAt,
	}); err != nil {
		return nil, err
	}

	copied := copyChatMessage(systemMessage)
	return &copied, nil
}

func (s *Service) AssignAssistant(ctx context.Context, input AssignAssistantInput) (*Participant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	meetingValue, actor, participant, err := s.meetingActorTargetLocked(input.MeetingID, input.HostID, input.ParticipantID)
	if err != nil {
		return nil, err
	}

	if actor.Role != RoleHost {
		return nil, ErrUnauthorized
	}

	participant.Role = RoleAssistant
	participant.GrantedCapabilities = capabilitySet(allCapabilities()...)
	now := time.Now().UTC()
	addMinuteLocked(meetingValue, now, fmt.Sprintf("%s 已将 %s 设为助理。", actor.Nickname, participant.Nickname))

	if err := s.store.UpdateMeetingParticipantUsageRole(ctx, meetingValue.ID, participant.ID, string(participant.Role), now); err != nil {
		return nil, err
	}

	if err := s.insertAudit(ctx, sqlite.AuditEvent{
		MeetingID:       meetingValue.ID,
		MeetingNumber:   meetingValue.MeetingNumber,
		ParticipantID:   participant.ID,
		UserID:          participant.UserID,
		ParticipantRole: string(participant.Role),
		EventType:       "assistant_assigned",
		DetailsJSON:     fmt.Sprintf(`{"assignedBy":%q}`, actor.ID),
		CreatedAt:       now,
	}); err != nil {
		return nil, err
	}

	return copyParticipant(participant), nil
}

func (s *Service) UpdateNickname(ctx context.Context, input UpdateNicknameInput) (*Participant, *ChatMessage, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	meetingValue, actor, err := s.meetingActorLocked(input.MeetingID, input.ParticipantID)
	if err != nil {
		return nil, nil, "", err
	}

	trimmedNickname := strings.TrimSpace(input.Nickname)
	if trimmedNickname == "" {
		return nil, nil, "", fmt.Errorf("nickname is required")
	}

	previousNickname := actor.Nickname
	if previousNickname == trimmedNickname {
		participantCopy := copyParticipant(actor)
		return participantCopy, nil, previousNickname, nil
	}

	actor.Nickname = trimmedNickname
	systemMessage, err := s.appendSystemChatLocked(
		meetingValue,
		actor.ID,
		ChatMessageKindSystem,
		fmt.Sprintf("%s 将昵称修改为 %s。", previousNickname, trimmedNickname),
		nil,
	)
	if err != nil {
		return nil, nil, "", err
	}

	addMinuteLocked(meetingValue, systemMessage.SentAt, fmt.Sprintf("%s 将昵称修改为 %s。", previousNickname, trimmedNickname))

	if err := s.store.UpdateMeetingParticipantUsageNickname(ctx, meetingValue.ID, actor.ID, actor.Nickname, systemMessage.SentAt); err != nil {
		return nil, nil, "", err
	}

	if err := s.insertAudit(ctx, sqlite.AuditEvent{
		MeetingID:       meetingValue.ID,
		MeetingNumber:   meetingValue.MeetingNumber,
		ParticipantID:   actor.ID,
		UserID:          actor.UserID,
		ParticipantRole: string(actor.Role),
		EventType:       "nickname_updated",
		DetailsJSON:     fmt.Sprintf(`{"before":%q,"after":%q}`, previousNickname, trimmedNickname),
		CreatedAt:       systemMessage.SentAt,
	}); err != nil {
		return nil, nil, "", err
	}

	participantCopy := copyParticipant(actor)
	messageCopy := copyChatMessage(systemMessage)
	return participantCopy, &messageCopy, previousNickname, nil
}

func (s *Service) AppendChatMessage(ctx context.Context, input ChatMessageInput) (*ChatMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	meetingValue, actor, err := s.meetingActorLocked(input.MeetingID, input.ParticipantID)
	if err != nil {
		return nil, err
	}

	if _, ok := actor.GrantedCapabilities[CapabilityChat]; !ok {
		return nil, ErrUnauthorized
	}

	messageID, err := generateID(10)
	if err != nil {
		return nil, err
	}

	message := ChatMessage{
		ID:            messageID,
		ParticipantID: actor.ID,
		Nickname:      actor.Nickname,
		Kind:          ChatMessageKindUser,
		Message:       input.Message,
		SentAt:        time.Now().UTC(),
	}
	meetingValue.ChatMessages = append(meetingValue.ChatMessages, message)
	addMinuteLocked(meetingValue, message.SentAt, fmt.Sprintf("%s：%s", actor.Nickname, input.Message))

	if err := s.insertAudit(ctx, sqlite.AuditEvent{
		MeetingID:       meetingValue.ID,
		MeetingNumber:   meetingValue.MeetingNumber,
		ParticipantID:   actor.ID,
		UserID:          actor.UserID,
		ParticipantRole: string(actor.Role),
		EventType:       "chat_message_appended",
		DetailsJSON:     fmt.Sprintf(`{"messageId":%q}`, message.ID),
		CreatedAt:       message.SentAt,
	}); err != nil {
		return nil, err
	}

	copied := copyChatMessage(message)
	return &copied, nil
}

func (s *Service) AppendWhiteboardAction(ctx context.Context, input WhiteboardActionInput) (*WhiteboardAction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	meetingValue, actor, err := s.meetingActorLocked(input.MeetingID, input.ParticipantID)
	if err != nil {
		return nil, err
	}

	if _, ok := actor.GrantedCapabilities[CapabilityWhiteboard]; !ok {
		return nil, ErrUnauthorized
	}

	action := input.Action
	if action.ID == "" {
		action.ID, err = generateID(10)
		if err != nil {
			return nil, err
		}
	}
	action.CreatedBy = actor.ID
	action.CreatedAt = time.Now().UTC()
	meetingValue.WhiteboardActions = append(meetingValue.WhiteboardActions, action)

	if err := s.insertAudit(ctx, sqlite.AuditEvent{
		MeetingID:       meetingValue.ID,
		MeetingNumber:   meetingValue.MeetingNumber,
		ParticipantID:   actor.ID,
		UserID:          actor.UserID,
		ParticipantRole: string(actor.Role),
		EventType:       "whiteboard_action_appended",
		DetailsJSON:     fmt.Sprintf(`{"actionId":%q,"kind":%q}`, action.ID, action.Kind),
		CreatedAt:       action.CreatedAt,
	}); err != nil {
		return nil, err
	}

	copied := copyWhiteboardAction(action)
	return &copied, nil
}

func (s *Service) ClearWhiteboard(ctx context.Context, input ClearWhiteboardInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	meetingValue, actor, err := s.meetingActorLocked(input.MeetingID, input.ParticipantID)
	if err != nil {
		return err
	}

	if _, ok := actor.GrantedCapabilities[CapabilityWhiteboard]; !ok {
		return ErrUnauthorized
	}

	meetingValue.WhiteboardActions = nil
	addMinuteLocked(meetingValue, time.Now().UTC(), fmt.Sprintf("%s 清空了白板。", actor.Nickname))

	if err := s.insertAudit(ctx, sqlite.AuditEvent{
		MeetingID:       meetingValue.ID,
		MeetingNumber:   meetingValue.MeetingNumber,
		ParticipantID:   actor.ID,
		UserID:          actor.UserID,
		ParticipantRole: string(actor.Role),
		EventType:       "whiteboard_cleared",
		CreatedAt:       time.Now().UTC(),
	}); err != nil {
		return err
	}

	return nil
}

func (s *Service) StartReadyCheck(ctx context.Context, input StartReadyCheckInput) (*ReadyCheckRound, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	meetingValue, actor, err := s.meetingActorLocked(input.MeetingID, input.ParticipantID)
	if err != nil {
		return nil, err
	}

	if _, ok := actor.GrantedCapabilities[CapabilityReadyCheck]; !ok {
		return nil, ErrUnauthorized
	}

	timeoutSeconds := input.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 15
	}

	now := time.Now().UTC()
	roundID, err := generateID(10)
	if err != nil {
		return nil, err
	}

	results := make(map[string]ReadyCheckResult, len(meetingValue.Participants))
	for participantID := range meetingValue.Participants {
		results[participantID] = ReadyCheckResult{
			ParticipantID: participantID,
			Status:        ReadyCheckPending,
		}
	}

	meetingValue.ActiveReadyCheck = &ReadyCheckRound{
		ID:             roundID,
		StartedBy:      actor.ID,
		TimeoutSeconds: timeoutSeconds,
		StartedAt:      now,
		DeadlineAt:     now.Add(time.Duration(timeoutSeconds) * time.Second),
		Status:         "active",
		Results:        results,
	}
	addMinuteLocked(meetingValue, now, fmt.Sprintf("%s 发起了就位确认，超时时间 %d 秒。", actor.Nickname, timeoutSeconds))

	if err := s.insertAudit(ctx, sqlite.AuditEvent{
		MeetingID:       meetingValue.ID,
		MeetingNumber:   meetingValue.MeetingNumber,
		ParticipantID:   actor.ID,
		UserID:          actor.UserID,
		ParticipantRole: string(actor.Role),
		EventType:       "ready_check_started",
		DetailsJSON:     fmt.Sprintf(`{"roundId":%q,"timeoutSeconds":%d}`, roundID, timeoutSeconds),
		CreatedAt:       now,
	}); err != nil {
		return nil, err
	}

	return copyReadyCheckRound(meetingValue.ActiveReadyCheck), nil
}

func (s *Service) RespondReadyCheck(ctx context.Context, input RespondReadyCheckInput) (*ReadyCheckRound, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	meetingValue, actor, err := s.meetingActorLocked(input.MeetingID, input.ParticipantID)
	if err != nil {
		return nil, false, err
	}

	round := meetingValue.ActiveReadyCheck
	if round == nil || round.Status != "active" {
		return nil, false, ErrMeetingNotFound
	}

	now := time.Now().UTC()
	round.Results[actor.ID] = ReadyCheckResult{
		ParticipantID: actor.ID,
		Status:        input.Status,
		UpdatedAt:     &now,
	}
	addMinuteLocked(meetingValue, now, fmt.Sprintf("%s 对就位确认选择了 %s。", actor.Nickname, input.Status))

	completed := readyCheckCompleted(round)
	if completed {
		round.Status = "completed"
	}

	if err := s.insertAudit(ctx, sqlite.AuditEvent{
		MeetingID:       meetingValue.ID,
		MeetingNumber:   meetingValue.MeetingNumber,
		ParticipantID:   actor.ID,
		UserID:          actor.UserID,
		ParticipantRole: string(actor.Role),
		EventType:       "ready_check_responded",
		DetailsJSON:     fmt.Sprintf(`{"roundId":%q,"status":%q}`, round.ID, input.Status),
		CreatedAt:       now,
	}); err != nil {
		return nil, false, err
	}

	return copyReadyCheckRound(round), completed, nil
}

func (s *Service) FinalizeReadyCheck(ctx context.Context, input FinalizeReadyCheckInput) (*ReadyCheckRound, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	meetingValue, _, ok := s.meetingByIdentifierLocked(input.MeetingID)
	if !ok {
		return nil, false, ErrMeetingNotFound
	}

	round := meetingValue.ActiveReadyCheck
	if round == nil || round.ID != input.RoundID {
		return nil, false, ErrMeetingNotFound
	}

	if round.Status == "completed" {
		return copyReadyCheckRound(round), false, nil
	}

	now := time.Now().UTC()
	for participantID, result := range round.Results {
		if result.Status != ReadyCheckPending {
			continue
		}
		updatedAt := now
		round.Results[participantID] = ReadyCheckResult{
			ParticipantID: participantID,
			Status:        ReadyCheckTimeout,
			UpdatedAt:     &updatedAt,
		}
	}
	round.Status = "completed"
	addMinuteLocked(meetingValue, now, fmt.Sprintf("就位确认已结束：%s。", summarizeReadyCheck(round)))

	if err := s.insertAudit(ctx, sqlite.AuditEvent{
		MeetingID:     meetingValue.ID,
		MeetingNumber: meetingValue.MeetingNumber,
		ParticipantID: round.StartedBy,
		EventType:     "ready_check_completed",
		DetailsJSON:   fmt.Sprintf(`{"roundId":%q}`, round.ID),
		CreatedAt:     now,
	}); err != nil {
		return nil, false, err
	}

	return copyReadyCheckRound(round), true, nil
}

func (s *Service) SaveUserPreference(ctx context.Context, input UpdatePreferenceInput) error {
	return s.store.UpsertUserPreference(ctx, sqlite.UserPreference{
		UserID:                   input.UserID,
		DefaultCameraEnabled:     input.DefaultCameraEnabled,
		DefaultMicrophoneEnabled: input.DefaultMicrophoneEnabled,
		UpdatedAt:                time.Now().UTC(),
	})
}

func (s *Service) RecordAuditReport(ctx context.Context, input AuditReportInput) error {
	detailsBytes, err := json.Marshal(input.Details)
	if err != nil {
		return fmt.Errorf("marshal audit details: %w", err)
	}
	meetingID := input.MeetingID
	meetingNumber := ""
	if meetingValue, found := s.GetMeeting(input.MeetingID); found {
		meetingID = meetingValue.ID
		meetingNumber = meetingValue.MeetingNumber
	}

	return s.insertAudit(ctx, sqlite.AuditEvent{
		MeetingID:        meetingID,
		MeetingNumber:    meetingNumber,
		ParticipantID:    input.ParticipantID,
		UserID:           input.UserID,
		ParticipantRole:  string(input.ParticipantRole),
		EventType:        "media_report",
		IPAddress:        input.IPAddress,
		DeviceType:       input.DeviceType,
		LatencyMS:        input.LatencyMS,
		PacketLossRate:   input.PacketLossRate,
		AverageFPS:       input.AverageFPS,
		AverageBitrateKB: input.AverageBitrateKB,
		DetailsJSON:      string(detailsBytes),
		CreatedAt:        time.Now().UTC(),
	})
}

func (s *Service) GetMeeting(meetingID string) (*Meeting, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	meeting, _, ok := s.meetingByIdentifierLocked(meetingID)
	if !ok {
		return nil, false
	}

	return copyMeeting(meeting), true
}

func (s *Service) insertAudit(ctx context.Context, event sqlite.AuditEvent) error {
	if event.DetailsJSON == "" {
		event.DetailsJSON = "{}"
	}
	if err := s.store.InsertAuditEvent(ctx, event); err != nil {
		return err
	}

	s.logger.Info("audit event recorded",
		"meetingId", event.MeetingID,
		"meetingNumber", event.MeetingNumber,
		"participantId", event.ParticipantID,
		"eventType", event.EventType,
	)

	return nil
}

func (s *Service) recordMeetingCreated(ctx context.Context, meetingValue *Meeting, host *Participant, hostEmail string, input CreateMeetingInput) error {
	record := sqlite.MeetingUsageRecord{
		ID:                meetingValue.ID,
		MeetingNumber:     meetingValue.MeetingNumber,
		JoinCode:          meetingValue.JoinCode,
		Title:             meetingValue.Title,
		MeetingType:       string(meetingValue.MeetingType),
		HostParticipantID: meetingValue.HostParticipantID,
		HostUserID:        host.UserID,
		HostEmail:         hostEmail,
		HostNickname:      host.Nickname,
		HostIPAddress:     input.IPAddress,
		CreatedAt:         meetingValue.CreatedAt,
		UpdatedAt:         meetingValue.CreatedAt,
	}
	if err := s.store.UpsertMeetingUsage(ctx, record); err != nil {
		return err
	}

	return s.store.UpsertMeetingParticipantUsage(ctx, sqlite.MeetingParticipantUsageRecord{
		MeetingID:         meetingValue.ID,
		ParticipantID:     host.ID,
		UserID:            host.UserID,
		Email:             hostEmail,
		Nickname:          host.Nickname,
		IsAnonymous:       host.IsAnonymous,
		IPAddress:         input.IPAddress,
		DeviceType:        input.DeviceType,
		ClientProfileJSON: encodeClientProfile(input.ClientProfile),
		ParticipantRole:   string(host.Role),
		JoinedAt:          host.JoinedAt,
		UpdatedAt:         host.JoinedAt,
	})
}

func (s *Service) recordParticipantJoined(ctx context.Context, meetingID string, participant *Participant, email string, input JoinMeetingInput) error {
	return s.store.UpsertMeetingParticipantUsage(ctx, sqlite.MeetingParticipantUsageRecord{
		MeetingID:         meetingID,
		ParticipantID:     participant.ID,
		UserID:            participant.UserID,
		Email:             email,
		Nickname:          participant.Nickname,
		IsAnonymous:       participant.IsAnonymous,
		IPAddress:         input.IPAddress,
		DeviceType:        input.DeviceType,
		ClientProfileJSON: encodeClientProfile(input.ClientProfile),
		ParticipantRole:   string(participant.Role),
		JoinedAt:          participant.JoinedAt,
		UpdatedAt:         participant.JoinedAt,
	})
}

func (s *Service) recordParticipantLeft(ctx context.Context, meetingID string, participantID string, leftAt time.Time) error {
	return s.store.UpdateMeetingParticipantUsageLeftAt(ctx, meetingID, participantID, leftAt, leftAt)
}

func (s *Service) recordMeetingEnded(ctx context.Context, meetingID string, endedAt time.Time) error {
	return s.store.UpdateMeetingUsageEndedAt(ctx, meetingID, endedAt, endedAt)
}

func (s *Service) resolveUserEmail(ctx context.Context, userID string, email string) (string, error) {
	trimmedEmail := strings.TrimSpace(email)
	if trimmedEmail != "" || strings.TrimSpace(userID) == "" {
		return trimmedEmail, nil
	}

	user, found, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", nil
	}

	return strings.TrimSpace(user.Email), nil
}

func encodeClientProfile(profile map[string]string) string {
	if len(profile) == 0 {
		return "{}"
	}

	allowedKeys := map[string]struct{}{
		"browser":               {},
		"os":                    {},
		"deviceCategory":        {},
		"language":              {},
		"timeZone":              {},
		"screenWidthBucket":     {},
		"viewportWidthBucket":   {},
		"colorScheme":           {},
		"networkEffectiveType":  {},
		"webRTCSupported":       {},
		"mediaDevicesSupported": {},
		"audioInputSupported":   {},
		"videoInputSupported":   {},
	}
	sanitized := make(map[string]string, len(profile))
	for key, value := range profile {
		normalizedKey := strings.TrimSpace(key)
		if _, ok := allowedKeys[normalizedKey]; !ok {
			continue
		}
		normalizedValue := strings.TrimSpace(value)
		if len(normalizedValue) > 80 {
			normalizedValue = normalizedValue[:80]
		}
		if normalizedValue != "" {
			sanitized[normalizedKey] = normalizedValue
		}
	}
	if len(sanitized) == 0 {
		return "{}"
	}

	data, err := json.Marshal(sanitized)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func (s *Service) resolvePreference(
	ctx context.Context,
	userID string,
	isAnonymous bool,
	defaultValue bool,
	requestCameraEnabled *bool,
	requestMicrophoneEnabled *bool,
) (MediaPreference, error) {
	preference := MediaPreference{
		CameraEnabled:     defaultValue,
		MicrophoneEnabled: defaultValue,
	}

	if !isAnonymous && userID != "" {
		savedPreference, found, err := s.store.GetUserPreference(ctx, userID)
		if err != nil {
			return MediaPreference{}, err
		}
		if found {
			preference.CameraEnabled = savedPreference.DefaultCameraEnabled
			preference.MicrophoneEnabled = savedPreference.DefaultMicrophoneEnabled
		}
	}

	if requestCameraEnabled != nil {
		preference.CameraEnabled = *requestCameraEnabled
	}

	if requestMicrophoneEnabled != nil {
		preference.MicrophoneEnabled = *requestMicrophoneEnabled
	}

	return preference, nil
}

func capabilitySet(capabilities ...Capability) map[Capability]struct{} {
	set := make(map[Capability]struct{}, len(capabilities))
	for _, capability := range capabilities {
		set[capability] = struct{}{}
	}
	return set
}

func defaultParticipantCapabilities(userID string, isAnonymous bool) map[Capability]struct{} {
	if isRegisteredParticipant(userID, isAnonymous) {
		return capabilitySet(
			CapabilityChat,
			CapabilityMicrophone,
			CapabilityCamera,
			CapabilityScreenShare,
		)
	}

	return capabilitySet(CapabilityChat)
}

func isRegisteredParticipant(userID string, isAnonymous bool) bool {
	return strings.TrimSpace(userID) != "" && !isAnonymous
}

func normalizeMeetingType(value MeetingType) MeetingType {
	switch value {
	case MeetingTypeScheduled:
		return MeetingTypeScheduled
	default:
		return MeetingTypeQuick
	}
}

func isParticipantCapabilityRequestable(capability Capability) bool {
	for _, candidate := range requestableParticipantCapabilities() {
		if capability == candidate {
			return true
		}
	}

	return false
}

func formatCapabilityLabel(capability Capability) string {
	switch capability {
	case CapabilityMicrophone:
		return "麦克风"
	case CapabilityCamera:
		return "摄像头"
	case CapabilityWhiteboard:
		return "白板"
	case CapabilityScreenShare:
		return "共享屏幕"
	case CapabilityRecord:
		return "录制"
	case CapabilityReadyCheck:
		return "就位确认"
	case CapabilityChat:
		return "聊天"
	default:
		return string(capability)
	}
}

func copyMeeting(value *Meeting) *Meeting {
	if value == nil {
		return nil
	}

	participants := make(map[string]*Participant, len(value.Participants))
	for id, participant := range value.Participants {
		participants[id] = copyParticipant(participant)
	}

	minutes := make([]string, len(value.TemporaryMinutes))
	copy(minutes, value.TemporaryMinutes)

	chatMessages := make([]ChatMessage, len(value.ChatMessages))
	for index, message := range value.ChatMessages {
		chatMessages[index] = copyChatMessage(message)
	}

	whiteboardActions := make([]WhiteboardAction, len(value.WhiteboardActions))
	for index, action := range value.WhiteboardActions {
		whiteboardActions[index] = copyWhiteboardAction(action)
	}

	copied := *value
	copied.Participants = participants
	copied.ChatMessages = chatMessages
	copied.WhiteboardActions = whiteboardActions
	copied.ActiveReadyCheck = copyReadyCheckRound(value.ActiveReadyCheck)
	copied.TemporaryMinutes = minutes
	return &copied
}

func copyParticipant(value *Participant) *Participant {
	if value == nil {
		return nil
	}

	capabilities := make(map[Capability]struct{}, len(value.GrantedCapabilities))
	for capability := range value.GrantedCapabilities {
		capabilities[capability] = struct{}{}
	}

	copied := *value
	copied.GrantedCapabilities = capabilities
	return &copied
}

func endMeetingLocked(meeting *Meeting, endedAt time.Time) {
	meeting.Status = StatusEnded
	meeting.EndedAt = &endedAt
	meeting.ChatMessages = nil
	meeting.WhiteboardActions = nil
	meeting.ActiveReadyCheck = nil
	meeting.TemporaryMinutes = nil
	meeting.Participants = map[string]*Participant{}
}

func (s *Service) meetingActorLocked(meetingID string, participantID string) (*Meeting, *Participant, error) {
	meetingValue, _, ok := s.meetingByIdentifierLocked(meetingID)
	if !ok {
		return nil, nil, ErrMeetingNotFound
	}

	participant, ok := meetingValue.Participants[participantID]
	if !ok {
		return nil, nil, ErrParticipantNotFound
	}

	return meetingValue, participant, nil
}

func (s *Service) meetingActorTargetLocked(meetingID string, actorID string, targetID string) (*Meeting, *Participant, *Participant, error) {
	meetingValue, actor, err := s.meetingActorLocked(meetingID, actorID)
	if err != nil {
		return nil, nil, nil, err
	}

	target, ok := meetingValue.Participants[targetID]
	if !ok {
		return nil, nil, nil, ErrParticipantNotFound
	}

	return meetingValue, actor, target, nil
}

func copyWhiteboardAction(value WhiteboardAction) WhiteboardAction {
	points := make([]WhiteboardPoint, len(value.Points))
	copy(points, value.Points)
	value.Points = points
	return value
}

func copyChatMessage(value ChatMessage) ChatMessage {
	if value.Action != nil {
		actionCopy := *value.Action
		value.Action = &actionCopy
	}
	return value
}

func copyReadyCheckRound(value *ReadyCheckRound) *ReadyCheckRound {
	if value == nil {
		return nil
	}

	results := make(map[string]ReadyCheckResult, len(value.Results))
	for participantID, result := range value.Results {
		results[participantID] = result
	}

	copied := *value
	copied.Results = results
	return &copied
}

func readyCheckCompleted(round *ReadyCheckRound) bool {
	for _, result := range round.Results {
		if result.Status == ReadyCheckPending {
			return false
		}
	}
	return true
}

func addMinuteLocked(meeting *Meeting, timestamp time.Time, message string) {
	entry := fmt.Sprintf("[%s] %s", timestamp.Format("15:04:05"), message)
	meeting.TemporaryMinutes = append(meeting.TemporaryMinutes, entry)
}

func (s *Service) generateUniqueMeetingNumberLocked() (string, error) {
	for attempt := 0; attempt < 64; attempt++ {
		meetingNumber, err := generateMeetingNumber()
		if err != nil {
			return "", err
		}
		if _, exists := s.numbers[meetingNumber]; exists {
			continue
		}
		return meetingNumber, nil
	}

	return "", fmt.Errorf("generate meeting number: too many collisions")
}

func (s *Service) meetingByIdentifierLocked(identifier string) (*Meeting, string, bool) {
	trimmedIdentifier := strings.TrimSpace(identifier)
	if trimmedIdentifier == "" {
		return nil, "", false
	}

	if meetingValue, ok := s.meetings[trimmedIdentifier]; ok {
		return meetingValue, trimmedIdentifier, true
	}

	meetingNumber := normalizeMeetingNumber(trimmedIdentifier)
	if meetingNumber == "" {
		return nil, "", false
	}

	internalMeetingID, ok := s.numbers[meetingNumber]
	if !ok {
		return nil, "", false
	}

	meetingValue, ok := s.meetings[internalMeetingID]
	if !ok {
		return nil, "", false
	}

	return meetingValue, internalMeetingID, true
}

func (s *Service) appendSystemChatLocked(
	meetingValue *Meeting,
	participantID string,
	kind ChatMessageKind,
	message string,
	action *ChatMessageAction,
) (ChatMessage, error) {
	messageID, err := generateID(10)
	if err != nil {
		return ChatMessage{}, err
	}

	systemMessage := ChatMessage{
		ID:            messageID,
		ParticipantID: participantID,
		Nickname:      "系统消息",
		Kind:          kind,
		Message:       message,
		Action:        action,
		SentAt:        time.Now().UTC(),
	}
	meetingValue.ChatMessages = append(meetingValue.ChatMessages, systemMessage)
	return systemMessage, nil
}

func actorLabel(meetingValue *Meeting, participantID string) string {
	if participant, ok := meetingValue.Participants[participantID]; ok {
		return participant.Nickname
	}

	return participantID
}

func summarizeReadyCheck(round *ReadyCheckRound) string {
	counts := map[ReadyCheckStatus]int{
		ReadyCheckConfirmed: 0,
		ReadyCheckCancelled: 0,
		ReadyCheckTimeout:   0,
	}
	for _, result := range round.Results {
		counts[result.Status]++
	}

	return fmt.Sprintf(
		"confirmed=%d, cancelled=%d, timeout=%d",
		counts[ReadyCheckConfirmed],
		counts[ReadyCheckCancelled],
		counts[ReadyCheckTimeout],
	)
}

func hashPassword(password string, salt string) string {
	sum := sha256.Sum256([]byte(salt + ":" + password))
	return hex.EncodeToString(sum[:])
}

func generateMeetingNumber() (string, error) {
	const baseValue = 100000000
	rangeValue := big.NewInt(900000000)

	randomValue, err := rand.Int(rand.Reader, rangeValue)
	if err != nil {
		return "", fmt.Errorf("generate meeting number: %w", err)
	}

	return fmt.Sprintf("%09d", randomValue.Int64()+baseValue), nil
}

func normalizeMeetingNumber(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}

	noHyphen := strings.ReplaceAll(trimmed, "-", "")
	return strings.Join(strings.Fields(noHyphen), "")
}

func generateID(bytesLength int) (string, error) {
	buffer := make([]byte, bytesLength)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}
