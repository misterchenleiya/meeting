# Traffic statistics email report

Status: implemented

Date: 2026-05-06

## Background

The meeting service currently records operational audit events, but meeting runtime state is kept in memory and removed when the meeting ends. This makes it hard to send a daily usage report or preserve participant-level usage details for later audit.

The system needs a daily traffic statistics email with an in-body summary table and detailed CSV attachments. The report must still be sent when no usage occurred in the past 24 hours.

## Goals

- Send one statistics email every day to configured recipients.
- Enable the report only when `MEETING_STATS_REPORT_TO` is configured.
- Send at `MEETING_STATS_REPORT_SEND_AT_UTC`, defaulting to `12:00` UTC.
- Report the previous 24 hours on every run.
- Persist meeting and participant statistics in SQLite by default and do not automatically delete them.
- Render summary metrics directly in the email body and attach CSV files for user details, meeting details, new users, email code logins, and meeting quality when the window contains usage data.
- Include backend version information in the email footer: tag, commit, and build time.

## Non-goals

- Do not introduce a separate scheduled meeting persistence model in this change.
- Do not add automatic retention cleanup for statistics data.
- Do not introduce XLSX generation or third-party spreadsheet dependencies.
- Do not add an admin UI for report history.

## Core decisions

- Detail attachments use CSV. Files use UTF-8 with BOM so common spreadsheet tools can open Chinese text consistently.
- Statistics are stored in dedicated tables instead of reconstructing from transient in-memory meetings.
- A meeting is counted in a report window when it was created, active, or ended within the window.
- Meeting duration is calculated as the overlap between the meeting lifetime and the report window; active meetings use the report window end as the temporary end time.
- User visit count is counted by distinct registered `user_id` and distinct anonymous `IP + nickname` visitor keys within the window.
- New registered users are counted from `users.created_at` within the report window.
- Email verification code logins are counted from consumed `auth_verification_codes` rows where `purpose = login`; `consumed_at` is treated as the login time for reporting.
- Quick and scheduled meetings share the same create-meeting API, with a new `meetingType` field identifying the source flow.

## Configuration

- `MEETING_STATS_REPORT_TO`: comma-separated recipient addresses. Empty means the report scheduler is disabled.
- `MEETING_STATS_REPORT_SEND_AT_UTC`: daily send time in `HH:MM` UTC. Empty defaults to `12:00`.

No report interval setting is required. Each run always reports the previous 24 hours.

## Data model

New persistent audit tables:

- `meeting_usage_meetings`: one row per created meeting, including ID, public meeting number, title, meeting type, host identity, host IP, created time, ended time, and update time.
- `meeting_usage_participants`: one row per participant, including internal meeting ID, participant ID, user ID, email snapshot, nickname, anonymous flag, IP, device type, coarse client profile JSON, role, join time, leave time, and update time.

These tables are append/update-only for normal service operation. They are not cleared automatically.

The report also reads existing authentication tables:

- `users`: email, current nickname, and registration time for newly created email users.
- `auth_verification_codes`: consumed login-purpose codes for email verification code login details.

## Email and attachments

When the report window contains usage data, the email includes a summary table in the email body:

- user visit count, new user count, email code login count, distinct login IP count, meeting count, total meeting duration, longest meeting and ID, shortest meeting and ID, meeting quality summary, and client profile distributions.

The email also includes five detail attachments:

- `users.csv`: registered user email, anonymous nickname, IP address, created meeting count, created meeting numbers.
- `meetings.csv`: public meeting number, host, meeting type, participant count, participant list.
- `new_users.csv`: email address, IP address, registration time, current nickname.
- `email_code_logins.csv`: email address, IP address, login time.
- `meeting_quality.csv`: public meeting number, participant ID, role, device type, sample count, average/max latency, average/max packet loss, average FPS, average bitrate, weak-network sample count.

When the report window has no meeting, participant, new user, email code login, or meeting quality data, the email has no attachments and only states that there was no usage data in the past 24 hours.

## Compatibility and migration

- Existing databases are migrated with `CREATE TABLE IF NOT EXISTS`; no existing table is removed or rewritten.
- Existing clients that do not send `meetingType` continue to work and are treated as `quick`.
- Existing clients that do not send `clientProfile` continue to work; profile distributions simply omit unknown values.
- Existing mailer configuration continues to be used for sending both verification emails and statistics reports.

## Risks

- SendCloud attachment delivery requires multipart form submission. SMTP remains available as a fallback.
- If the server is offline at the scheduled time, the next process start schedules the next daily run; this change does not backfill missed emails.
- Participant email is snapshotted at join time when a registered user record is available. If lookup fails, the user ID is still retained.

## Verification

- Unit test report window aggregation, summary table rendering, and CSV generation.
- Unit test statistics persistence writes on create, join, leave, and end.
- Unit test SendCloud multipart attachment request.
- Run `go test ./...`.
- Run `npm run build` for the web client.

## Rollback

- Remove `MEETING_STATS_REPORT_TO` to disable sending immediately.
- If needed, revert application code while keeping the new SQLite tables; unused tables are harmless for older binaries.
