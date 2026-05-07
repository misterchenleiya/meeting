package meeting

import "time"

type Role string

const (
	RoleHost        Role = "host"
	RoleAssistant   Role = "assistant"
	RoleParticipant Role = "participant"
)

type Capability string

const (
	CapabilityChat        Capability = "chat"
	CapabilityMicrophone  Capability = "microphone"
	CapabilityCamera      Capability = "camera"
	CapabilityWhiteboard  Capability = "whiteboard"
	CapabilityScreenShare Capability = "screen_share"
	CapabilityRecord      Capability = "record"
	CapabilityReadyCheck  Capability = "ready_check"
)

type Status string

const (
	StatusActive Status = "active"
	StatusEnded  Status = "ended"
)

type MeetingType string

const (
	MeetingTypeQuick     MeetingType = "quick"
	MeetingTypeScheduled MeetingType = "scheduled"
)

type MediaPreference struct {
	CameraEnabled     bool `json:"cameraEnabled"`
	MicrophoneEnabled bool `json:"microphoneEnabled"`
}

type ChatMessageKind string

const (
	ChatMessageKindUser              ChatMessageKind = "user"
	ChatMessageKindSystem            ChatMessageKind = "system"
	ChatMessageKindCapabilityRequest ChatMessageKind = "capability_request"
)

type ChatMessageActionType string

const (
	ChatMessageActionTypeOpenPermissions ChatMessageActionType = "open_permissions"
)

type ChatMessageAction struct {
	Type                ChatMessageActionType `json:"type"`
	TargetParticipantID string                `json:"targetParticipantId"`
	Capability          Capability            `json:"capability"`
	RequestedBy         string                `json:"requestedBy"`
}

type ChatMessage struct {
	ID            string             `json:"id"`
	ParticipantID string             `json:"participantId"`
	Nickname      string             `json:"nickname"`
	Kind          ChatMessageKind    `json:"kind"`
	Message       string             `json:"message"`
	Action        *ChatMessageAction `json:"action,omitempty"`
	SentAt        time.Time          `json:"sentAt"`
}

type WhiteboardPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type WhiteboardAction struct {
	ID          string            `json:"id"`
	Kind        string            `json:"kind"`
	Color       string            `json:"color"`
	StrokeWidth float64           `json:"strokeWidth"`
	Points      []WhiteboardPoint `json:"points"`
	CreatedBy   string            `json:"createdBy"`
	CreatedAt   time.Time         `json:"createdAt"`
}

type ReadyCheckStatus string

const (
	ReadyCheckPending   ReadyCheckStatus = "pending"
	ReadyCheckConfirmed ReadyCheckStatus = "confirmed"
	ReadyCheckCancelled ReadyCheckStatus = "cancelled"
	ReadyCheckTimeout   ReadyCheckStatus = "timeout"
)

type ReadyCheckResult struct {
	ParticipantID string           `json:"participantId"`
	Status        ReadyCheckStatus `json:"status"`
	UpdatedAt     *time.Time       `json:"updatedAt,omitempty"`
}

type ReadyCheckRound struct {
	ID             string                      `json:"id"`
	StartedBy      string                      `json:"startedBy"`
	TimeoutSeconds int                         `json:"timeoutSeconds"`
	StartedAt      time.Time                   `json:"startedAt"`
	DeadlineAt     time.Time                   `json:"deadlineAt"`
	Status         string                      `json:"status"`
	Results        map[string]ReadyCheckResult `json:"results"`
}

type Participant struct {
	ID                       string                  `json:"id"`
	UserID                   string                  `json:"userId,omitempty"`
	Nickname                 string                  `json:"nickname"`
	Role                     Role                    `json:"role"`
	IsAnonymous              bool                    `json:"isAnonymous"`
	JoinedAt                 time.Time               `json:"joinedAt"`
	LeftAt                   *time.Time              `json:"leftAt,omitempty"`
	RequestedMediaPreference MediaPreference         `json:"requestedMediaPreference"`
	EffectiveMediaState      MediaPreference         `json:"effectiveMediaState"`
	GrantedCapabilities      map[Capability]struct{} `json:"grantedCapabilities"`
}

type Meeting struct {
	ID                string                  `json:"id"`
	MeetingNumber     string                  `json:"meetingNumber"`
	JoinCode          string                  `json:"joinCode"`
	PasswordRequired  bool                    `json:"passwordRequired"`
	Title             string                  `json:"title"`
	MeetingType       MeetingType             `json:"meetingType"`
	HostParticipantID string                  `json:"hostParticipantId"`
	Status            Status                  `json:"status"`
	CreatedAt         time.Time               `json:"createdAt"`
	EndedAt           *time.Time              `json:"endedAt,omitempty"`
	PasswordSalt      string                  `json:"-"`
	PasswordHash      string                  `json:"-"`
	Participants      map[string]*Participant `json:"participants"`
	ChatMessages      []ChatMessage           `json:"chatMessages,omitempty"`
	WhiteboardActions []WhiteboardAction      `json:"whiteboardActions,omitempty"`
	ActiveReadyCheck  *ReadyCheckRound        `json:"activeReadyCheck,omitempty"`
	TemporaryMinutes  []string                `json:"temporaryMinutes,omitempty"`
}

func allCapabilities() []Capability {
	return []Capability{
		CapabilityChat,
		CapabilityMicrophone,
		CapabilityCamera,
		CapabilityWhiteboard,
		CapabilityScreenShare,
		CapabilityRecord,
		CapabilityReadyCheck,
	}
}

func hostGrantableCapabilities() []Capability {
	return []Capability{
		CapabilityMicrophone,
		CapabilityCamera,
		CapabilityWhiteboard,
		CapabilityScreenShare,
		CapabilityRecord,
		CapabilityReadyCheck,
	}
}

func requestableParticipantCapabilities() []Capability {
	return []Capability{
		CapabilityMicrophone,
		CapabilityCamera,
		CapabilityScreenShare,
		CapabilityRecord,
	}
}
