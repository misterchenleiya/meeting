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
	newUsers                []sqlite.UserRegistrationRecord
	emailCodeLogins         []sqlite.EmailCodeLoginRecord
	auditEvents             []sqlite.AuditEvent
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

func (s fakeReportStore) ListUsersCreatedWindow(_ context.Context, _ time.Time, _ time.Time) ([]sqlite.UserRegistrationRecord, error) {
	return s.newUsers, nil
}

func (s fakeReportStore) ListEmailCodeLoginsWindow(_ context.Context, _ time.Time, _ time.Time) ([]sqlite.EmailCodeLoginRecord, error) {
	return s.emailCodeLogins, nil
}

func (s fakeReportStore) ListAuditEventsWindow(_ context.Context, _ time.Time, _ time.Time) ([]sqlite.AuditEvent, error) {
	return s.auditEvents, nil
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
				MeetingID:         "mtg_001",
				ParticipantID:     "host_001",
				UserID:            "usr_host",
				Email:             "host@example.com",
				Nickname:          "主持人",
				IPAddress:         "203.0.113.10",
				ClientProfileJSON: `{"browser":"Chrome","os":"macOS","deviceCategory":"desktop","networkEffectiveType":"4g"}`,
				ParticipantRole:   "host",
				JoinedAt:          windowStart,
			},
			{
				MeetingID:         "mtg_001",
				ParticipantID:     "anon_001",
				Nickname:          "匿名用户",
				IsAnonymous:       true,
				IPAddress:         "203.0.113.20",
				DeviceType:        "browser",
				ClientProfileJSON: `{"browser":"Safari","os":"iOS","deviceCategory":"mobile","networkEffectiveType":"4g"}`,
				ParticipantRole:   "participant",
				JoinedAt:          windowStart.Add(5 * time.Minute),
			},
		},
		newUsers: []sqlite.UserRegistrationRecord{{
			Email:     "new-user@example.com",
			Nickname:  "新用户",
			IPAddress: "203.0.113.30",
			CreatedAt: windowStart.Add(30 * time.Minute),
		}},
		emailCodeLogins: []sqlite.EmailCodeLoginRecord{
			{
				Email:     "host@example.com",
				IPAddress: "203.0.113.10",
				LoginAt:   windowStart.Add(10 * time.Minute),
			},
			{
				Email:     "new-user@example.com",
				IPAddress: "203.0.113.30",
				LoginAt:   windowStart.Add(40 * time.Minute),
			},
		},
		auditEvents: []sqlite.AuditEvent{
			{
				MeetingID:        "mtg_001",
				ParticipantID:    "anon_001",
				ParticipantRole:  "participant",
				EventType:        "media_report",
				DeviceType:       "browser",
				LatencyMS:        320,
				PacketLossRate:   0.08,
				AverageFPS:       10,
				AverageBitrateKB: 640,
				CreatedAt:        windowStart.Add(15 * time.Minute),
			},
			{
				MeetingID:        "mtg_001",
				ParticipantID:    "host_001",
				ParticipantRole:  "host",
				EventType:        "media_report",
				DeviceType:       "browser",
				LatencyMS:        40,
				PacketLossRate:   0.01,
				AverageFPS:       24,
				AverageBitrateKB: 1800,
				CreatedAt:        windowStart.Add(15 * time.Minute),
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

	if len(mailer.message.Attachments) != 5 {
		t.Fatalf("attachment count = %d, want 5", len(mailer.message.Attachments))
	}
	if !strings.Contains(mailer.message.TextBody, "commit: abcdef123456") {
		t.Fatalf("email body missing build info: %s", mailer.message.TextBody)
	}
	if !strings.Contains(mailer.message.TextBody, "用户访问数量 | 2") {
		t.Fatalf("email text body missing summary table: %s", mailer.message.TextBody)
	}
	if !strings.Contains(mailer.message.TextBody, "新增用户数 | 1") || !strings.Contains(mailer.message.TextBody, "邮件验证码登录次数 | 2") {
		t.Fatalf("email text body missing auth summary: %s", mailer.message.TextBody)
	}
	if !strings.Contains(mailer.message.TextBody, "会议质量样本数 | 2") || !strings.Contains(mailer.message.TextBody, "客户端设备分布 | desktop: 1；mobile: 1") {
		t.Fatalf("email text body missing quality or client profile summary: %s", mailer.message.TextBody)
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
	if mailer.message.Attachments[2].Filename != "new_users.csv" {
		t.Fatalf("third attachment filename = %q, want new_users.csv", mailer.message.Attachments[2].Filename)
	}
	newUsers := string(mailer.message.Attachments[2].Data)
	if !strings.Contains(newUsers, "new-user@example.com") || !strings.Contains(newUsers, "203.0.113.30") || !strings.Contains(newUsers, "新用户") {
		t.Fatalf("new users csv = %s", newUsers)
	}
	if mailer.message.Attachments[3].Filename != "email_code_logins.csv" {
		t.Fatalf("fourth attachment filename = %q, want email_code_logins.csv", mailer.message.Attachments[3].Filename)
	}
	logins := string(mailer.message.Attachments[3].Data)
	if !strings.Contains(logins, "host@example.com") || !strings.Contains(logins, "203.0.113.30") {
		t.Fatalf("email code logins csv = %s", logins)
	}
	if mailer.message.Attachments[4].Filename != "meeting_quality.csv" {
		t.Fatalf("fifth attachment filename = %q, want meeting_quality.csv", mailer.message.Attachments[4].Filename)
	}
	quality := string(mailer.message.Attachments[4].Data)
	if !strings.Contains(quality, "anon_001") || !strings.Contains(quality, "320") || !strings.Contains(quality, "1") {
		t.Fatalf("meeting quality csv = %s", quality)
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

func TestReporterTreatsAuthActivityAsUsage(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	mailer := &fakeReportMailer{}
	reporter, err := NewReporter(nil, fakeReportStore{
		newUsers: []sqlite.UserRegistrationRecord{{
			Email:     "new-user@example.com",
			Nickname:  "新用户",
			IPAddress: "203.0.113.10",
			CreatedAt: start.Add(time.Hour),
		}},
		emailCodeLogins: []sqlite.EmailCodeLoginRecord{{
			Email:     "new-user@example.com",
			IPAddress: "203.0.113.10",
			LoginAt:   start.Add(2 * time.Hour),
		}},
	}, mailer, Config{
		Recipients: []string{"ops@example.com"},
	})
	if err != nil {
		t.Fatalf("NewReporter() error = %v", err)
	}

	if err := reporter.SendWindow(context.Background(), start, start.Add(24*time.Hour)); err != nil {
		t.Fatalf("SendWindow() error = %v", err)
	}

	if len(mailer.message.Attachments) != 5 {
		t.Fatalf("attachment count = %d, want 5", len(mailer.message.Attachments))
	}
	if strings.Contains(mailer.message.TextBody, "过去 24 小时没有任何使用数据") {
		t.Fatalf("email body should not be no-data message: %s", mailer.message.TextBody)
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
