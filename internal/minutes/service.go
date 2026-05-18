package minutes

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/misterchenleiya/meeting/internal/auth"
	"github.com/misterchenleiya/meeting/internal/storage/sqlite"
)

type Service struct {
	logger *slog.Logger
	store  Store
	mailer auth.Mailer
	asr    ASRProvider
	llm    LLMProvider
	config Config

	onJobCompleted func(job sqlite.MinutesJobRecord, record sqlite.MeetingMinutesRecord)

	mu                  sync.Mutex
	meetingUsageSeconds map[string]int
	dailyUsageSeconds   map[string]int
	meetingParticipants map[string]map[string]struct{}
	activeJobs          map[string]struct{}
}

func NewService(logger *slog.Logger, config Config, deps Dependencies) (*Service, error) {
	if deps.Store == nil {
		return nil, fmt.Errorf("minutes store is required")
	}
	if deps.ASR == nil {
		deps.ASR = FakeASRProvider{}
	}
	if deps.LLM == nil {
		deps.LLM = NewFakeLLMProvider("fake-minutes")
	}
	if config.DefaultLanguage == "" {
		config.DefaultLanguage = "zh-CN"
	}
	if config.ASRChunkMaxBytes <= 0 {
		config.ASRChunkMaxBytes = 1 << 20
	}
	if config.MeetingLimitSeconds <= 0 {
		config.MeetingLimitSeconds = 3600
	}
	if config.DailyLimitSeconds <= 0 {
		config.DailyLimitSeconds = 7200
	}
	if config.ConcurrentParticipantLimit <= 0 {
		config.ConcurrentParticipantLimit = 3
	}
	if config.MinutesJobTimeout <= 0 {
		config.MinutesJobTimeout = 10 * time.Minute
	}

	return &Service{
		logger:              logger,
		store:               deps.Store,
		mailer:              deps.Mailer,
		asr:                 deps.ASR,
		llm:                 deps.LLM,
		config:              config,
		onJobCompleted:      deps.OnJobCompleted,
		meetingUsageSeconds: make(map[string]int),
		dailyUsageSeconds:   make(map[string]int),
		meetingParticipants: make(map[string]map[string]struct{}),
		activeJobs:          make(map[string]struct{}),
	}, nil
}

func (s *Service) Enabled() bool {
	return s.config.Enabled
}

func (s *Service) MaxChunkBytes() int64 {
	return int64(s.config.ASRChunkMaxBytes)
}

func (s *Service) ProcessAudioChunk(ctx context.Context, input ProcessAudioChunkInput) (*sqlite.TranscriptSegmentRecord, error) {
	if !s.config.Enabled {
		return nil, ErrDisabled
	}
	if len(input.Data) == 0 || len(input.Data) > s.config.ASRChunkMaxBytes {
		return nil, ErrInvalidAudioChunk
	}
	if strings.TrimSpace(input.ParticipantID) == "" || strings.TrimSpace(input.MeetingID) == "" {
		return nil, ErrInvalidAudioChunk
	}
	startedAt := input.StartedAt.UTC()
	endedAt := input.EndedAt.UTC()
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	if endedAt.IsZero() || !endedAt.After(startedAt) {
		endedAt = startedAt.Add(time.Second)
	}
	durationSeconds := int(endedAt.Sub(startedAt).Seconds())
	if durationSeconds <= 0 {
		durationSeconds = 1
	}
	if err := s.reserveQuota(input.MeetingID, input.ParticipantID, durationSeconds); err != nil {
		return nil, err
	}

	language := strings.TrimSpace(input.Language)
	if language == "" {
		language = s.config.DefaultLanguage
	}
	chunk := AudioChunk{
		MeetingID:     input.MeetingID,
		MeetingNumber: input.MeetingNumber,
		ParticipantID: input.ParticipantID,
		UserID:        input.UserID,
		Nickname:      input.Nickname,
		Language:      language,
		Sequence:      input.Sequence,
		StartedAt:     startedAt,
		EndedAt:       endedAt,
		MimeType:      input.MimeType,
		SampleRate:    input.SampleRate,
		Data:          input.Data,
	}

	result, err := s.asr.Transcribe(ctx, chunk)
	if err != nil {
		return nil, err
	}

	text := strings.TrimSpace(result.Text)
	if text == "" {
		return nil, nil
	}

	now := time.Now().UTC()
	segment := sqlite.TranscriptSegmentRecord{
		ID:            randomID("seg"),
		MeetingID:     input.MeetingID,
		MeetingNumber: input.MeetingNumber,
		ParticipantID: input.ParticipantID,
		UserID:        input.UserID,
		Nickname:      input.Nickname,
		Language:      language,
		Sequence:      input.Sequence,
		StartedAt:     startedAt,
		EndedAt:       endedAt,
		Text:          text,
		IsFinal:       result.IsFinal,
		ASRProvider:   s.asr.Name(),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.store.InsertTranscriptSegment(ctx, segment); err != nil {
		return nil, err
	}

	return &segment, nil
}

func (s *Service) ListTranscriptSegments(ctx context.Context, meetingID string) ([]sqlite.TranscriptSegmentRecord, error) {
	return s.store.ListTranscriptSegments(ctx, meetingID)
}

func (s *Service) CreateMinutesJob(ctx context.Context, input CreateJobInput) (sqlite.MinutesJobRecord, bool, error) {
	if !s.config.Enabled {
		return sqlite.MinutesJobRecord{}, false, ErrDisabled
	}
	if strings.TrimSpace(input.MeetingID) == "" || strings.TrimSpace(input.RequestedByUserID) == "" {
		return sqlite.MinutesJobRecord{}, false, ErrInvalidAudioChunk
	}

	now := time.Now().UTC()
	record := sqlite.MinutesJobRecord{
		ID:                       randomID("job"),
		MeetingID:                input.MeetingID,
		MeetingNumber:            input.MeetingNumber,
		RequestedByUserID:        input.RequestedByUserID,
		RequestedByParticipantID: input.RequestedByParticipantID,
		Status:                   JobStatusPending,
		CreatedAt:                now,
		UpdatedAt:                now,
	}

	job, created, err := s.store.CreateMinutesJob(ctx, record)
	if err != nil {
		return sqlite.MinutesJobRecord{}, false, err
	}
	if created {
		s.enqueueJob(job.ID)
	}

	return job, created, nil
}

func (s *Service) GetMinutesJob(ctx context.Context, jobID string) (sqlite.MinutesJobRecord, bool, error) {
	return s.store.GetMinutesJob(ctx, jobID)
}

func (s *Service) GetMeetingMinutes(ctx context.Context, minutesID string, userID string) (sqlite.MeetingMinutesRecord, bool, error) {
	allowed, err := s.store.UserCanAccessMinutes(ctx, minutesID, userID)
	if err != nil {
		return sqlite.MeetingMinutesRecord{}, false, err
	}
	if !allowed {
		return sqlite.MeetingMinutesRecord{}, false, ErrUnauthorized
	}

	return s.store.GetMeetingMinutes(ctx, minutesID)
}

func (s *Service) ShareMeetingMinutes(ctx context.Context, minutesID string, hostUserID string, sharedWithUserID string) error {
	minutesRecord, found, err := s.store.GetMeetingMinutes(ctx, minutesID)
	if err != nil {
		return err
	}
	if !found {
		return ErrMinutesNotFound
	}
	if minutesRecord.HostUserID != hostUserID {
		return ErrUnauthorized
	}
	if strings.TrimSpace(sharedWithUserID) == "" || sharedWithUserID == hostUserID {
		return ErrUnauthorized
	}

	participants, err := s.store.ListRegisteredParticipantsForMeeting(ctx, minutesRecord.MeetingID)
	if err != nil {
		return err
	}
	allowed := false
	for _, participant := range participants {
		if participant.UserID == sharedWithUserID {
			allowed = true
			break
		}
	}
	if !allowed {
		return ErrUnauthorized
	}

	return s.store.UpsertMeetingMinutesShare(ctx, sqlite.MeetingMinutesShareRecord{
		MinutesID:        minutesID,
		SharedByUserID:   hostUserID,
		SharedWithUserID: sharedWithUserID,
		CreatedAt:        time.Now().UTC(),
	})
}

func (s *Service) RevokeMeetingMinutesShare(ctx context.Context, minutesID string, hostUserID string, sharedWithUserID string) error {
	minutesRecord, found, err := s.store.GetMeetingMinutes(ctx, minutesID)
	if err != nil {
		return err
	}
	if !found {
		return ErrMinutesNotFound
	}
	if minutesRecord.HostUserID != hostUserID {
		return ErrUnauthorized
	}

	return s.store.DeleteMeetingMinutesShare(ctx, minutesID, sharedWithUserID)
}

func (s *Service) ListUserMeetingHistory(ctx context.Context, userID string) ([]sqlite.UserMeetingHistoryRecord, error) {
	return s.store.ListUserMeetingHistory(ctx, userID)
}

func (s *Service) Start(ctx context.Context) {
	jobs, err := s.store.ListRunnableMinutesJobs(ctx)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("recover minutes jobs failed", "error", err)
		}
		return
	}
	for _, job := range jobs {
		s.enqueueJob(job.ID)
	}
	go func() {
		<-ctx.Done()
	}()
}

func (s *Service) reserveQuota(meetingID string, participantID string, durationSeconds int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	participants := s.meetingParticipants[meetingID]
	if participants == nil {
		participants = make(map[string]struct{})
		s.meetingParticipants[meetingID] = participants
	}
	if _, exists := participants[participantID]; !exists && len(participants) >= s.config.ConcurrentParticipantLimit {
		return ErrQuotaExceeded
	}

	dateKey := time.Now().UTC().Format("2006-01-02")
	if s.meetingUsageSeconds[meetingID]+durationSeconds > s.config.MeetingLimitSeconds {
		return ErrQuotaExceeded
	}
	if s.dailyUsageSeconds[dateKey]+durationSeconds > s.config.DailyLimitSeconds {
		return ErrQuotaExceeded
	}

	participants[participantID] = struct{}{}
	s.meetingUsageSeconds[meetingID] += durationSeconds
	s.dailyUsageSeconds[dateKey] += durationSeconds
	return nil
}

func (s *Service) enqueueJob(jobID string) {
	s.mu.Lock()
	if _, exists := s.activeJobs[jobID]; exists {
		s.mu.Unlock()
		return
	}
	s.activeJobs[jobID] = struct{}{}
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.activeJobs, jobID)
			s.mu.Unlock()
		}()
		s.runJob(context.Background(), jobID)
	}()
}

func (s *Service) runJob(ctx context.Context, jobID string) {
	job, found, err := s.store.GetMinutesJob(ctx, jobID)
	if err != nil || !found {
		if s.logger != nil {
			s.logger.Error("load minutes job failed", "jobId", jobID, "found", found, "error", err)
		}
		return
	}

	startedAt := time.Now().UTC()
	if err := s.store.UpdateMinutesJobStatus(ctx, job.ID, JobStatusRunning, "", "", &startedAt, nil, startedAt); err != nil {
		s.logJobError("mark minutes job running failed", job.ID, err)
		return
	}

	runCtx, cancel := context.WithTimeout(ctx, s.config.MinutesJobTimeout)
	defer cancel()

	minutesRecord, err := s.generateAndStoreMinutes(runCtx, job)
	completedAt := time.Now().UTC()
	if err != nil {
		_ = s.store.UpdateMinutesJobStatus(ctx, job.ID, JobStatusFailed, err.Error(), "", nil, &completedAt, completedAt)
		s.logJobError("minutes job failed", job.ID, err)
		return
	}

	emailError := s.sendHostEmail(ctx, job, minutesRecord)
	emailErrorMessage := ""
	if emailError != nil {
		emailErrorMessage = emailError.Error()
		s.logJobError("send minutes email failed", job.ID, emailError)
	}

	if err := s.store.UpdateMinutesJobStatus(ctx, job.ID, JobStatusSucceeded, "", emailErrorMessage, nil, &completedAt, completedAt); err != nil {
		s.logJobError("mark minutes job succeeded failed", job.ID, err)
		return
	}

	if s.onJobCompleted != nil {
		job.Status = JobStatusSucceeded
		job.CompletedAt = &completedAt
		job.EmailError = emailErrorMessage
		s.onJobCompleted(job, minutesRecord)
	}
}

func (s *Service) generateAndStoreMinutes(ctx context.Context, job sqlite.MinutesJobRecord) (sqlite.MeetingMinutesRecord, error) {
	segments, err := s.store.ListTranscriptSegments(ctx, job.MeetingID)
	if err != nil {
		return sqlite.MeetingMinutesRecord{}, err
	}
	if len(segments) == 0 {
		return sqlite.MeetingMinutesRecord{}, ErrNoTranscript
	}
	sort.SliceStable(segments, func(i int, j int) bool {
		if segments[i].StartedAt.Equal(segments[j].StartedAt) {
			return segments[i].Sequence < segments[j].Sequence
		}
		return segments[i].StartedAt.Before(segments[j].StartedAt)
	})

	meetingRecord, found, err := s.store.GetMeetingUsageByID(ctx, job.MeetingID)
	if err != nil {
		return sqlite.MeetingMinutesRecord{}, err
	}
	if !found {
		return sqlite.MeetingMinutesRecord{}, fmt.Errorf("meeting usage not found")
	}

	participants, err := s.store.ListRegisteredParticipantsForMeeting(ctx, job.MeetingID)
	if err != nil {
		return sqlite.MeetingMinutesRecord{}, err
	}

	result, err := s.llm.GenerateMinutes(ctx, GenerateMinutesInput{
		Meeting:      meetingRecord,
		Participants: participants,
		Segments:     segments,
	})
	if err != nil {
		return sqlite.MeetingMinutesRecord{}, err
	}

	now := time.Now().UTC()
	record := sqlite.MeetingMinutesRecord{
		ID:              randomID("min"),
		JobID:           job.ID,
		MeetingID:       job.MeetingID,
		MeetingNumber:   job.MeetingNumber,
		HostUserID:      job.RequestedByUserID,
		Title:           result.Title,
		Summary:         result.Summary,
		MarkdownContent: result.MarkdownContent,
		OutlineJSON:     result.OutlineJSON,
		LLMProvider:     s.llm.Name(),
		LLMModel:        s.llm.Model(),
		GeneratedAt:     now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.store.InsertMeetingMinutes(ctx, record); err != nil {
		return sqlite.MeetingMinutesRecord{}, err
	}

	return record, nil
}

func (s *Service) sendHostEmail(ctx context.Context, job sqlite.MinutesJobRecord, record sqlite.MeetingMinutesRecord) error {
	if s.mailer == nil {
		return nil
	}

	meetingRecord, found, err := s.store.GetMeetingUsageByID(ctx, job.MeetingID)
	if err != nil {
		return err
	}
	if !found || strings.TrimSpace(meetingRecord.HostEmail) == "" {
		return nil
	}

	subject := record.Title
	if strings.TrimSpace(subject) == "" {
		subject = "会议纪要：" + meetingRecord.Title
	}

	_, err = s.mailer.SendEmail(ctx, auth.EmailMessage{
		To:             []string{meetingRecord.HostEmail},
		Subject:        subject,
		TextBody:       record.MarkdownContent,
		HTMLBody:       markdownEmailHTML(record),
		ContentSummary: record.Summary,
	})
	return err
}

func markdownEmailHTML(record sqlite.MeetingMinutesRecord) string {
	escaped := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	).Replace(record.MarkdownContent)
	return "<pre style=\"white-space:pre-wrap;font-family:Menlo,Consolas,monospace;line-height:1.6\">" + escaped + "</pre>"
}

func (s *Service) logJobError(message string, jobID string, err error) {
	if s.logger != nil && err != nil {
		s.logger.Error(message, "jobId", jobID, "error", err)
	}
}

func randomID(prefix string) string {
	var buffer [8]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(buffer[:])
}

func IsUnauthorized(err error) bool {
	return errors.Is(err, ErrUnauthorized)
}
