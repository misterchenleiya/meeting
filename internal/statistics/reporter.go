package statistics

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"html"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/misterchenleiya/meeting/internal/auth"
	"github.com/misterchenleiya/meeting/internal/buildinfo"
	"github.com/misterchenleiya/meeting/internal/storage/sqlite"
)

const defaultSendAtUTC = "12:00"

type Store interface {
	ListMeetingUsageWindow(ctx context.Context, start time.Time, end time.Time) ([]sqlite.MeetingUsageRecord, error)
	ListParticipantUsageWindow(ctx context.Context, start time.Time, end time.Time) ([]sqlite.MeetingParticipantUsageRecord, error)
	ListParticipantsForMeetings(ctx context.Context, meetingIDs []string) ([]sqlite.MeetingParticipantUsageRecord, error)
}

type Reporter struct {
	logger     *slog.Logger
	store      Store
	mailer     auth.Mailer
	recipients []string
	sendAt     time.Duration
	buildInfo  buildinfo.Info
	now        func() time.Time
}

type Config struct {
	Recipients []string
	SendAtUTC  string
	BuildInfo  buildinfo.Info
	Now        func() time.Time
}

func NewReporter(logger *slog.Logger, store Store, mailer auth.Mailer, config Config) (*Reporter, error) {
	recipients := normalizeRecipients(config.Recipients)
	sendAt, err := parseSendAtUTC(config.SendAtUTC)
	if err != nil {
		return nil, err
	}
	now := config.Now
	if now == nil {
		now = func() time.Time {
			return time.Now().UTC()
		}
	}

	return &Reporter{
		logger:     logger,
		store:      store,
		mailer:     mailer,
		recipients: recipients,
		sendAt:     sendAt,
		buildInfo:  config.BuildInfo,
		now:        now,
	}, nil
}

func (r *Reporter) Enabled() bool {
	return len(r.recipients) > 0
}

func (r *Reporter) Run(ctx context.Context) {
	if !r.Enabled() {
		if r.logger != nil {
			r.logger.Info("traffic statistics report disabled")
		}
		return
	}

	for {
		nextRun := nextDailyRun(r.now().UTC(), r.sendAt)
		timer := time.NewTimer(time.Until(nextRun))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			windowEnd := r.now().UTC()
			windowStart := windowEnd.Add(-24 * time.Hour)
			if err := r.SendWindow(ctx, windowStart, windowEnd); err != nil {
				if r.logger != nil {
					r.logger.Error("traffic statistics report failed", "error", err, "windowStart", windowStart, "windowEnd", windowEnd)
				}
			}
		}
	}
}

func (r *Reporter) SendWindow(ctx context.Context, start time.Time, end time.Time) error {
	if !r.Enabled() {
		return nil
	}

	meetings, err := r.store.ListMeetingUsageWindow(ctx, start, end)
	if err != nil {
		return err
	}
	participantsInWindow, err := r.store.ListParticipantUsageWindow(ctx, start, end)
	if err != nil {
		return err
	}

	attachments := []auth.MailAttachment(nil)
	summaryRows := buildSummaryRows(start, end, meetings, participantsInWindow)
	textBody := r.buildTextBody(start, end, len(meetings), len(participantsInWindow), summaryRows)
	htmlBody := r.buildHTMLBody(start, end, len(meetings), len(participantsInWindow), summaryRows)
	if len(meetings) > 0 || len(participantsInWindow) > 0 {
		meetingParticipants, err := r.store.ListParticipantsForMeetings(ctx, meetingIDs(meetings))
		if err != nil {
			return err
		}
		attachments, err = csvAttachments(
			csvAttachmentInput{filename: "users.csv", rows: buildUserRows(meetings, participantsInWindow)},
			csvAttachmentInput{filename: "meetings.csv", rows: buildMeetingRows(meetings, meetingParticipants)},
		)
		if err != nil {
			return err
		}
	}

	subject := fmt.Sprintf("meeting 流量统计报告 %s - %s UTC", start.UTC().Format("2006-01-02 15:04"), end.UTC().Format("2006-01-02 15:04"))
	if _, err := r.mailer.SendEmail(ctx, auth.EmailMessage{
		To:             r.recipients,
		Subject:        subject,
		TextBody:       textBody,
		HTMLBody:       htmlBody,
		ContentSummary: "meeting 流量统计报告",
		Attachments:    attachments,
	}); err != nil {
		return err
	}

	if r.logger != nil {
		r.logger.Info(
			"traffic statistics report sent",
			"recipientCount", len(r.recipients),
			"meetingCount", len(meetings),
			"participantCount", len(participantsInWindow),
			"attachmentCount", len(attachments),
			"windowStart", start.UTC(),
			"windowEnd", end.UTC(),
		)
	}

	return nil
}

func (r *Reporter) buildTextBody(start time.Time, end time.Time, meetingCount int, participantCount int, summaryRows [][]string) string {
	versionFooter := fmt.Sprintf(
		"\n\n版本信息：\ntag: %s\ncommit: %s\nbuild time: %s\n",
		emptyAsUnknown(r.buildInfo.Tag),
		emptyAsUnknown(r.buildInfo.Commit),
		emptyAsUnknown(r.buildInfo.BuildTime),
	)
	windowText := fmt.Sprintf("统计窗口：%s 至 %s UTC", start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	if meetingCount == 0 && participantCount == 0 {
		return windowText + "\n\n过去 24 小时没有任何使用数据。" + versionFooter
	}

	return fmt.Sprintf("%s\n\n过去 24 小时共有 %d 场会议、%d 条参会记录。\n\n%s\n\n详细明细见附件 CSV。",
		windowText,
		meetingCount,
		participantCount,
		formatSummaryTextTable(summaryRows),
	) + versionFooter
}

func (r *Reporter) buildHTMLBody(start time.Time, end time.Time, meetingCount int, participantCount int, summaryRows [][]string) string {
	windowText := fmt.Sprintf("统计窗口：%s 至 %s UTC", start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	versionHTML := fmt.Sprintf(
		`<p style="margin-top:24px;color:#475569;font-size:13px;line-height:1.6;">版本信息：<br>tag: %s<br>commit: %s<br>build time: %s</p>`,
		html.EscapeString(emptyAsUnknown(r.buildInfo.Tag)),
		html.EscapeString(emptyAsUnknown(r.buildInfo.Commit)),
		html.EscapeString(emptyAsUnknown(r.buildInfo.BuildTime)),
	)
	if meetingCount == 0 && participantCount == 0 {
		return fmt.Sprintf(
			`<!doctype html><html><body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:#0f172a;"><p>%s</p><p>过去 24 小时没有任何使用数据。</p>%s</body></html>`,
			html.EscapeString(windowText),
			versionHTML,
		)
	}

	return fmt.Sprintf(
		`<!doctype html><html><body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:#0f172a;"><p>%s</p><p>过去 24 小时共有 %d 场会议、%d 条参会记录。</p>%s<p>详细明细见附件 CSV。</p>%s</body></html>`,
		html.EscapeString(windowText),
		meetingCount,
		participantCount,
		formatSummaryHTMLTable(summaryRows),
		versionHTML,
	)
}

func buildSummaryRows(start time.Time, end time.Time, meetings []sqlite.MeetingUsageRecord, participants []sqlite.MeetingParticipantUsageRecord) [][]string {
	summary := calculateMeetingSummary(start, end, meetings)
	return [][]string{
		{"指标", "数值", "会议ID"},
		{"用户访问数量", fmt.Sprintf("%d", countDistinctVisitors(participants)), ""},
		{"会议数量", fmt.Sprintf("%d", len(meetings)), ""},
		{"会议总时长", formatDuration(summary.TotalDuration), ""},
		{"时间最长的会议", formatDuration(summary.LongestDuration), summary.LongestMeetingID},
		{"时间最短的会议", formatDuration(summary.ShortestDuration), summary.ShortestMeetingID},
	}
}

func formatSummaryTextTable(rows [][]string) string {
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, strings.Join(row, " | "))
	}
	return strings.Join(lines, "\n")
}

func formatSummaryHTMLTable(rows [][]string) string {
	var builder strings.Builder
	builder.WriteString(`<table style="border-collapse:collapse;margin:16px 0;width:100%;max-width:720px;">`)
	for rowIndex, row := range rows {
		if rowIndex == 0 {
			builder.WriteString("<thead><tr>")
			for _, cell := range row {
				builder.WriteString(`<th style="border:1px solid #cbd5e1;background:#f1f5f9;padding:8px 10px;text-align:left;">`)
				builder.WriteString(html.EscapeString(cell))
				builder.WriteString("</th>")
			}
			builder.WriteString("</tr></thead><tbody>")
			continue
		}
		builder.WriteString("<tr>")
		for _, cell := range row {
			builder.WriteString(`<td style="border:1px solid #cbd5e1;padding:8px 10px;">`)
			builder.WriteString(html.EscapeString(cell))
			builder.WriteString("</td>")
		}
		builder.WriteString("</tr>")
	}
	builder.WriteString("</tbody></table>")
	return builder.String()
}

func buildUserRows(meetings []sqlite.MeetingUsageRecord, participants []sqlite.MeetingParticipantUsageRecord) [][]string {
	rows := [][]string{{"注册用户邮箱", "匿名用户昵称", "IP地址", "创建的会议数量", "创建的会议ID"}}
	users := aggregateUsers(meetings, participants)
	keys := make([]string, 0, len(users))
	for key := range users {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		user := users[key]
		sort.Strings(user.CreatedMeetingIDs)
		rows = append(rows, []string{
			user.Email,
			user.AnonymousNickname,
			user.IPAddress,
			fmt.Sprintf("%d", len(user.CreatedMeetingIDs)),
			strings.Join(user.CreatedMeetingIDs, " "),
		})
	}
	return rows
}

func buildMeetingRows(meetings []sqlite.MeetingUsageRecord, participants []sqlite.MeetingParticipantUsageRecord) [][]string {
	rows := [][]string{{"会议ID", "主持人", "会议类型", "参会人数", "参会人员"}}
	participantsByMeeting := make(map[string][]sqlite.MeetingParticipantUsageRecord)
	for _, participant := range participants {
		participantsByMeeting[participant.MeetingID] = append(participantsByMeeting[participant.MeetingID], participant)
	}

	sort.SliceStable(meetings, func(i int, j int) bool {
		return meetings[i].CreatedAt.Before(meetings[j].CreatedAt)
	})
	for _, meeting := range meetings {
		meetingParticipants := participantsByMeeting[meeting.ID]
		rows = append(rows, []string{
			meeting.ID,
			hostLabel(meeting),
			meeting.MeetingType,
			fmt.Sprintf("%d", len(meetingParticipants)),
			formatParticipants(meetingParticipants),
		})
	}

	return rows
}

type meetingSummary struct {
	TotalDuration     time.Duration
	LongestMeetingID  string
	LongestDuration   time.Duration
	ShortestMeetingID string
	ShortestDuration  time.Duration
}

func calculateMeetingSummary(start time.Time, end time.Time, meetings []sqlite.MeetingUsageRecord) meetingSummary {
	var summary meetingSummary
	for index, meeting := range meetings {
		duration := overlapDuration(start, end, meeting)
		summary.TotalDuration += duration
		if index == 0 || duration > summary.LongestDuration {
			summary.LongestMeetingID = meeting.ID
			summary.LongestDuration = duration
		}
		if index == 0 || duration < summary.ShortestDuration {
			summary.ShortestMeetingID = meeting.ID
			summary.ShortestDuration = duration
		}
	}
	return summary
}

func overlapDuration(start time.Time, end time.Time, meeting sqlite.MeetingUsageRecord) time.Duration {
	overlapStart := meeting.CreatedAt
	if overlapStart.Before(start) {
		overlapStart = start
	}
	overlapEnd := end
	if meeting.EndedAt != nil && meeting.EndedAt.Before(overlapEnd) {
		overlapEnd = *meeting.EndedAt
	}
	if !overlapEnd.After(overlapStart) {
		return 0
	}
	return overlapEnd.Sub(overlapStart)
}

func countDistinctVisitors(participants []sqlite.MeetingParticipantUsageRecord) int {
	visitors := make(map[string]struct{}, len(participants))
	for _, participant := range participants {
		visitors[visitorKey(participant)] = struct{}{}
	}
	return len(visitors)
}

type userAggregate struct {
	Email             string
	AnonymousNickname string
	IPAddress         string
	CreatedMeetingIDs []string
}

func aggregateUsers(meetings []sqlite.MeetingUsageRecord, participants []sqlite.MeetingParticipantUsageRecord) map[string]*userAggregate {
	users := make(map[string]*userAggregate)
	for _, participant := range participants {
		key := visitorKey(participant)
		if _, exists := users[key]; !exists {
			users[key] = &userAggregate{
				Email:             participant.Email,
				AnonymousNickname: anonymousNickname(participant),
				IPAddress:         participant.IPAddress,
			}
		}
	}

	for _, meeting := range meetings {
		key := hostVisitorKey(meeting)
		user := users[key]
		if user == nil {
			user = &userAggregate{
				Email:     meeting.HostEmail,
				IPAddress: meeting.HostIPAddress,
			}
			users[key] = user
		}
		user.CreatedMeetingIDs = append(user.CreatedMeetingIDs, meeting.ID)
	}

	return users
}

func visitorKey(participant sqlite.MeetingParticipantUsageRecord) string {
	if participant.UserID != "" {
		return "user:" + participant.UserID
	}
	return "anonymous:" + participant.IPAddress + ":" + participant.Nickname
}

func hostVisitorKey(meeting sqlite.MeetingUsageRecord) string {
	if meeting.HostUserID != "" {
		return "user:" + meeting.HostUserID
	}
	return "anonymous:" + meeting.HostIPAddress + ":" + meeting.HostNickname
}

func meetingIDs(meetings []sqlite.MeetingUsageRecord) []string {
	ids := make([]string, 0, len(meetings))
	for _, meeting := range meetings {
		ids = append(ids, meeting.ID)
	}
	return ids
}

type csvAttachmentInput struct {
	filename string
	rows     [][]string
}

func csvAttachments(inputs ...csvAttachmentInput) ([]auth.MailAttachment, error) {
	attachments := make([]auth.MailAttachment, 0, len(inputs))
	for _, input := range inputs {
		data, err := csvBytes(input.rows)
		if err != nil {
			return nil, fmt.Errorf("build %s: %w", input.filename, err)
		}
		attachments = append(attachments, auth.MailAttachment{
			Filename:    input.filename,
			ContentType: "text/csv; charset=utf-8",
			Data:        data,
		})
	}
	return attachments, nil
}

func csvBytes(rows [][]string) ([]byte, error) {
	var buffer bytes.Buffer
	if _, err := buffer.Write([]byte{0xef, 0xbb, 0xbf}); err != nil {
		return nil, fmt.Errorf("write csv bom: %w", err)
	}
	writer := csv.NewWriter(&buffer)
	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			return nil, fmt.Errorf("write csv row: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("flush csv: %w", err)
	}
	return buffer.Bytes(), nil
}

func formatParticipants(participants []sqlite.MeetingParticipantUsageRecord) string {
	labels := make([]string, 0, len(participants))
	for _, participant := range participants {
		labels = append(labels, participantLabel(participant))
	}
	return strings.Join(labels, "；")
}

func participantLabel(participant sqlite.MeetingParticipantUsageRecord) string {
	if participant.Email != "" {
		return fmt.Sprintf("%s(%s)", participant.Email, participant.ParticipantRole)
	}
	fields := []string{
		"nickname=" + participant.Nickname,
		"ip=" + participant.IPAddress,
		"device=" + participant.DeviceType,
		"role=" + participant.ParticipantRole,
		"participantId=" + participant.ParticipantID,
	}
	return strings.Join(fields, ",")
}

func hostLabel(meeting sqlite.MeetingUsageRecord) string {
	if meeting.HostEmail != "" {
		return meeting.HostEmail
	}
	if meeting.HostNickname != "" && meeting.HostIPAddress != "" {
		return meeting.HostNickname + " (" + meeting.HostIPAddress + ")"
	}
	if meeting.HostNickname != "" {
		return meeting.HostNickname
	}
	return meeting.HostUserID
}

func anonymousNickname(participant sqlite.MeetingParticipantUsageRecord) string {
	if participant.IsAnonymous {
		return participant.Nickname
	}
	return ""
}

func formatDuration(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	totalSeconds := int64(duration.Round(time.Second).Seconds())
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

func parseSendAtUTC(value string) (time.Duration, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		trimmed = defaultSendAtUTC
	}
	parsed, err := time.Parse("15:04", trimmed)
	if err != nil {
		return 0, fmt.Errorf("invalid MEETING_STATS_REPORT_SEND_AT_UTC %q: %w", value, err)
	}
	return time.Duration(parsed.Hour())*time.Hour + time.Duration(parsed.Minute())*time.Minute, nil
}

func nextDailyRun(now time.Time, sendAt time.Duration) time.Time {
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	next := dayStart.Add(sendAt)
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

func normalizeRecipients(recipients []string) []string {
	normalized := make([]string, 0, len(recipients))
	seen := make(map[string]struct{}, len(recipients))
	for _, recipient := range recipients {
		trimmed := strings.TrimSpace(recipient)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func emptyAsUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}
