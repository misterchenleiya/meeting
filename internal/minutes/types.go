package minutes

import (
	"context"
	"errors"
	"time"

	"github.com/misterchenleiya/meeting/internal/auth"
	"github.com/misterchenleiya/meeting/internal/storage/sqlite"
)

const (
	ProviderFake     = "fake"
	ProviderTencent  = "tencent"
	ProviderDeepSeek = "deepseek"

	JobStatusPending           = "pending"
	JobStatusWaitingTranscript = "waiting_transcript"
	JobStatusRunning           = "running"
	JobStatusSucceeded         = "succeeded"
	JobStatusFailed            = "failed"
)

var (
	ErrDisabled          = errors.New("transcription is disabled")
	ErrInvalidAudioChunk = errors.New("invalid audio chunk")
	ErrQuotaExceeded     = errors.New("transcription quota exceeded")
	ErrProviderConfig    = errors.New("provider config is incomplete")
	ErrMinutesNotFound   = errors.New("meeting minutes not found")
	ErrNoTranscript      = errors.New("meeting transcript is empty")
	ErrUnauthorized      = errors.New("unauthorized minutes access")
)

type Store interface {
	InsertTranscriptSegment(ctx context.Context, record sqlite.TranscriptSegmentRecord) error
	ListTranscriptSegments(ctx context.Context, meetingID string) ([]sqlite.TranscriptSegmentRecord, error)
	CreateMinutesJob(ctx context.Context, record sqlite.MinutesJobRecord) (sqlite.MinutesJobRecord, bool, error)
	GetMinutesJob(ctx context.Context, jobID string) (sqlite.MinutesJobRecord, bool, error)
	GetActiveOrSucceededMinutesJob(ctx context.Context, meetingID string) (sqlite.MinutesJobRecord, bool, error)
	ListRunnableMinutesJobs(ctx context.Context) ([]sqlite.MinutesJobRecord, error)
	UpdateMinutesJobStatus(ctx context.Context, jobID string, status string, errorMessage string, emailError string, startedAt *time.Time, completedAt *time.Time, updatedAt time.Time) error
	InsertMeetingMinutes(ctx context.Context, record sqlite.MeetingMinutesRecord) error
	GetMeetingMinutes(ctx context.Context, minutesID string) (sqlite.MeetingMinutesRecord, bool, error)
	GetMeetingMinutesByJob(ctx context.Context, jobID string) (sqlite.MeetingMinutesRecord, bool, error)
	UpsertMeetingMinutesShare(ctx context.Context, record sqlite.MeetingMinutesShareRecord) error
	DeleteMeetingMinutesShare(ctx context.Context, minutesID string, sharedWithUserID string) error
	UserCanAccessMinutes(ctx context.Context, minutesID string, userID string) (bool, error)
	GetMeetingUsageByID(ctx context.Context, meetingID string) (sqlite.MeetingUsageRecord, bool, error)
	ListRegisteredParticipantsForMeeting(ctx context.Context, meetingID string) ([]sqlite.MeetingParticipantUsageRecord, error)
	ListUserMeetingHistory(ctx context.Context, userID string) ([]sqlite.UserMeetingHistoryRecord, error)
}

type ASRProvider interface {
	Name() string
	Transcribe(ctx context.Context, chunk AudioChunk) (ASRResult, error)
}

type LLMProvider interface {
	Name() string
	Model() string
	GenerateMinutes(ctx context.Context, input GenerateMinutesInput) (GenerateMinutesResult, error)
}

type AudioChunk struct {
	MeetingID     string
	MeetingNumber string
	ParticipantID string
	UserID        string
	Nickname      string
	Language      string
	Sequence      int64
	StartedAt     time.Time
	EndedAt       time.Time
	MimeType      string
	SampleRate    int
	Data          []byte
}

type ASRResult struct {
	Text        string
	IsFinal     bool
	DurationMS  int
	ProviderRef string
}

type GenerateMinutesInput struct {
	Meeting      sqlite.MeetingUsageRecord
	Participants []sqlite.MeetingParticipantUsageRecord
	Segments     []sqlite.TranscriptSegmentRecord
}

type GenerateMinutesResult struct {
	Title           string
	Summary         string
	MarkdownContent string
	OutlineJSON     string
}

type Config struct {
	Enabled                    bool
	ASRProvider                string
	ASRChunkMaxBytes           int
	DefaultLanguage            string
	MeetingLimitSeconds        int
	DailyLimitSeconds          int
	ConcurrentParticipantLimit int
	MinutesJobTimeout          time.Duration
}

type Dependencies struct {
	Store          Store
	Mailer         auth.Mailer
	ASR            ASRProvider
	LLM            LLMProvider
	OnJobCompleted func(job sqlite.MinutesJobRecord, record sqlite.MeetingMinutesRecord)
}

type ProcessAudioChunkInput struct {
	MeetingID     string
	MeetingNumber string
	ParticipantID string
	UserID        string
	Nickname      string
	Language      string
	Sequence      int64
	StartedAt     time.Time
	EndedAt       time.Time
	MimeType      string
	SampleRate    int
	Data          []byte
}

type CreateJobInput struct {
	MeetingID                string
	MeetingNumber            string
	RequestedByUserID        string
	RequestedByParticipantID string
}
