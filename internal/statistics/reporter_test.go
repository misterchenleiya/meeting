package statistics

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/misterchenleiya/meeting/internal/auth"
	"github.com/misterchenleiya/meeting/internal/buildinfo"
	"github.com/misterchenleiya/meeting/internal/storage/sqlite"
)

type fakeReportStore struct {
	meetings                []sqlite.MeetingUsageRecord
	participantsInWindow    []sqlite.MeetingParticipantUsageRecord
	participantsForMeetings []sqlite.MeetingParticipantUsageRecord
}

func (s fakeReportStore) ListMeetingUsageWindow(_ context.Context, _ time.Time, _ time.Time) ([]sqlite.MeetingUsageRecord, error) {
	return s.meetings, nil
}

func (s fakeReportStore) ListParticipantUsageWindow(_ context.Context, _ time.Time, _ time.Time) ([]sqlite.MeetingParticipantUsageRecord, error) {
	return s.participantsInWindow, nil
}

func (s fakeReportStore) ListParticipantsForMeetings(_ context.Context, _ []string) ([]sqlite.MeetingParticipantUsageRecord, error) {
	return s.participantsForMeetings, nil
}

type fakeReportMailer struct {
	message auth.EmailMessage
}

func (m *fakeReportMailer) SendVerificationCode(_ context.Context, _ auth.VerificationMessage) (auth.MailDelivery, error) {
	return auth.MailDelivery{Mode: auth.MailerModeDebug}, nil
}

func (m *fakeReportMailer) SendEmail(_ context.Context, message auth.EmailMessage) (auth.MailDelivery, error) {
	m.message = message
	return auth.MailDelivery{Mode: auth.MailerModeDebug}, nil
}

func TestReporterSendsSummaryTableAndDetailCSVAttachmentsWhenUsageExists(t *testing.T) {
	t.Parallel()

	windowStart := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(24 * time.Hour)
	endedAt := windowStart.Add(90 * time.Minute)
	store := fakeReportStore{
		meetings: []sqlite.MeetingUsageRecord{{
			ID:                "mtg_001",
			MeetingNumber:     "123456789",
			Title:             "daily sync",
			MeetingType:       "scheduled",
			HostParticipantID: "host_001",
			HostUserID:        "usr_host",
			HostEmail:         "host@example.com",
			HostNickname:      "主持人",
			HostIPAddress:     "203.0.113.10",
			CreatedAt:         windowStart,
			EndedAt:           &endedAt,
			UpdatedAt:         endedAt,
		}},
		participantsInWindow: []sqlite.MeetingParticipantUsageRecord{
			{
				MeetingID:       "mtg_001",
				ParticipantID:   "host_001",
				UserID:          "usr_host",
				Email:           "host@example.com",
				Nickname:        "主持人",
				IPAddress:       "203.0.113.10",
				ParticipantRole: "host",
				JoinedAt:        windowStart,
			},
			{
				MeetingID:       "mtg_001",
				ParticipantID:   "anon_001",
				Nickname:        "匿名用户",
				IsAnonymous:     true,
				IPAddress:       "203.0.113.20",
				DeviceType:      "browser",
				ParticipantRole: "participant",
				JoinedAt:        windowStart.Add(5 * time.Minute),
			},
		},
	}
	store.participantsForMeetings = store.participantsInWindow
	mailer := &fakeReportMailer{}
	reporter, err := NewReporter(nil, store, mailer, Config{
		Recipients: []string{"ops@example.com"},
		SendAtUTC:  "12:00",
		BuildInfo: buildinfo.Info{
			Tag:       "v1.2.3",
			Commit:    "abcdef123456",
			BuildTime: "2026-05-06 12:00:00 +0800",
		},
	})
	if err != nil {
		t.Fatalf("NewReporter() error = %v", err)
	}

	if err := reporter.SendWindow(context.Background(), windowStart, windowEnd); err != nil {
		t.Fatalf("SendWindow() error = %v", err)
	}

	if len(mailer.message.Attachments) != 2 {
		t.Fatalf("attachment count = %d, want 2", len(mailer.message.Attachments))
	}
	if !strings.Contains(mailer.message.TextBody, "commit: abcdef123456") {
		t.Fatalf("email body missing build info: %s", mailer.message.TextBody)
	}
	if !strings.Contains(mailer.message.TextBody, "用户访问数量 | 2") {
		t.Fatalf("email text body missing summary table: %s", mailer.message.TextBody)
	}
	if !strings.Contains(mailer.message.HTMLBody, "<table") || !strings.Contains(mailer.message.HTMLBody, "用户访问数量") {
		t.Fatalf("email html body missing summary table: %s", mailer.message.HTMLBody)
	}
	if mailer.message.Attachments[0].Filename != "users.csv" {
		t.Fatalf("first attachment filename = %q, want users.csv", mailer.message.Attachments[0].Filename)
	}
	users := string(mailer.message.Attachments[0].Data)
	if !strings.Contains(users, "host@example.com") || !strings.Contains(users, "匿名用户") {
		t.Fatalf("users csv = %s", users)
	}
	if mailer.message.Attachments[1].Filename != "meetings.csv" {
		t.Fatalf("second attachment filename = %q, want meetings.csv", mailer.message.Attachments[1].Filename)
	}
	meetings := string(mailer.message.Attachments[1].Data)
	if !strings.Contains(meetings, "mtg_001") || !strings.Contains(meetings, "scheduled") {
		t.Fatalf("meetings csv = %s", meetings)
	}
}

func TestReporterSendsNoAttachmentEmailWhenNoUsageExists(t *testing.T) {
	t.Parallel()

	mailer := &fakeReportMailer{}
	reporter, err := NewReporter(nil, fakeReportStore{}, mailer, Config{
		Recipients: []string{"ops@example.com"},
		SendAtUTC:  "",
		BuildInfo:  buildinfo.Info{Tag: "untagged", Commit: "abc", BuildTime: "now"},
	})
	if err != nil {
		t.Fatalf("NewReporter() error = %v", err)
	}

	start := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	if err := reporter.SendWindow(context.Background(), start, start.Add(24*time.Hour)); err != nil {
		t.Fatalf("SendWindow() error = %v", err)
	}

	if len(mailer.message.Attachments) != 0 {
		t.Fatalf("attachment count = %d, want 0", len(mailer.message.Attachments))
	}
	if !strings.Contains(mailer.message.TextBody, "过去 24 小时没有任何使用数据") {
		t.Fatalf("email body = %s", mailer.message.TextBody)
	}
	if !strings.Contains(mailer.message.HTMLBody, "过去 24 小时没有任何使用数据") {
		t.Fatalf("email html body = %s", mailer.message.HTMLBody)
	}
}

func TestNextDailyRunUsesConfiguredUTCTime(t *testing.T) {
	t.Parallel()

	sendAt, err := parseSendAtUTC("12:00")
	if err != nil {
		t.Fatalf("parseSendAtUTC() error = %v", err)
	}
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	next := nextDailyRun(now, sendAt)
	want := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("nextDailyRun() = %s, want %s", next, want)
	}
}
