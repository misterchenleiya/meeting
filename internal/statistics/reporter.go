package statistics

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/misterchenleiya/meeting/internal/auth"
	"github.com/misterchenleiya/meeting/internal/buildinfo"
	"github.com/misterchenleiya/meeting/internal/storage/sqlite"
)

const defaultSendAtUTC = "12:00"
const unknownMeetingNumber = "未知会议号"

type Store interface {
	ListMeetingUsageWindow(ctx context.Context, start time.Time, end time.Time) ([]sqlite.MeetingUsageRecord, error)
	ListParticipantUsageWindow(ctx context.Context, start time.Time, end time.Time) ([]sqlite.MeetingParticipantUsageRecord, error)
	ListParticipantsForMeetings(ctx context.Context, meetingIDs []string) ([]sqlite.MeetingParticipantUsageRecord, error)
	ListUsersCreatedWindow(ctx context.Context, start time.Time, end time.Time) ([]sqlite.UserRegistrationRecord, error)
	ListEmailCodeLoginsWindow(ctx context.Context, start time.Time, end time.Time) ([]sqlite.EmailCodeLoginRecord, error)
	ListAuditEventsWindow(ctx context.Context, start time.Time, end time.Time) ([]sqlite.AuditEvent, error)
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
	newUsers, err := r.store.ListUsersCreatedWindow(ctx, start, end)
	if err != nil {
		return err
	}
	emailCodeLogins, err := r.store.ListEmailCodeLoginsWindow(ctx, start, end)
	if err != nil {
		return err
	}
	auditEvents, err := r.store.ListAuditEventsWindow(ctx, start, end)
	if err != nil {
		return err
	}

	attachments := []auth.MailAttachment(nil)
	quality := calculateQualitySummary(auditEvents)
	profiles := calculateClientProfileSummary(participantsInWindow)
	summaryRows := buildSummaryRows(start, end, meetings, participantsInWindow, newUsers, emailCodeLogins, quality, profiles)
	activity := reportActivity{
		MeetingCount:        len(meetings),
		ParticipantCount:    len(participantsInWindow),
		NewUserCount:        len(newUsers),
		EmailCodeLoginCount: len(emailCodeLogins),
		QualitySampleCount:  quality.SampleCount,
	}
	textBody := r.buildTextBody(start, end, activity, summaryRows)
	htmlBody := r.buildHTMLBody(start, end, activity, summaryRows)
	if activity.HasData() {
		meetingParticipants, err := r.store.ListParticipantsForMeetings(ctx, meetingIDs(meetings))
		if err != nil {
			return err
		}
		attachments, err = csvAttachments(
			csvAttachmentInput{filename: "users.csv", rows: buildUserRows(meetings, participantsInWindow)},
			csvAttachmentInput{filename: "meetings.csv", rows: buildMeetingRows(meetings, meetingParticipants)},
			csvAttachmentInput{filename: "new_users.csv", rows: buildNewUserRows(newUsers)},
			csvAttachmentInput{filename: "email_code_logins.csv", rows: buildEmailCodeLoginRows(emailCodeLogins)},
			csvAttachmentInput{filename: "meeting_quality.csv", rows: buildMeetingQualityRows(auditEvents, meetings)},
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
			"newUserCount", len(newUsers),
			"emailCodeLoginCount", len(emailCodeLogins),
			"qualitySampleCount", quality.SampleCount,
			"attachmentCount", len(attachments),
			"windowStart", start.UTC(),
			"windowEnd", end.UTC(),
		)
	}

	return nil
}

type reportActivity struct {
	MeetingCount        int
	ParticipantCount    int
	NewUserCount        int
	EmailCodeLoginCount int
	QualitySampleCount  int
}

func (a reportActivity) HasData() bool {
	return a.MeetingCount > 0 || a.ParticipantCount > 0 || a.NewUserCount > 0 || a.EmailCodeLoginCount > 0 || a.QualitySampleCount > 0
}

func (r *Reporter) buildTextBody(start time.Time, end time.Time, activity reportActivity, summaryRows [][]string) string {
	versionFooter := fmt.Sprintf(
		"\n\n版本信息：\ntag: %s\ncommit: %s\nbuild time: %s\n",
		emptyAsUnknown(r.buildInfo.Tag),
		emptyAsUnknown(r.buildInfo.Commit),
		emptyAsUnknown(r.buildInfo.BuildTime),
	)
	windowText := fmt.Sprintf("统计窗口：%s 至 %s UTC", start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	if !activity.HasData() {
		return windowText + "\n\n过去 24 小时没有任何使用数据。" + versionFooter
	}

	return fmt.Sprintf("%s\n\n过去 24 小时共有 %d 场会议、%d 条参会记录、%d 个新增用户、%d 次邮件验证码登录、%d 个会议质量样本。\n\n%s\n\n详细明细见附件 CSV。",
		windowText,
		activity.MeetingCount,
		activity.ParticipantCount,
		activity.NewUserCount,
		activity.EmailCodeLoginCount,
		activity.QualitySampleCount,
		formatSummaryTextTable(summaryRows),
	) + versionFooter
}

func (r *Reporter) buildHTMLBody(start time.Time, end time.Time, activity reportActivity, summaryRows [][]string) string {
	windowText := fmt.Sprintf("统计窗口：%s 至 %s UTC", start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	versionHTML := fmt.Sprintf(
		`<p style="margin-top:24px;color:#475569;font-size:13px;line-height:1.6;">版本信息：<br>tag: %s<br>commit: %s<br>build time: %s</p>`,
		html.EscapeString(emptyAsUnknown(r.buildInfo.Tag)),
		html.EscapeString(emptyAsUnknown(r.buildInfo.Commit)),
		html.EscapeString(emptyAsUnknown(r.buildInfo.BuildTime)),
	)
	if !activity.HasData() {
		return fmt.Sprintf(
			`<!doctype html><html><body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:#0f172a;"><p>%s</p><p>过去 24 小时没有任何使用数据。</p>%s</body></html>`,
			html.EscapeString(windowText),
			versionHTML,
		)
	}

	return fmt.Sprintf(
		`<!doctype html><html><body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:#0f172a;"><p>%s</p><p>过去 24 小时共有 %d 场会议、%d 条参会记录、%d 个新增用户、%d 次邮件验证码登录、%d 个会议质量样本。</p>%s<p>详细明细见附件 CSV。</p>%s</body></html>`,
		html.EscapeString(windowText),
		activity.MeetingCount,
		activity.ParticipantCount,
		activity.NewUserCount,
		activity.EmailCodeLoginCount,
		activity.QualitySampleCount,
		formatSummaryHTMLTable(summaryRows),
		versionHTML,
	)
}

func buildSummaryRows(start time.Time, end time.Time, meetings []sqlite.MeetingUsageRecord, participants []sqlite.MeetingParticipantUsageRecord, newUsers []sqlite.UserRegistrationRecord, emailCodeLogins []sqlite.EmailCodeLoginRecord, quality qualitySummary, profiles clientProfileSummary) [][]string {
	summary := calculateMeetingSummary(start, end, meetings)
	rows := [][]string{
		{"指标", "数值", "会议号"},
		{"用户访问数量", fmt.Sprintf("%d", countDistinctVisitors(participants)), ""},
		{"新增用户数", fmt.Sprintf("%d", len(newUsers)), ""},
		{"邮件验证码登录次数", fmt.Sprintf("%d", len(emailCodeLogins)), ""},
		{"独立登录 IP 数量", fmt.Sprintf("%d", countDistinctLoginIPs(emailCodeLogins)), ""},
		{"会议数量", fmt.Sprintf("%d", len(meetings)), ""},
		{"会议总时长", formatDuration(summary.TotalDuration), ""},
		{"时间最长的会议", formatDuration(summary.LongestDuration), summary.LongestMeetingNumber},
		{"时间最短的会议", formatDuration(summary.ShortestDuration), summary.ShortestMeetingNumber},
		{"会议质量样本数", fmt.Sprintf("%d", quality.SampleCount), ""},
		{"平均延迟", formatMilliseconds(quality.AverageLatencyMS), ""},
		{"最高延迟", formatMilliseconds(float64(quality.MaxLatencyMS)), ""},
		{"平均丢包率", formatPercent(quality.AveragePacketLossRate), ""},
		{"最高丢包率", formatPercent(quality.MaxPacketLossRate), ""},
		{"平均帧率", formatFloat(quality.AverageFPS, 1), ""},
		{"平均码率", formatKbps(quality.AverageBitrateKbps), ""},
		{"弱网样本数", fmt.Sprintf("%d", quality.WeakSampleCount), ""},
	}
	rows = append(rows, clientProfileSummaryRows(profiles)...)
	return rows
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
	rows := [][]string{{"注册用户邮箱", "匿名用户昵称", "IP地址", "创建的会议数量", "创建的会议号"}}
	users := aggregateUsers(meetings, participants)
	keys := make([]string, 0, len(users))
	for key := range users {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		user := users[key]
		sort.Strings(user.CreatedMeetingNumbers)
		rows = append(rows, []string{
			user.Email,
			user.AnonymousNickname,
			user.IPAddress,
			fmt.Sprintf("%d", len(user.CreatedMeetingNumbers)),
			strings.Join(user.CreatedMeetingNumbers, " "),
		})
	}
	return rows
}

func buildMeetingRows(meetings []sqlite.MeetingUsageRecord, participants []sqlite.MeetingParticipantUsageRecord) [][]string {
	rows := [][]string{{"会议号", "主持人", "会议类型", "参会人数", "参会人员"}}
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
			reportMeetingNumber(meeting),
			hostLabel(meeting),
			meeting.MeetingType,
			fmt.Sprintf("%d", len(meetingParticipants)),
			formatParticipants(meetingParticipants),
		})
	}

	return rows
}

func buildNewUserRows(users []sqlite.UserRegistrationRecord) [][]string {
	rows := [][]string{{"邮件地址", "IP地址", "注册时间", "当前昵称"}}
	sort.SliceStable(users, func(i int, j int) bool {
		return users[i].CreatedAt.Before(users[j].CreatedAt)
	})
	for _, user := range users {
		rows = append(rows, []string{
			user.Email,
			user.IPAddress,
			formatReportTime(user.CreatedAt),
			user.Nickname,
		})
	}
	return rows
}

func buildEmailCodeLoginRows(logins []sqlite.EmailCodeLoginRecord) [][]string {
	rows := [][]string{{"邮件地址", "IP地址", "登录时间"}}
	sort.SliceStable(logins, func(i int, j int) bool {
		return logins[i].LoginAt.Before(logins[j].LoginAt)
	})
	for _, login := range logins {
		rows = append(rows, []string{
			login.Email,
			login.IPAddress,
			formatReportTime(login.LoginAt),
		})
	}
	return rows
}

func buildMeetingQualityRows(events []sqlite.AuditEvent, meetings []sqlite.MeetingUsageRecord) [][]string {
	rows := [][]string{{"会议号", "参会者ID", "角色", "设备类型", "样本数", "平均延迟ms", "最高延迟ms", "平均丢包率", "最高丢包率", "平均FPS", "平均码率Kbps", "弱网样本数"}}
	aggregates := aggregateQualityEvents(events)
	meetingNumbers := meetingNumberByID(meetings)
	keys := make([]string, 0, len(aggregates))
	for key := range aggregates {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		aggregate := aggregates[key]
		rows = append(rows, []string{
			displayMeetingNumber(meetingNumbers[aggregate.MeetingID]),
			aggregate.ParticipantID,
			aggregate.ParticipantRole,
			aggregate.DeviceType,
			fmt.Sprintf("%d", aggregate.SampleCount),
			fmt.Sprintf("%d", roundFloat(aggregate.averageLatencyMS())),
			fmt.Sprintf("%d", aggregate.MaxLatencyMS),
			formatFloat(aggregate.averagePacketLossRate(), 4),
			formatFloat(aggregate.MaxPacketLossRate, 4),
			formatFloat(aggregate.averageFPS(), 1),
			fmt.Sprintf("%d", roundFloat(aggregate.averageBitrateKbps())),
			fmt.Sprintf("%d", aggregate.WeakSampleCount),
		})
	}
	return rows
}

type meetingSummary struct {
	TotalDuration         time.Duration
	LongestMeetingNumber  string
	LongestDuration       time.Duration
	ShortestMeetingNumber string
	ShortestDuration      time.Duration
}

type qualitySummary struct {
	SampleCount           int
	AverageLatencyMS      float64
	MaxLatencyMS          int64
	AveragePacketLossRate float64
	MaxPacketLossRate     float64
	AverageFPS            float64
	AverageBitrateKbps    float64
	WeakSampleCount       int
}

type qualityAggregate struct {
	MeetingID         string
	ParticipantID     string
	ParticipantRole   string
	DeviceType        string
	SampleCount       int
	LatencyTotalMS    int64
	MaxLatencyMS      int64
	PacketLossTotal   float64
	MaxPacketLossRate float64
	FPSTotal          float64
	BitrateTotalKbps  int64
	WeakSampleCount   int
}

type clientProfileSummary struct {
	DeviceCategories map[string]int
	Browsers         map[string]int
	OperatingSystems map[string]int
	Networks         map[string]int
}

func calculateQualitySummary(events []sqlite.AuditEvent) qualitySummary {
	var summary qualitySummary
	for _, event := range events {
		if event.EventType != "media_report" {
			continue
		}
		summary.SampleCount++
		summary.AverageLatencyMS += float64(event.LatencyMS)
		if event.LatencyMS > summary.MaxLatencyMS {
			summary.MaxLatencyMS = event.LatencyMS
		}
		summary.AveragePacketLossRate += event.PacketLossRate
		if event.PacketLossRate > summary.MaxPacketLossRate {
			summary.MaxPacketLossRate = event.PacketLossRate
		}
		summary.AverageFPS += event.AverageFPS
		summary.AverageBitrateKbps += float64(event.AverageBitrateKB)
		if weakQualitySample(event) {
			summary.WeakSampleCount++
		}
	}
	if summary.SampleCount == 0 {
		return summary
	}

	count := float64(summary.SampleCount)
	summary.AverageLatencyMS /= count
	summary.AveragePacketLossRate /= count
	summary.AverageFPS /= count
	summary.AverageBitrateKbps /= count
	return summary
}

func aggregateQualityEvents(events []sqlite.AuditEvent) map[string]*qualityAggregate {
	aggregates := make(map[string]*qualityAggregate)
	for _, event := range events {
		if event.EventType != "media_report" {
			continue
		}
		key := event.MeetingID + "\x00" + event.ParticipantID
		aggregate := aggregates[key]
		if aggregate == nil {
			aggregate = &qualityAggregate{
				MeetingID:       event.MeetingID,
				ParticipantID:   event.ParticipantID,
				ParticipantRole: event.ParticipantRole,
				DeviceType:      event.DeviceType,
			}
			aggregates[key] = aggregate
		}
		aggregate.SampleCount++
		aggregate.LatencyTotalMS += event.LatencyMS
		if event.LatencyMS > aggregate.MaxLatencyMS {
			aggregate.MaxLatencyMS = event.LatencyMS
		}
		aggregate.PacketLossTotal += event.PacketLossRate
		if event.PacketLossRate > aggregate.MaxPacketLossRate {
			aggregate.MaxPacketLossRate = event.PacketLossRate
		}
		aggregate.FPSTotal += event.AverageFPS
		aggregate.BitrateTotalKbps += event.AverageBitrateKB
		if weakQualitySample(event) {
			aggregate.WeakSampleCount++
		}
	}
	return aggregates
}

func (a qualityAggregate) averageLatencyMS() float64 {
	if a.SampleCount == 0 {
		return 0
	}
	return float64(a.LatencyTotalMS) / float64(a.SampleCount)
}

func (a qualityAggregate) averagePacketLossRate() float64 {
	if a.SampleCount == 0 {
		return 0
	}
	return a.PacketLossTotal / float64(a.SampleCount)
}

func (a qualityAggregate) averageFPS() float64 {
	if a.SampleCount == 0 {
		return 0
	}
	return a.FPSTotal / float64(a.SampleCount)
}

func (a qualityAggregate) averageBitrateKbps() float64 {
	if a.SampleCount == 0 {
		return 0
	}
	return float64(a.BitrateTotalKbps) / float64(a.SampleCount)
}

func weakQualitySample(event sqlite.AuditEvent) bool {
	if event.LatencyMS >= 280 || event.PacketLossRate >= 0.06 {
		return true
	}
	if event.AverageBitrateKB > 0 && event.AverageBitrateKB < 900 {
		return true
	}
	return event.AverageFPS > 0 && event.AverageFPS < 12
}

func calculateClientProfileSummary(participants []sqlite.MeetingParticipantUsageRecord) clientProfileSummary {
	summary := clientProfileSummary{
		DeviceCategories: make(map[string]int),
		Browsers:         make(map[string]int),
		OperatingSystems: make(map[string]int),
		Networks:         make(map[string]int),
	}
	for _, participant := range participants {
		profile := decodeClientProfile(participant.ClientProfileJSON)
		addDistributionValue(summary.DeviceCategories, profile["deviceCategory"])
		addDistributionValue(summary.Browsers, profile["browser"])
		addDistributionValue(summary.OperatingSystems, profile["os"])
		addDistributionValue(summary.Networks, profile["networkEffectiveType"])
	}
	return summary
}

func decodeClientProfile(raw string) map[string]string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var profile map[string]string
	if err := json.Unmarshal([]byte(raw), &profile); err != nil {
		return nil
	}
	return profile
}

func addDistributionValue(values map[string]int, value string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return
	}
	values[trimmed]++
}

func clientProfileSummaryRows(summary clientProfileSummary) [][]string {
	return [][]string{
		{"客户端设备分布", formatDistribution(summary.DeviceCategories), ""},
		{"客户端浏览器分布", formatDistribution(summary.Browsers), ""},
		{"客户端系统分布", formatDistribution(summary.OperatingSystems), ""},
		{"客户端网络分布", formatDistribution(summary.Networks), ""},
	}
}

func formatDistribution(values map[string]int) string {
	if len(values) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s: %d", key, values[key]))
	}
	return strings.Join(parts, "；")
}

func calculateMeetingSummary(start time.Time, end time.Time, meetings []sqlite.MeetingUsageRecord) meetingSummary {
	var summary meetingSummary
	for index, meeting := range meetings {
		duration := overlapDuration(start, end, meeting)
		summary.TotalDuration += duration
		if index == 0 || duration > summary.LongestDuration {
			summary.LongestMeetingNumber = reportMeetingNumber(meeting)
			summary.LongestDuration = duration
		}
		if index == 0 || duration < summary.ShortestDuration {
			summary.ShortestMeetingNumber = reportMeetingNumber(meeting)
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

func countDistinctLoginIPs(logins []sqlite.EmailCodeLoginRecord) int {
	ips := make(map[string]struct{}, len(logins))
	for _, login := range logins {
		ip := strings.TrimSpace(login.IPAddress)
		if ip == "" {
			continue
		}
		ips[ip] = struct{}{}
	}
	return len(ips)
}

type userAggregate struct {
	Email                 string
	AnonymousNickname     string
	IPAddress             string
	CreatedMeetingNumbers []string
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
		user.CreatedMeetingNumbers = append(user.CreatedMeetingNumbers, reportMeetingNumber(meeting))
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

func meetingNumberByID(meetings []sqlite.MeetingUsageRecord) map[string]string {
	numbers := make(map[string]string, len(meetings))
	for _, meeting := range meetings {
		numbers[meeting.ID] = reportMeetingNumber(meeting)
	}
	return numbers
}

func reportMeetingNumber(meeting sqlite.MeetingUsageRecord) string {
	return displayMeetingNumber(meeting.MeetingNumber)
}

func displayMeetingNumber(value string) string {
	if strings.TrimSpace(value) == "" {
		return unknownMeetingNumber
	}
	return strings.TrimSpace(value)
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

func formatMilliseconds(value float64) string {
	return fmt.Sprintf("%d ms", roundFloat(value))
}

func formatKbps(value float64) string {
	return fmt.Sprintf("%d kbps", roundFloat(value))
}

func formatPercent(value float64) string {
	return fmt.Sprintf("%.2f%%", value*100)
}

func formatFloat(value float64, precision int) string {
	return fmt.Sprintf("%.*f", precision, value)
}

func roundFloat(value float64) int64 {
	return int64(math.Round(value))
}

func formatReportTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339)
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
