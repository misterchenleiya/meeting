package meeting

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/07c2/projects/meeting/internal/storage/sqlite"
)

type stubStore struct {
	preferences map[string]sqlite.UserPreference
	auditEvents []sqlite.AuditEvent
}

func newStubStore() *stubStore {
	return &stubStore{
		preferences: map[string]sqlite.UserPreference{},
	}
}

func (s *stubStore) GetUserPreference(_ context.Context, userID string) (sqlite.UserPreference, bool, error) {
	preference, ok := s.preferences[userID]
	return preference, ok, nil
}

func (s *stubStore) UpsertUserPreference(_ context.Context, pref sqlite.UserPreference) error {
	s.preferences[pref.UserID] = pref
	return nil
}

func (s *stubStore) InsertAuditEvent(_ context.Context, event sqlite.AuditEvent) error {
	s.auditEvents = append(s.auditEvents, event)
	return nil
}

func TestJoinMeetingParticipantDefaultsToChatOnly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newStubStore()
	store.preferences["registered-user"] = sqlite.UserPreference{
		UserID:                   "registered-user",
		DefaultCameraEnabled:     true,
		DefaultMicrophoneEnabled: true,
		UpdatedAt:                time.Now().UTC(),
	}

	service := NewService(testLogger(t), store)

	meetingValue, host, err := service.CreateMeeting(ctx, CreateMeetingInput{
		Title:        "daily sync",
		Password:     "secret",
		HostUserID:   "host-user",
		HostNickname: "主持人",
		DeviceType:   "desktop",
		IPAddress:    "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("CreateMeeting() error = %v", err)
	}

	if host == nil {
		t.Fatalf("CreateMeeting() host is nil")
	}

	_, participant, err := service.JoinMeeting(ctx, JoinMeetingInput{
		MeetingID:   meetingValue.ID,
		Password:    "secret",
		UserID:      "registered-user",
		Nickname:    "参与者",
		DeviceType:  "desktop",
		IPAddress:   "127.0.0.1",
		IsAnonymous: false,
	})
	if err != nil {
		t.Fatalf("JoinMeeting() error = %v", err)
	}

	if participant == nil {
		t.Fatalf("JoinMeeting() participant is nil")
	}

	if participant.Role != RoleParticipant {
		t.Fatalf("participant role = %s, want %s", participant.Role, RoleParticipant)
	}

	if participant.RequestedMediaPreference.CameraEnabled != true {
		t.Fatalf("requested camera preference = %t, want true", participant.RequestedMediaPreference.CameraEnabled)
	}

	if participant.RequestedMediaPreference.MicrophoneEnabled != true {
		t.Fatalf("requested microphone preference = %t, want true", participant.RequestedMediaPreference.MicrophoneEnabled)
	}

	if len(participant.GrantedCapabilities) != 1 {
		t.Fatalf("granted capability count = %d, want 1", len(participant.GrantedCapabilities))
	}

	if _, ok := participant.GrantedCapabilities[CapabilityChat]; !ok {
		t.Fatalf("participant should have chat capability")
	}

	if participant.EffectiveMediaState.CameraEnabled {
		t.Fatalf("effective camera state = true, want false")
	}

	if participant.EffectiveMediaState.MicrophoneEnabled {
		t.Fatalf("effective microphone state = true, want false")
	}
}

func TestJoinMeetingAllowsPasswordlessMeeting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service := NewService(testLogger(t), newStubStore())

	meetingValue, _, err := service.CreateMeeting(ctx, CreateMeetingInput{
		Title:        "instant sync",
		Password:     "",
		HostUserID:   "host-user",
		HostNickname: "主持人",
		DeviceType:   "desktop",
		IPAddress:    "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("CreateMeeting() error = %v", err)
	}

	if meetingValue.PasswordRequired {
		t.Fatalf("PasswordRequired = true, want false")
	}

	_, participant, err := service.JoinMeeting(ctx, JoinMeetingInput{
		MeetingID:   meetingValue.ID,
		Password:    "",
		Nickname:    "成员A",
		IsAnonymous: true,
		DeviceType:  "desktop",
		IPAddress:   "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("JoinMeeting() error = %v", err)
	}

	if participant == nil {
		t.Fatalf("JoinMeeting() participant is nil")
	}
}

func TestEndMeetingRemovesRuntimeState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service := NewService(testLogger(t), newStubStore())

	meetingValue, host, err := service.CreateMeeting(ctx, CreateMeetingInput{
		Title:        "retro",
		Password:     "secret",
		HostUserID:   "host-user",
		HostNickname: "主持人",
		DeviceType:   "desktop",
		IPAddress:    "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("CreateMeeting() error = %v", err)
	}

	if err := service.EndMeeting(ctx, meetingValue.ID, host.ID, "desktop", "127.0.0.1"); err != nil {
		t.Fatalf("EndMeeting() error = %v", err)
	}

	if _, ok := service.GetMeeting(meetingValue.ID); ok {
		t.Fatalf("meeting still exists after EndMeeting()")
	}
}

func TestAssignAssistantGrantsAllCapabilities(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service := NewService(testLogger(t), newStubStore())

	meetingValue, host, err := service.CreateMeeting(ctx, CreateMeetingInput{
		Title:        "architecture review",
		Password:     "secret",
		HostUserID:   "host-user",
		HostNickname: "主持人",
		DeviceType:   "desktop",
		IPAddress:    "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("CreateMeeting() error = %v", err)
	}

	_, participant, err := service.JoinMeeting(ctx, JoinMeetingInput{
		MeetingID:   meetingValue.ID,
		Password:    "secret",
		Nickname:    "成员A",
		IsAnonymous: true,
		DeviceType:  "desktop",
		IPAddress:   "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("JoinMeeting() error = %v", err)
	}

	assistant, err := service.AssignAssistant(ctx, AssignAssistantInput{
		MeetingID:     meetingValue.ID,
		HostID:        host.ID,
		ParticipantID: participant.ID,
	})
	if err != nil {
		t.Fatalf("AssignAssistant() error = %v", err)
	}

	if assistant.Role != RoleAssistant {
		t.Fatalf("assistant role = %s, want %s", assistant.Role, RoleAssistant)
	}

	if len(assistant.GrantedCapabilities) != len(allCapabilities()) {
		t.Fatalf("assistant granted capability count = %d, want %d", len(assistant.GrantedCapabilities), len(allCapabilities()))
	}
}

func TestHostCannotLeaveWhileOtherParticipantsRemain(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service := NewService(testLogger(t), newStubStore())

	meetingValue, host, err := service.CreateMeeting(ctx, CreateMeetingInput{
		Title:        "team sync",
		Password:     "secret",
		HostUserID:   "host-user",
		HostNickname: "主持人",
		DeviceType:   "desktop",
		IPAddress:    "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("CreateMeeting() error = %v", err)
	}

	if _, _, err := service.JoinMeeting(ctx, JoinMeetingInput{
		MeetingID:   meetingValue.ID,
		Password:    "secret",
		Nickname:    "成员A",
		IsAnonymous: true,
		DeviceType:  "desktop",
		IPAddress:   "127.0.0.1",
	}); err != nil {
		t.Fatalf("JoinMeeting() error = %v", err)
	}

	if err := service.LeaveMeeting(ctx, meetingValue.ID, host.ID, "desktop", "127.0.0.1"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("LeaveMeeting() error = %v, want unauthorized", err)
	}
}

func TestFinalizeReadyCheckTurnsPendingIntoTimeout(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service := NewService(testLogger(t), newStubStore())

	meetingValue, host, err := service.CreateMeeting(ctx, CreateMeetingInput{
		Title:        "raid check",
		Password:     "secret",
		HostUserID:   "host-user",
		HostNickname: "主持人",
		DeviceType:   "desktop",
		IPAddress:    "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("CreateMeeting() error = %v", err)
	}

	_, participant, err := service.JoinMeeting(ctx, JoinMeetingInput{
		MeetingID:   meetingValue.ID,
		Password:    "secret",
		Nickname:    "成员A",
		IsAnonymous: true,
		DeviceType:  "desktop",
		IPAddress:   "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("JoinMeeting() error = %v", err)
	}

	if err := service.GrantCapability(ctx, GrantCapabilityInput{
		MeetingID:     meetingValue.ID,
		HostID:        host.ID,
		ParticipantID: participant.ID,
		Capability:    CapabilityReadyCheck,
	}); err != nil {
		t.Fatalf("GrantCapability() error = %v", err)
	}

	round, err := service.StartReadyCheck(ctx, StartReadyCheckInput{
		MeetingID:      meetingValue.ID,
		ParticipantID:  host.ID,
		TimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatalf("StartReadyCheck() error = %v", err)
	}

	updatedRound, completed, err := service.RespondReadyCheck(ctx, RespondReadyCheckInput{
		MeetingID:     meetingValue.ID,
		ParticipantID: host.ID,
		Status:        ReadyCheckConfirmed,
	})
	if err != nil {
		t.Fatalf("RespondReadyCheck() error = %v", err)
	}

	if completed {
		t.Fatalf("RespondReadyCheck() completed = true, want false")
	}

	if updatedRound.Results[host.ID].Status != ReadyCheckConfirmed {
		t.Fatalf("host ready check status = %s, want %s", updatedRound.Results[host.ID].Status, ReadyCheckConfirmed)
	}

	finalizedRound, changed, err := service.FinalizeReadyCheck(ctx, FinalizeReadyCheckInput{
		MeetingID: meetingValue.ID,
		RoundID:   round.ID,
	})
	if err != nil {
		t.Fatalf("FinalizeReadyCheck() error = %v", err)
	}

	if !changed {
		t.Fatalf("FinalizeReadyCheck() changed = false, want true")
	}

	if finalizedRound.Results[participant.ID].Status != ReadyCheckTimeout {
		t.Fatalf("participant ready check status = %s, want %s", finalizedRound.Results[participant.ID].Status, ReadyCheckTimeout)
	}
}

func TestAppendChatMessageStoresTemporaryHistory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service := NewService(testLogger(t), newStubStore())

	meetingValue, host, err := service.CreateMeeting(ctx, CreateMeetingInput{
		Title:        "design review",
		Password:     "secret",
		HostUserID:   "host-user",
		HostNickname: "主持人",
		DeviceType:   "desktop",
		IPAddress:    "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("CreateMeeting() error = %v", err)
	}

	message, err := service.AppendChatMessage(ctx, ChatMessageInput{
		MeetingID:     meetingValue.ID,
		ParticipantID: host.ID,
		Message:       "今天先看白板方案。",
	})
	if err != nil {
		t.Fatalf("AppendChatMessage() error = %v", err)
	}

	if message.Nickname != "主持人" {
		t.Fatalf("chat nickname = %q, want 主持人", message.Nickname)
	}

	snapshot, found := service.GetMeeting(meetingValue.ID)
	if !found {
		t.Fatalf("GetMeeting() found = false, want true")
	}

	if len(snapshot.ChatMessages) != 1 {
		t.Fatalf("chat message count = %d, want 1", len(snapshot.ChatMessages))
	}

	if len(snapshot.TemporaryMinutes) == 0 {
		t.Fatalf("temporary minutes should not be empty")
	}

	lastMinute := snapshot.TemporaryMinutes[len(snapshot.TemporaryMinutes)-1]
	if lastMinute == "" || !strings.Contains(lastMinute, "今天先看白板方案") {
		t.Fatalf("last temporary minute = %q, want chat content", lastMinute)
	}
}

func TestUpdateNicknameAppendsSystemChatMessage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service := NewService(testLogger(t), newStubStore())

	meetingValue, _, err := service.CreateMeeting(ctx, CreateMeetingInput{
		Title:        "rename review",
		Password:     "secret",
		HostUserID:   "host-user",
		HostNickname: "旧昵称",
		DeviceType:   "desktop",
		IPAddress:    "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("CreateMeeting() error = %v", err)
	}

	hostParticipantID := meetingValue.HostParticipantID
	participant, systemMessage, previousNickname, err := service.UpdateNickname(ctx, UpdateNicknameInput{
		MeetingID:     meetingValue.ID,
		ParticipantID: hostParticipantID,
		Nickname:      "新昵称",
	})
	if err != nil {
		t.Fatalf("UpdateNickname() error = %v", err)
	}

	if previousNickname != "旧昵称" {
		t.Fatalf("previous nickname = %q, want 旧昵称", previousNickname)
	}

	if participant.Nickname != "新昵称" {
		t.Fatalf("participant nickname = %q, want 新昵称", participant.Nickname)
	}

	if systemMessage == nil {
		t.Fatalf("systemMessage = nil, want value")
	}

	if systemMessage.Nickname != "系统消息" {
		t.Fatalf("system message nickname = %q, want 系统消息", systemMessage.Nickname)
	}

	if !strings.Contains(systemMessage.Message, "旧昵称 将昵称修改为 新昵称") {
		t.Fatalf("system message = %q, want rename log", systemMessage.Message)
	}
}
