CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    email TEXT,
    password_hash TEXT,
    wechat_openid TEXT,
    nickname TEXT NOT NULL DEFAULT '',
    email_verified_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_unique
ON users(email)
WHERE email IS NOT NULL AND email <> '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_wechat_openid_unique
ON users(wechat_openid)
WHERE wechat_openid IS NOT NULL AND wechat_openid <> '';

CREATE INDEX IF NOT EXISTS idx_users_created_at
ON users(created_at)
WHERE email IS NOT NULL AND email <> '';

CREATE TABLE IF NOT EXISTS auth_verification_codes (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL,
    purpose TEXT NOT NULL,
    nickname TEXT NOT NULL DEFAULT '',
    client_id TEXT NOT NULL DEFAULT '',
    ip_address TEXT NOT NULL DEFAULT '',
    code_hash TEXT NOT NULL,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    sent_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_auth_verification_codes_email_purpose
ON auth_verification_codes(email, purpose, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_auth_verification_codes_purpose_consumed_at
ON auth_verification_codes(purpose, consumed_at)
WHERE consumed_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS auth_sessions (
    token_hash TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    revoked_at TEXT,
    user_agent TEXT NOT NULL DEFAULT '',
    ip_address TEXT NOT NULL DEFAULT '',
    FOREIGN KEY(user_id) REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_auth_sessions_user_id ON auth_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_expires_at ON auth_sessions(expires_at);

CREATE TABLE IF NOT EXISTS user_preferences (
    user_id TEXT PRIMARY KEY,
    default_camera_enabled INTEGER NOT NULL DEFAULT 0,
    default_microphone_enabled INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL,
    FOREIGN KEY(user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS audit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    meeting_id TEXT NOT NULL,
    participant_id TEXT NOT NULL,
    user_id TEXT NOT NULL DEFAULT '',
    participant_role TEXT NOT NULL,
    event_type TEXT NOT NULL,
    ip_address TEXT NOT NULL DEFAULT '',
    device_type TEXT NOT NULL DEFAULT '',
    latency_ms INTEGER NOT NULL DEFAULT 0,
    packet_loss_rate REAL NOT NULL DEFAULT 0,
    avg_fps REAL NOT NULL DEFAULT 0,
    avg_bitrate_kbps INTEGER NOT NULL DEFAULT 0,
    details_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_events_meeting_id ON audit_events(meeting_id);
CREATE INDEX IF NOT EXISTS idx_audit_events_created_at ON audit_events(created_at);

CREATE TABLE IF NOT EXISTS meeting_usage_meetings (
    id TEXT PRIMARY KEY,
    meeting_number TEXT NOT NULL DEFAULT '',
    join_code TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    meeting_type TEXT NOT NULL DEFAULT 'quick',
    host_participant_id TEXT NOT NULL DEFAULT '',
    host_user_id TEXT NOT NULL DEFAULT '',
    host_email TEXT NOT NULL DEFAULT '',
    host_nickname TEXT NOT NULL DEFAULT '',
    host_ip_address TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    ended_at TEXT,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_meeting_usage_meetings_created_at ON meeting_usage_meetings(created_at);
CREATE INDEX IF NOT EXISTS idx_meeting_usage_meetings_ended_at ON meeting_usage_meetings(ended_at);

CREATE TABLE IF NOT EXISTS meeting_usage_participants (
    meeting_id TEXT NOT NULL,
    participant_id TEXT NOT NULL,
    user_id TEXT NOT NULL DEFAULT '',
    email TEXT NOT NULL DEFAULT '',
    nickname TEXT NOT NULL DEFAULT '',
    is_anonymous INTEGER NOT NULL DEFAULT 0,
    ip_address TEXT NOT NULL DEFAULT '',
    device_type TEXT NOT NULL DEFAULT '',
    client_profile_json TEXT NOT NULL DEFAULT '{}',
    participant_role TEXT NOT NULL DEFAULT '',
    joined_at TEXT NOT NULL,
    left_at TEXT,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (meeting_id, participant_id)
);

CREATE INDEX IF NOT EXISTS idx_meeting_usage_participants_meeting_id ON meeting_usage_participants(meeting_id);
CREATE INDEX IF NOT EXISTS idx_meeting_usage_participants_joined_at ON meeting_usage_participants(joined_at);
CREATE INDEX IF NOT EXISTS idx_meeting_usage_participants_user_id ON meeting_usage_participants(user_id);

CREATE TABLE IF NOT EXISTS meeting_transcript_segments (
    id TEXT PRIMARY KEY,
    meeting_id TEXT NOT NULL,
    meeting_number TEXT NOT NULL DEFAULT '',
    participant_id TEXT NOT NULL,
    user_id TEXT NOT NULL DEFAULT '',
    nickname TEXT NOT NULL DEFAULT '',
    language TEXT NOT NULL DEFAULT '',
    sequence INTEGER NOT NULL DEFAULT 0,
    started_at TEXT NOT NULL,
    ended_at TEXT NOT NULL,
    text TEXT NOT NULL,
    is_final INTEGER NOT NULL DEFAULT 1,
    asr_provider TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_meeting_transcript_segments_unique_chunk
ON meeting_transcript_segments(meeting_id, participant_id, sequence);

CREATE INDEX IF NOT EXISTS idx_meeting_transcript_segments_meeting_time
ON meeting_transcript_segments(meeting_id, started_at, sequence);

CREATE TABLE IF NOT EXISTS meeting_minutes_jobs (
    id TEXT PRIMARY KEY,
    meeting_id TEXT NOT NULL,
    meeting_number TEXT NOT NULL DEFAULT '',
    requested_by_user_id TEXT NOT NULL,
    requested_by_participant_id TEXT NOT NULL,
    status TEXT NOT NULL,
    error_message TEXT NOT NULL DEFAULT '',
    email_error TEXT NOT NULL DEFAULT '',
    started_at TEXT,
    completed_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_meeting_minutes_jobs_meeting_id
ON meeting_minutes_jobs(meeting_id);

CREATE INDEX IF NOT EXISTS idx_meeting_minutes_jobs_status
ON meeting_minutes_jobs(status, updated_at);

CREATE TABLE IF NOT EXISTS meeting_minutes (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL UNIQUE,
    meeting_id TEXT NOT NULL,
    meeting_number TEXT NOT NULL DEFAULT '',
    host_user_id TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    markdown_content TEXT NOT NULL,
    outline_json TEXT NOT NULL DEFAULT '{}',
    llm_provider TEXT NOT NULL DEFAULT '',
    llm_model TEXT NOT NULL DEFAULT '',
    generated_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_meeting_minutes_meeting_id
ON meeting_minutes(meeting_id);

CREATE INDEX IF NOT EXISTS idx_meeting_minutes_host_user_id
ON meeting_minutes(host_user_id, generated_at DESC);

CREATE TABLE IF NOT EXISTS meeting_minutes_shares (
    minutes_id TEXT NOT NULL,
    shared_by_user_id TEXT NOT NULL,
    shared_with_user_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (minutes_id, shared_with_user_id),
    FOREIGN KEY(minutes_id) REFERENCES meeting_minutes(id)
);

CREATE INDEX IF NOT EXISTS idx_meeting_minutes_shares_user_id
ON meeting_minutes_shares(shared_with_user_id, created_at DESC);
