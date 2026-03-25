export type ClientLogLevel = "debug" | "info" | "warn" | "error";

export type ClientLogEntry = {
  level: ClientLogLevel;
  time: string;
  message: string;
  scope?: string;
  page?: string;
  sessionId?: string;
  [key: string]: unknown;
};

const clientLogUploadEndpoint = "/api/client-logs";
const recentLogBufferLimit = 400;
const uploadQueueLimit = 200;
const uploadBatchLimit = 20;
const uploadFlushIntervalMs = 2000;
const uploadedInfoMessages = new Set([
  "auth.login_succeeded",
  "meeting.schedule_create_requested",
  "meeting.schedule_create_succeeded",
  "meeting.quick_create_requested",
  "meeting.quick_create_succeeded",
  "meeting.join_requested",
  "meeting.join_succeeded",
  "meeting.lookup_requested",
  "meeting.lookup_requires_password",
  "join.scan_modal_opened",
  "join.scan_decoded",
  "join.scan_pick_image_requested",
  "join.scan_image_selected",
  "join.scan_live_requested",
  "signal.connected",
  "signal.meeting_ended_received",
  "meeting.end_confirm_opened",
  "meeting.end_requested",
  "meeting.end_request_succeeded",
  "meeting.end_summary_preparing"
]);

const recentEntries: ClientLogEntry[] = [];
const uploadQueue: ClientLogEntry[] = [];
const sessionId = createClientSessionID();

let transportInitialized = false;
let uploadTimer: number | null = null;
let uploadInFlight = false;

export function createClientLogger(scope: string) {
  return {
    debug: (message: string, fields?: Record<string, unknown>) =>
      writeClientLog("debug", message, {
        scope,
        ...(fields ?? {})
      }),
    info: (message: string, fields?: Record<string, unknown>) =>
      writeClientLog("info", message, {
        scope,
        ...(fields ?? {})
      }),
    warn: (message: string, fields?: Record<string, unknown>) =>
      writeClientLog("warn", message, {
        scope,
        ...(fields ?? {})
      }),
    error: (message: string, fields?: Record<string, unknown>) =>
      writeClientLog("error", message, {
        scope,
        ...(fields ?? {})
      })
  };
}

export function formatClientLogs(): string {
  if (recentEntries.length === 0) {
    return "";
  }

  return recentEntries.map((entry) => JSON.stringify(entry)).join("\n");
}

function writeClientLog(level: ClientLogLevel, message: string, fields: Record<string, unknown>) {
  const entry: ClientLogEntry = {
    level,
    time: new Date().toISOString(),
    message,
    page: resolveCurrentPage(),
    sessionId,
    ...sanitizeLogFields(fields)
  };

  writeToConsole(level, entry);
  appendToRecentBuffer(entry);
  enqueueForUpload(entry);
  return entry;
}

function writeToConsole(level: ClientLogLevel, entry: ClientLogEntry) {
  const text = JSON.stringify(entry);
  switch (level) {
    case "debug":
      console.debug(text);
      return;
    case "info":
      console.info(text);
      return;
    case "warn":
      console.warn(text);
      return;
    case "error":
      console.error(text);
      return;
  }
}

function appendToRecentBuffer(entry: ClientLogEntry) {
  recentEntries.push(entry);
  if (recentEntries.length > recentLogBufferLimit) {
    recentEntries.splice(0, recentEntries.length - recentLogBufferLimit);
  }
}

function enqueueForUpload(entry: ClientLogEntry) {
  if (!shouldUpload(entry)) {
    return;
  }

  ensureTransportInitialized();
  uploadQueue.push(entry);
  if (uploadQueue.length > uploadQueueLimit) {
    uploadQueue.splice(0, uploadQueue.length - uploadQueueLimit);
  }

  if (entry.level === "warn" || entry.level === "error") {
    void flushUploadQueue("priority");
    return;
  }

  scheduleUploadFlush();
}

function shouldUpload(entry: ClientLogEntry) {
  switch (entry.level) {
    case "error":
    case "warn":
      return true;
    case "info":
      return uploadedInfoMessages.has(entry.message);
    case "debug":
      return false;
  }
}

function ensureTransportInitialized() {
  if (transportInitialized || typeof window === "undefined") {
    return;
  }

  window.addEventListener("pagehide", () => {
    flushWithBeacon("pagehide");
  });

  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "hidden") {
      flushWithBeacon("hidden");
    }
  });

  transportInitialized = true;
}

function scheduleUploadFlush() {
  if (uploadTimer !== null || typeof window === "undefined") {
    return;
  }

  uploadTimer = window.setTimeout(() => {
    uploadTimer = null;
    void flushUploadQueue("timer");
  }, uploadFlushIntervalMs);
}

function clearUploadTimer() {
  if (uploadTimer === null || typeof window === "undefined") {
    return;
  }

  window.clearTimeout(uploadTimer);
  uploadTimer = null;
}

function flushWithBeacon(reason: string) {
  if (typeof navigator === "undefined" || uploadQueue.length === 0) {
    return;
  }

  if (uploadInFlight) {
    emitTransportDiagnostic("warn", "client_log_upload_skipped", {
      reason,
      detail: "upload already in flight"
    });
    return;
  }

  clearUploadTimer();
  const batch = uploadQueue.splice(0, Math.min(uploadQueue.length, uploadBatchLimit));
  if (batch.length === 0) {
    return;
  }

  const payload = JSON.stringify({ logs: batch });
  if (typeof navigator.sendBeacon === "function") {
    const ok = navigator.sendBeacon(
      clientLogUploadEndpoint,
      new Blob([payload], { type: "application/json" })
    );
    if (ok) {
      if (uploadQueue.length > 0) {
        scheduleUploadFlush();
      }
      return;
    }
  }

  requeueBatch(batch);
  void flushUploadQueue(reason, true);
}

async function flushUploadQueue(reason: string, keepalive = false) {
  if (uploadInFlight || uploadQueue.length === 0 || typeof window === "undefined") {
    return;
  }

  clearUploadTimer();
  uploadInFlight = true;
  const batch = uploadQueue.splice(0, Math.min(uploadQueue.length, uploadBatchLimit));

  try {
    const response = await fetch(clientLogUploadEndpoint, {
      method: "POST",
      headers: {
        "Content-Type": "application/json"
      },
      body: JSON.stringify({ logs: batch }),
      keepalive
    });

    if (!response.ok) {
      throw new Error(`client log upload failed with status ${response.status}`);
    }
  } catch (error) {
    requeueBatch(batch);
    emitTransportDiagnostic("warn", "client_log_upload_failed", {
      reason,
      error
    });
  } finally {
    uploadInFlight = false;
    if (uploadQueue.length > 0) {
      scheduleUploadFlush();
    }
  }
}

function requeueBatch(batch: ClientLogEntry[]) {
  uploadQueue.splice(0, 0, ...batch);
  if (uploadQueue.length > uploadQueueLimit) {
    uploadQueue.splice(uploadQueueLimit);
  }
}

function emitTransportDiagnostic(
  level: Extract<ClientLogLevel, "warn" | "error">,
  message: string,
  fields: Record<string, unknown>
) {
  const entry: ClientLogEntry = {
    level,
    time: new Date().toISOString(),
    message,
    scope: "frontend.logger.transport",
    page: resolveCurrentPage(),
    sessionId,
    ...sanitizeLogFields(fields)
  };
  writeToConsole(level, entry);
}

function sanitizeLogFields(fields: Record<string, unknown>) {
  try {
    return JSON.parse(JSON.stringify(fields, logFieldReplacer)) as Record<string, unknown>;
  } catch {
    return {
      serialization_error: "failed to serialize log fields"
    };
  }
}

function logFieldReplacer(_key: string, value: unknown) {
  if (value instanceof Error) {
    return {
      name: value.name,
      message: value.message,
      stack: value.stack
    };
  }

  if (typeof value === "bigint") {
    return value.toString();
  }

  return value;
}

function resolveCurrentPage() {
  if (typeof window === "undefined") {
    return "";
  }

  return `${window.location.pathname}${window.location.search}${window.location.hash}`;
}

function createClientSessionID() {
  if (typeof globalThis.crypto?.randomUUID === "function") {
    return globalThis.crypto.randomUUID();
  }

  return `client-${Date.now()}-${Math.random().toString(16).slice(2, 10)}`;
}
