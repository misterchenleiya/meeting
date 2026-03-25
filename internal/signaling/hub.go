package signaling

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/07c2/projects/meeting/internal/meeting"
	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 1 << 20
)

var disconnectGracePeriod = 3 * time.Second

var (
	ErrParticipantOffline = errors.New("participant is offline")
	ErrInvalidMessage     = errors.New("invalid signaling message")
)

type MeetingService interface {
	GetMeeting(meetingID string) (*meeting.Meeting, bool)
	LeaveMeeting(ctx context.Context, meetingID string, participantID string, deviceType string, ipAddress string) error
	GrantCapability(ctx context.Context, input meeting.GrantCapabilityInput) error
	AssignAssistant(ctx context.Context, input meeting.AssignAssistantInput) (*meeting.Participant, error)
	UpdateNickname(ctx context.Context, input meeting.UpdateNicknameInput) (*meeting.Participant, *meeting.ChatMessage, string, error)
	AppendChatMessage(ctx context.Context, input meeting.ChatMessageInput) (*meeting.ChatMessage, error)
	AppendWhiteboardAction(ctx context.Context, input meeting.WhiteboardActionInput) (*meeting.WhiteboardAction, error)
	ClearWhiteboard(ctx context.Context, input meeting.ClearWhiteboardInput) error
	StartReadyCheck(ctx context.Context, input meeting.StartReadyCheckInput) (*meeting.ReadyCheckRound, error)
	RespondReadyCheck(ctx context.Context, input meeting.RespondReadyCheckInput) (*meeting.ReadyCheckRound, bool, error)
	FinalizeReadyCheck(ctx context.Context, input meeting.FinalizeReadyCheckInput) (*meeting.ReadyCheckRound, bool, error)
}

type Hub struct {
	logger   *slog.Logger
	meetings MeetingService

	mu                 sync.RWMutex
	rooms              map[string]map[string]*client
	disconnectCleanups map[string]*time.Timer
}

type envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type serverEnvelope struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload,omitempty"`
}

type client struct {
	conn          *websocket.Conn
	hub           *Hub
	meetingID     string
	participantID string
	send          chan serverEnvelope
}

type welcomePayload struct {
	Meeting              *meeting.Meeting `json:"meeting"`
	ParticipantID        string           `json:"participantId"`
	OnlineParticipantIDs []string         `json:"onlineParticipantIds"`
	ServerTime           time.Time        `json:"serverTime"`
}

type participantPresencePayload struct {
	ParticipantID string `json:"participantId"`
	Status        string `json:"status"`
}

type participantJoinedPayload struct {
	Participant *meeting.Participant `json:"participant"`
}

type participantLeftPayload struct {
	ParticipantID string `json:"participantId"`
}

type capabilityRequestPayload struct {
	FromParticipantID string             `json:"fromParticipantId"`
	Capability        meeting.Capability `json:"capability"`
}

type capabilityGrantedPayload struct {
	TargetParticipantID string             `json:"targetParticipantId"`
	GrantedBy           string             `json:"grantedBy"`
	Capability          meeting.Capability `json:"capability"`
}

type meetingEndedPayload struct {
	EndedByParticipantID string `json:"endedByParticipantId"`
}

type chatMessagePayload struct {
	Message meeting.ChatMessage `json:"message"`
}

type assistantAssignedPayload struct {
	Participant *meeting.Participant `json:"participant"`
	AssignedBy  string               `json:"assignedBy"`
}

type nicknameUpdatedPayload struct {
	Participant      *meeting.Participant `json:"participant"`
	PreviousNickname string               `json:"previousNickname"`
	SystemMessage    *meeting.ChatMessage `json:"systemMessage,omitempty"`
}

type whiteboardActionPayload struct {
	Action meeting.WhiteboardAction `json:"action"`
}

type readyCheckRoundPayload struct {
	Round *meeting.ReadyCheckRound `json:"round"`
}

type targetedPayload struct {
	TargetParticipantID string          `json:"targetParticipantId"`
	Data                json.RawMessage `json:"data"`
}

type forwardedSignalPayload struct {
	FromParticipantID string          `json:"fromParticipantId"`
	Data              json.RawMessage `json:"data"`
}

type outgoingErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(_ *http.Request) bool {
		// 开发阶段先允许跨源，后续可收敛到明确白名单。
		return true
	},
}

func NewHub(logger *slog.Logger, meetings MeetingService) *Hub {
	return &Hub{
		logger:             logger,
		meetings:           meetings,
		rooms:              make(map[string]map[string]*client),
		disconnectCleanups: make(map[string]*time.Timer),
	}
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request, meetingID string, participantID string) error {
	if participantID == "" {
		return fmt.Errorf("%w: participantId is required", ErrInvalidMessage)
	}

	meetingValue, found := h.meetings.GetMeeting(meetingID)
	if !found {
		return meeting.ErrMeetingNotFound
	}

	if _, ok := meetingValue.Participants[participantID]; !ok {
		return meeting.ErrParticipantNotFound
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Warn("websocket upgrade failed", "meetingId", meetingID, "participantId", participantID, "error", err)
		return fmt.Errorf("upgrade websocket: %w", err)
	}

	clientValue := &client{
		conn:          conn,
		hub:           h,
		meetingID:     meetingID,
		participantID: participantID,
		send:          make(chan serverEnvelope, 32),
	}

	onlineParticipantIDs := h.register(clientValue)
	h.logger.Info("websocket connected", "meetingId", meetingID, "participantId", participantID)
	clientValue.send <- serverEnvelope{
		Type: "session.welcome",
		Payload: welcomePayload{
			Meeting:              meetingValue,
			ParticipantID:        participantID,
			OnlineParticipantIDs: onlineParticipantIDs,
			ServerTime:           time.Now().UTC(),
		},
	}

	go clientValue.writePump()
	go clientValue.readPump()

	return nil
}

func (h *Hub) NotifyParticipantJoined(meetingID string, participant *meeting.Participant) {
	h.broadcast(meetingID, serverEnvelope{
		Type: "participant.joined",
		Payload: participantJoinedPayload{
			Participant: participant,
		},
	}, "")
}

func (h *Hub) NotifyParticipantLeft(meetingID string, participantID string) {
	h.broadcast(meetingID, serverEnvelope{
		Type: "participant.left",
		Payload: participantLeftPayload{
			ParticipantID: participantID,
		},
	}, "")
}

func (h *Hub) NotifyCapabilityGranted(meetingID string, grantedBy string, participantID string, capability meeting.Capability) {
	h.broadcast(meetingID, serverEnvelope{
		Type: "capability.granted",
		Payload: capabilityGrantedPayload{
			TargetParticipantID: participantID,
			GrantedBy:           grantedBy,
			Capability:          capability,
		},
	}, "")
}

func (h *Hub) NotifyAssistantAssigned(meetingID string, assignedBy string, participant *meeting.Participant) {
	h.broadcast(meetingID, serverEnvelope{
		Type: "role.assistant_assigned",
		Payload: assistantAssignedPayload{
			Participant: participant,
			AssignedBy:  assignedBy,
		},
	}, "")
}

func (h *Hub) NotifyNicknameUpdated(meetingID string, participant *meeting.Participant, previousNickname string, systemMessage *meeting.ChatMessage) {
	h.broadcast(meetingID, serverEnvelope{
		Type: "participant.nickname_updated",
		Payload: nicknameUpdatedPayload{
			Participant:      participant,
			PreviousNickname: previousNickname,
			SystemMessage:    systemMessage,
		},
	}, "")
}

func (h *Hub) NotifyWhiteboardAction(meetingID string, action meeting.WhiteboardAction) {
	h.broadcast(meetingID, serverEnvelope{
		Type: "whiteboard.action",
		Payload: whiteboardActionPayload{
			Action: action,
		},
	}, "")
}

func (h *Hub) NotifyWhiteboardCleared(meetingID string, clearedBy string) {
	h.broadcast(meetingID, serverEnvelope{
		Type: "whiteboard.cleared",
		Payload: map[string]string{
			"clearedBy": clearedBy,
		},
	}, "")
}

func (h *Hub) NotifyReadyCheckStarted(meetingID string, round *meeting.ReadyCheckRound) {
	h.broadcast(meetingID, serverEnvelope{
		Type: "ready_check.started",
		Payload: readyCheckRoundPayload{
			Round: round,
		},
	}, "")
}

func (h *Hub) NotifyReadyCheckUpdated(meetingID string, round *meeting.ReadyCheckRound) {
	h.broadcast(meetingID, serverEnvelope{
		Type: "ready_check.updated",
		Payload: readyCheckRoundPayload{
			Round: round,
		},
	}, "")
}

func (h *Hub) NotifyReadyCheckFinished(meetingID string, round *meeting.ReadyCheckRound) {
	h.broadcast(meetingID, serverEnvelope{
		Type: "ready_check.finished",
		Payload: readyCheckRoundPayload{
			Round: round,
		},
	}, "")
}

func (h *Hub) NotifyMeetingEnded(meetingID string, endedByParticipantID string) {
	h.broadcast(meetingID, serverEnvelope{
		Type: "meeting.ended",
		Payload: meetingEndedPayload{
			EndedByParticipantID: endedByParticipantID,
		},
	}, "")

	h.closeRoom(meetingID)
}

func (h *Hub) register(clientValue *client) []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.cancelDisconnectCleanupLocked(clientValue.meetingID, clientValue.participantID)

	room, ok := h.rooms[clientValue.meetingID]
	if !ok {
		room = make(map[string]*client)
		h.rooms[clientValue.meetingID] = room
	}

	room[clientValue.participantID] = clientValue

	onlineParticipantIDs := make([]string, 0, len(room))
	for participantID := range room {
		onlineParticipantIDs = append(onlineParticipantIDs, participantID)
	}

	go h.broadcast(clientValue.meetingID, serverEnvelope{
		Type: "participant.online",
		Payload: participantPresencePayload{
			ParticipantID: clientValue.participantID,
			Status:        "online",
		},
	}, clientValue.participantID)

	return onlineParticipantIDs
}

func (h *Hub) unregister(clientValue *client) {
	h.mu.Lock()

	room, ok := h.rooms[clientValue.meetingID]
	if !ok {
		h.mu.Unlock()
		return
	}

	currentClient, ok := room[clientValue.participantID]
	if !ok || currentClient != clientValue {
		h.mu.Unlock()
		return
	}

	delete(room, clientValue.participantID)
	close(clientValue.send)

	if len(room) == 0 {
		delete(h.rooms, clientValue.meetingID)
	} else {
		go h.broadcast(clientValue.meetingID, serverEnvelope{
			Type: "participant.offline",
			Payload: participantPresencePayload{
				ParticipantID: clientValue.participantID,
				Status:        "offline",
			},
		}, clientValue.participantID)
	}

	h.mu.Unlock()

	h.logger.Info("websocket disconnected", "meetingId", clientValue.meetingID, "participantId", clientValue.participantID)
	h.scheduleDisconnectCleanup(clientValue.meetingID, clientValue.participantID)
}

func (h *Hub) closeRoom(meetingID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.cancelDisconnectCleanupsByMeetingLocked(meetingID)

	room, ok := h.rooms[meetingID]
	if !ok {
		return
	}

	for participantID, clientValue := range room {
		close(clientValue.send)
		_ = clientValue.conn.Close()
		delete(room, participantID)
	}

	delete(h.rooms, meetingID)
}

func (h *Hub) broadcast(meetingID string, event serverEnvelope, excludeParticipantID string) {
	h.mu.RLock()
	room := h.rooms[meetingID]
	clients := make([]*client, 0, len(room))
	for participantID, clientValue := range room {
		if participantID == excludeParticipantID {
			continue
		}
		clients = append(clients, clientValue)
	}
	h.mu.RUnlock()

	for _, clientValue := range clients {
		select {
		case clientValue.send <- event:
		default:
			h.logger.Warn("dropping websocket event for slow client",
				"meetingId", meetingID,
				"participantId", clientValue.participantID,
				"type", event.Type,
			)
		}
	}
}

func (h *Hub) scheduleDisconnectCleanup(meetingID string, participantID string) {
	meetingValue, found := h.meetings.GetMeeting(meetingID)
	if !found {
		return
	}

	participant, ok := meetingValue.Participants[participantID]
	if !ok {
		return
	}

	if participant.Role == meeting.RoleHost {
		h.logger.Info("participant disconnect cleanup skipped", "meetingId", meetingID, "participantId", participantID, "reason", "host")
		return
	}

	key := disconnectCleanupKey(meetingID, participantID)
	timer := time.AfterFunc(disconnectGracePeriod, func() {
		h.runDisconnectCleanup(meetingID, participantID)
	})

	h.mu.Lock()
	if existing := h.disconnectCleanups[key]; existing != nil {
		existing.Stop()
	}
	h.disconnectCleanups[key] = timer
	h.mu.Unlock()

	h.logger.Info("participant disconnect cleanup scheduled", "meetingId", meetingID, "participantId", participantID, "gracePeriod", disconnectGracePeriod.String())
}

func (h *Hub) runDisconnectCleanup(meetingID string, participantID string) {
	h.mu.Lock()
	delete(h.disconnectCleanups, disconnectCleanupKey(meetingID, participantID))
	room := h.rooms[meetingID]
	if room != nil {
		if _, ok := room[participantID]; ok {
			h.mu.Unlock()
			h.logger.Info("participant disconnect cleanup canceled", "meetingId", meetingID, "participantId", participantID, "reason", "reconnected")
			return
		}
	}
	h.mu.Unlock()

	err := h.meetings.LeaveMeeting(context.Background(), meetingID, participantID, "signal_disconnect", "")
	switch {
	case err == nil:
		h.logger.Info("participant disconnect cleanup completed", "meetingId", meetingID, "participantId", participantID)
		h.NotifyParticipantLeft(meetingID, participantID)
	case errors.Is(err, meeting.ErrMeetingNotFound), errors.Is(err, meeting.ErrParticipantNotFound):
		h.logger.Info("participant disconnect cleanup skipped", "meetingId", meetingID, "participantId", participantID, "reason", "already_removed")
	case errors.Is(err, meeting.ErrUnauthorized):
		h.logger.Info("participant disconnect cleanup skipped", "meetingId", meetingID, "participantId", participantID, "reason", "unauthorized")
	default:
		h.logger.Error("participant disconnect cleanup failed", "meetingId", meetingID, "participantId", participantID, "error", err)
	}
}

func (h *Hub) cancelDisconnectCleanupLocked(meetingID string, participantID string) {
	key := disconnectCleanupKey(meetingID, participantID)
	if timer := h.disconnectCleanups[key]; timer != nil {
		timer.Stop()
		delete(h.disconnectCleanups, key)
	}
}

func (h *Hub) cancelDisconnectCleanupsByMeetingLocked(meetingID string) {
	for key, timer := range h.disconnectCleanups {
		if !strings.HasPrefix(key, meetingID+"\x00") {
			continue
		}
		timer.Stop()
		delete(h.disconnectCleanups, key)
	}
}

func disconnectCleanupKey(meetingID string, participantID string) string {
	return meetingID + "\x00" + participantID
}

func (h *Hub) sendToParticipant(meetingID string, participantID string, event serverEnvelope) error {
	h.mu.RLock()
	room := h.rooms[meetingID]
	clientValue, ok := room[participantID]
	h.mu.RUnlock()
	if !ok {
		return ErrParticipantOffline
	}

	select {
	case clientValue.send <- event:
		return nil
	default:
		return ErrParticipantOffline
	}
}

func (c *client) readPump() {
	defer func() {
		c.hub.unregister(c)
		_ = c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		var message envelope
		if err := c.conn.ReadJSON(&message); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.hub.logger.Warn("websocket read failed", "participantId", c.participantID, "error", err)
			}
			return
		}

		if err := c.handleMessage(message); err != nil {
			c.sendError("bad_request", err.Error())
		}
	}
}

func (c *client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteJSON(message); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *client) handleMessage(message envelope) error {
	switch message.Type {
	case "signal.offer", "signal.answer", "signal.ice_candidate":
		return c.handleTargetedSignal(message)
	case "capability.request":
		return c.handleCapabilityRequest(message.Payload)
	case "capability.grant":
		return c.handleCapabilityGrant(message.Payload)
	case "role.assign_assistant":
		return c.handleAssignAssistant(message.Payload)
	case "chat.message":
		return c.handleChatMessage(message.Payload)
	case "whiteboard.draw":
		return c.handleWhiteboardDraw(message.Payload)
	case "whiteboard.clear":
		return c.handleWhiteboardClear()
	case "ready_check.start":
		return c.handleReadyCheckStart(message.Payload)
	case "ready_check.respond":
		return c.handleReadyCheckRespond(message.Payload)
	default:
		return fmt.Errorf("%w: unsupported type %q", ErrInvalidMessage, message.Type)
	}
}

func (c *client) handleTargetedSignal(message envelope) error {
	var payload targetedPayload
	if err := json.Unmarshal(message.Payload, &payload); err != nil {
		return fmt.Errorf("%w: decode targeted payload: %v", ErrInvalidMessage, err)
	}

	if payload.TargetParticipantID == "" {
		return fmt.Errorf("%w: targetParticipantId is required", ErrInvalidMessage)
	}

	return c.hub.sendToParticipant(c.meetingID, payload.TargetParticipantID, serverEnvelope{
		Type: message.Type,
		Payload: forwardedSignalPayload{
			FromParticipantID: c.participantID,
			Data:              payload.Data,
		},
	})
}

func (c *client) handleCapabilityRequest(rawPayload json.RawMessage) error {
	meetingValue, participant, err := c.meetingParticipant()
	if err != nil {
		return err
	}

	var payload struct {
		Capability meeting.Capability `json:"capability"`
	}
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return fmt.Errorf("%w: decode capability request: %v", ErrInvalidMessage, err)
	}

	if payload.Capability == "" {
		return fmt.Errorf("%w: capability is required", ErrInvalidMessage)
	}

	return c.hub.sendToParticipant(c.meetingID, meetingValue.HostParticipantID, serverEnvelope{
		Type: "capability.requested",
		Payload: capabilityRequestPayload{
			FromParticipantID: participant.ID,
			Capability:        payload.Capability,
		},
	})
}

func (c *client) handleCapabilityGrant(rawPayload json.RawMessage) error {
	meetingValue, participant, err := c.meetingParticipant()
	if err != nil {
		return err
	}

	if participant.Role != meeting.RoleHost {
		return meeting.ErrUnauthorized
	}

	var payload struct {
		TargetParticipantID string             `json:"targetParticipantId"`
		Capability          meeting.Capability `json:"capability"`
	}
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return fmt.Errorf("%w: decode capability grant: %v", ErrInvalidMessage, err)
	}

	if payload.TargetParticipantID == "" || payload.Capability == "" {
		return fmt.Errorf("%w: targetParticipantId and capability are required", ErrInvalidMessage)
	}

	if err := c.hub.meetings.GrantCapability(context.Background(), meeting.GrantCapabilityInput{
		MeetingID:     meetingValue.ID,
		HostID:        participant.ID,
		ParticipantID: payload.TargetParticipantID,
		Capability:    payload.Capability,
	}); err != nil {
		return err
	}

	c.hub.NotifyCapabilityGranted(c.meetingID, participant.ID, payload.TargetParticipantID, payload.Capability)
	return nil
}

func (c *client) handleChatMessage(rawPayload json.RawMessage) error {
	_, participant, err := c.meetingParticipant()
	if err != nil {
		return err
	}

	if _, ok := participant.GrantedCapabilities[meeting.CapabilityChat]; !ok {
		return meeting.ErrUnauthorized
	}

	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return fmt.Errorf("%w: decode chat message: %v", ErrInvalidMessage, err)
	}

	if payload.Message == "" {
		return fmt.Errorf("%w: message is required", ErrInvalidMessage)
	}

	storedMessage, err := c.hub.meetings.AppendChatMessage(context.Background(), meeting.ChatMessageInput{
		MeetingID:     c.meetingID,
		ParticipantID: participant.ID,
		Message:       payload.Message,
	})
	if err != nil {
		return err
	}

	c.hub.broadcast(c.meetingID, serverEnvelope{
		Type: "chat.message",
		Payload: chatMessagePayload{
			Message: *storedMessage,
		},
	}, "")

	return nil
}

func (c *client) handleAssignAssistant(rawPayload json.RawMessage) error {
	meetingValue, participant, err := c.meetingParticipant()
	if err != nil {
		return err
	}

	if participant.Role != meeting.RoleHost {
		return meeting.ErrUnauthorized
	}

	var payload struct {
		TargetParticipantID string `json:"targetParticipantId"`
	}
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return fmt.Errorf("%w: decode assign assistant payload: %v", ErrInvalidMessage, err)
	}

	if payload.TargetParticipantID == "" {
		return fmt.Errorf("%w: targetParticipantId is required", ErrInvalidMessage)
	}

	assignedParticipant, err := c.hub.meetings.AssignAssistant(context.Background(), meeting.AssignAssistantInput{
		MeetingID:     meetingValue.ID,
		HostID:        participant.ID,
		ParticipantID: payload.TargetParticipantID,
	})
	if err != nil {
		return err
	}

	c.hub.NotifyAssistantAssigned(c.meetingID, participant.ID, assignedParticipant)
	return nil
}

func (c *client) handleWhiteboardDraw(rawPayload json.RawMessage) error {
	var payload struct {
		Action meeting.WhiteboardAction `json:"action"`
	}
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return fmt.Errorf("%w: decode whiteboard action payload: %v", ErrInvalidMessage, err)
	}

	action, err := c.hub.meetings.AppendWhiteboardAction(context.Background(), meeting.WhiteboardActionInput{
		MeetingID:     c.meetingID,
		ParticipantID: c.participantID,
		Action:        payload.Action,
	})
	if err != nil {
		return err
	}

	c.hub.NotifyWhiteboardAction(c.meetingID, *action)
	return nil
}

func (c *client) handleWhiteboardClear() error {
	if err := c.hub.meetings.ClearWhiteboard(context.Background(), meeting.ClearWhiteboardInput{
		MeetingID:     c.meetingID,
		ParticipantID: c.participantID,
	}); err != nil {
		return err
	}

	c.hub.NotifyWhiteboardCleared(c.meetingID, c.participantID)
	return nil
}

func (c *client) handleReadyCheckStart(rawPayload json.RawMessage) error {
	var payload struct {
		TimeoutSeconds int `json:"timeoutSeconds"`
	}
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return fmt.Errorf("%w: decode ready check start payload: %v", ErrInvalidMessage, err)
	}

	round, err := c.hub.meetings.StartReadyCheck(context.Background(), meeting.StartReadyCheckInput{
		MeetingID:      c.meetingID,
		ParticipantID:  c.participantID,
		TimeoutSeconds: payload.TimeoutSeconds,
	})
	if err != nil {
		return err
	}

	c.hub.NotifyReadyCheckStarted(c.meetingID, round)
	go c.hub.scheduleReadyCheckFinalize(c.meetingID, round)
	return nil
}

func (c *client) handleReadyCheckRespond(rawPayload json.RawMessage) error {
	var payload struct {
		Status meeting.ReadyCheckStatus `json:"status"`
	}
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return fmt.Errorf("%w: decode ready check response payload: %v", ErrInvalidMessage, err)
	}

	round, completed, err := c.hub.meetings.RespondReadyCheck(context.Background(), meeting.RespondReadyCheckInput{
		MeetingID:     c.meetingID,
		ParticipantID: c.participantID,
		Status:        payload.Status,
	})
	if err != nil {
		return err
	}

	if completed {
		c.hub.NotifyReadyCheckFinished(c.meetingID, round)
		return nil
	}

	c.hub.NotifyReadyCheckUpdated(c.meetingID, round)
	return nil
}

func (c *client) meetingParticipant() (*meeting.Meeting, *meeting.Participant, error) {
	meetingValue, found := c.hub.meetings.GetMeeting(c.meetingID)
	if !found {
		return nil, nil, meeting.ErrMeetingNotFound
	}

	participant, ok := meetingValue.Participants[c.participantID]
	if !ok {
		return nil, nil, meeting.ErrParticipantNotFound
	}

	return meetingValue, participant, nil
}

func (c *client) sendError(code string, message string) {
	select {
	case c.send <- serverEnvelope{
		Type: "error",
		Payload: outgoingErrorPayload{
			Code:    code,
			Message: message,
		},
	}:
	default:
	}
}

func (h *Hub) scheduleReadyCheckFinalize(meetingID string, round *meeting.ReadyCheckRound) {
	duration := time.Until(round.DeadlineAt)
	if duration > 0 {
		time.Sleep(duration)
	}

	finalizedRound, changed, err := h.meetings.FinalizeReadyCheck(context.Background(), meeting.FinalizeReadyCheckInput{
		MeetingID: meetingID,
		RoundID:   round.ID,
	})
	if err != nil {
		return
	}
	if !changed {
		return
	}

	h.NotifyReadyCheckFinished(meetingID, finalizedRound)
}
