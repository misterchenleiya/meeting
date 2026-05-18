import { createClientLogger } from "./logger";
import { normalizeIceServers, resolveApiUrl, type RuntimeIceServer } from "./runtime-config";
import type { ClientProfile } from "./clientProfile";
import type {
  AuthUser,
  ChatMessage,
  MeetingHistoryRecord,
  MinutesJob,
  MinutesParticipant,
  Meeting,
  Participant,
  PersistentMeetingMinutes,
  ReadyCheckRound,
  TranscriptSegment,
  WhiteboardAction
} from "./types";

export type AuthCodeDelivery = {
  email: string;
  purpose: "register" | "login";
  debugCode?: string;
  expiresAt: string;
  resendAfter: string;
  deliveryMode: string;
};

export type AuthLoginResponse = {
  status: string;
  user: AuthUser;
  autoRegistered?: boolean;
};

type RuntimeIceResponseFields = {
  iceServers: RTCIceServer[];
  iceCredentialExpiresAt?: string;
};

type CreateMeetingResponse = {
  meeting: Meeting;
  host: Participant;
} & RuntimeIceResponseFields;

type JoinMeetingResponse = {
  meeting: Meeting;
  participant: Participant;
} & RuntimeIceResponseFields;

type GetMeetingResponse = {
  meeting: Meeting;
};

type RawRuntimeIceResponseFields = {
  iceServers: RuntimeIceServer[];
  iceCredentialExpiresAt?: string;
};

type RawCreateMeetingResponse = {
  meeting: Meeting;
  host: Participant;
} & RawRuntimeIceResponseFields;

type RawJoinMeetingResponse = {
  meeting: Meeting;
  participant: Participant;
} & RawRuntimeIceResponseFields;

type RawMeetingIceServersResponse = {
  iceServers: RuntimeIceServer[];
  expiresAt?: string;
};

export type MeetingIceServersResponse = {
  iceServers: RTCIceServer[];
  expiresAt?: string;
};

export type MeetingMinutesSnapshot = {
  meetingNumber: string;
  title: string;
  chatMessages: ChatMessage[];
  whiteboardActions: WhiteboardAction[];
  temporaryMinutes: string[];
  activeReadyCheck?: ReadyCheckRound;
};

export type TranscriptResponse = {
  meetingNumber: string;
  segments: TranscriptSegment[];
};

export type MinutesJobResponse = {
  job: MinutesJob;
  created?: boolean;
  minutes?: PersistentMeetingMinutes;
};

export type MeetingHistoryResponse = {
  records: MeetingHistoryRecord[];
};

export type PersistentMinutesResponse = {
  minutes: PersistentMeetingMinutes;
  participants: MinutesParticipant[];
};

const logger = createClientLogger("frontend.api");

export class ApiError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

function meetingPathSegment(meetingNumber: string): string {
  return encodeURIComponent(meetingNumber);
}

function normalizeRuntimeIcePayload<T extends { iceServers: RuntimeIceServer[] }>(
  payload: T
): Omit<T, "iceServers"> & { iceServers: RTCIceServer[] } {
  return {
    ...payload,
    iceServers: normalizeIceServers(payload.iceServers)
  };
}

async function requestJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const method = init?.method ?? "GET";
  const url = resolveApiUrl(path);
  logger.debug("request.started", {
    method,
    url
  });

  const response = await fetch(url, {
    ...init,
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers ?? {})
    }
  });

  const data = (await response.json()) as T & { error?: string };
  if (!response.ok) {
    logger.warn("request.failed", {
      method,
      url,
      status: response.status,
      error: data.error ?? `Request failed with status ${response.status}`
    });
    throw new ApiError(data.error ?? `Request failed with status ${response.status}`, response.status);
  }

  logger.debug("request.succeeded", {
    method,
    url,
    status: response.status
  });

  return data;
}

async function requestFormData<T>(path: string, body: FormData): Promise<T> {
  const method = "POST";
  const url = resolveApiUrl(path);
  logger.debug("request.started", {
    method,
    url
  });

  const response = await fetch(url, {
    method,
    credentials: "include",
    body
  });

  const data = (await response.json()) as T & { error?: string };
  if (!response.ok) {
    logger.warn("request.failed", {
      method,
      url,
      status: response.status,
      error: data.error ?? `Request failed with status ${response.status}`
    });
    throw new ApiError(data.error ?? `Request failed with status ${response.status}`, response.status);
  }

  logger.debug("request.succeeded", {
    method,
    url,
    status: response.status
  });

  return data;
}

export async function createMeeting(input: {
  title: string;
  password: string;
  meetingType: "quick" | "scheduled";
  hostUserId: string;
  hostNickname: string;
  deviceType: string;
  clientProfile?: ClientProfile;
}): Promise<CreateMeetingResponse> {
  const response = await requestJSON<RawCreateMeetingResponse>("/api/meetings", {
    method: "POST",
    body: JSON.stringify(input)
  });
  return normalizeRuntimeIcePayload(response);
}

export async function requestRegisterCode(input: {
  email: string;
  nickname: string;
}): Promise<AuthCodeDelivery> {
  return requestJSON<AuthCodeDelivery>("/api/auth/register/code", {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export async function completeRegister(input: {
  email: string;
  code: string;
}): Promise<{ status: string; user: AuthUser }> {
  return requestJSON<{ status: string; user: AuthUser }>("/api/auth/register/verify", {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export async function requestLoginCode(input: { email: string }): Promise<AuthCodeDelivery> {
  return requestJSON<AuthCodeDelivery>("/api/auth/login/code", {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export async function completeLogin(input: {
  email: string;
  code: string;
}): Promise<AuthLoginResponse> {
  return requestJSON<AuthLoginResponse>("/api/auth/login/verify", {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export async function completePasswordLogin(input: {
  email: string;
  password: string;
}): Promise<AuthLoginResponse> {
  return requestJSON<AuthLoginResponse>("/api/auth/login/password", {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export async function fetchCurrentUser(): Promise<{ user: AuthUser; sessionEndsAt: string }> {
  return requestJSON<{ user: AuthUser; sessionEndsAt: string }>("/api/auth/me");
}

export async function logout(): Promise<{ status: string }> {
  return requestJSON<{ status: string }>("/api/auth/logout", {
    method: "POST"
  });
}

export async function joinMeeting(input: {
  meetingNumber: string;
  password: string;
  userId?: string;
  nickname: string;
  deviceType: string;
  clientProfile?: ClientProfile;
  isAnonymous: boolean;
  requestCameraEnabled?: boolean;
  requestMicrophoneEnabled?: boolean;
}): Promise<JoinMeetingResponse> {
  const response = await requestJSON<RawJoinMeetingResponse>(`/api/meetings/${meetingPathSegment(input.meetingNumber)}/join`, {
    method: "POST",
    body: JSON.stringify({
      password: input.password,
      userId: input.userId ?? "",
      nickname: input.nickname,
      deviceType: input.deviceType,
      clientProfile: input.clientProfile ?? {},
      isAnonymous: input.isAnonymous,
      requestCameraEnabled: input.requestCameraEnabled,
      requestMicrophoneEnabled: input.requestMicrophoneEnabled
    })
  });
  return normalizeRuntimeIcePayload(response);
}

export async function getMeeting(input: { meetingNumber: string }): Promise<GetMeetingResponse> {
  return requestJSON<GetMeetingResponse>(`/api/meetings/${meetingPathSegment(input.meetingNumber)}`);
}

export async function fetchMeetingIceServers(input: {
  meetingNumber: string;
  participantId: string;
}): Promise<MeetingIceServersResponse> {
  const response = await requestJSON<RawMeetingIceServersResponse>(
    `/api/meetings/${meetingPathSegment(input.meetingNumber)}/participants/${input.participantId}/ice-servers`,
    {
      method: "POST"
    }
  );
  return normalizeRuntimeIcePayload(response);
}

export async function endMeeting(input: {
  meetingNumber: string;
  hostParticipantId: string;
  deviceType: string;
}): Promise<{ status: string }> {
  return requestJSON<{ status: string }>(`/api/meetings/${meetingPathSegment(input.meetingNumber)}/end`, {
    method: "POST",
    body: JSON.stringify({
      hostParticipantId: input.hostParticipantId,
      deviceType: input.deviceType
    })
  });
}

export async function leaveMeeting(input: {
  meetingNumber: string;
  participantId: string;
  deviceType: string;
}): Promise<{ status: string }> {
  return requestJSON<{ status: string }>(
    `/api/meetings/${meetingPathSegment(input.meetingNumber)}/participants/${input.participantId}/leave`,
    {
      method: "POST",
      body: JSON.stringify({
        deviceType: input.deviceType
      })
    }
  );
}

export async function updateNickname(input: {
  meetingNumber: string;
  participantId: string;
  nickname: string;
}): Promise<{
  participant: Participant;
  previousNickname: string;
  systemMessage?: ChatMessage;
}> {
  return requestJSON<{
    participant: Participant;
    previousNickname: string;
    systemMessage?: ChatMessage;
  }>(`/api/meetings/${meetingPathSegment(input.meetingNumber)}/participants/${input.participantId}/nickname`, {
    method: "POST",
    body: JSON.stringify({
      nickname: input.nickname
    })
  });
}

export async function getMeetingMinutes(input: {
  meetingNumber: string;
  participantId: string;
}): Promise<MeetingMinutesSnapshot> {
  const query = new URLSearchParams({
    participantId: input.participantId
  });
  return requestJSON<MeetingMinutesSnapshot>(`/api/meetings/${meetingPathSegment(input.meetingNumber)}/minutes?${query.toString()}`);
}

export async function startTranscription(input: {
  meetingNumber: string;
  hostParticipantId: string;
}): Promise<{ status: string; transcription: Meeting["transcription"] }> {
  return requestJSON<{ status: string; transcription: Meeting["transcription"] }>(
    `/api/meetings/${meetingPathSegment(input.meetingNumber)}/transcription/start`,
    {
      method: "POST",
      body: JSON.stringify({
        hostParticipantId: input.hostParticipantId
      })
    }
  );
}

export async function getTranscript(input: {
  meetingNumber: string;
  participantId: string;
}): Promise<TranscriptResponse> {
  const query = new URLSearchParams({
    participantId: input.participantId
  });
  return requestJSON<TranscriptResponse>(`/api/meetings/${meetingPathSegment(input.meetingNumber)}/transcript?${query.toString()}`);
}

export async function uploadTranscriptionChunk(input: {
  meetingNumber: string;
  participantId: string;
  audio: Blob;
  sequence: number;
  startedAt: string;
  endedAt: string;
  language: string;
  mimeType: string;
  sampleRate: number;
}): Promise<{ status: string; segment?: TranscriptSegment | null }> {
  const form = new FormData();
  form.set("audio", input.audio, `chunk-${input.sequence}.wav`);
  form.set("sequence", String(input.sequence));
  form.set("startedAt", input.startedAt);
  form.set("endedAt", input.endedAt);
  form.set("language", input.language);
  form.set("mimeType", input.mimeType);
  form.set("sampleRate", String(input.sampleRate));
  return requestFormData<{ status: string; segment?: TranscriptSegment | null }>(
    `/api/meetings/${meetingPathSegment(input.meetingNumber)}/participants/${input.participantId}/transcription/chunks`,
    form
  );
}

export async function createMinutesJob(input: {
  meetingNumber: string;
  hostParticipantId: string;
}): Promise<MinutesJobResponse> {
  return requestJSON<MinutesJobResponse>(`/api/meetings/${meetingPathSegment(input.meetingNumber)}/minutes/jobs`, {
    method: "POST",
    body: JSON.stringify({
      hostParticipantId: input.hostParticipantId
    })
  });
}

export async function getMinutesJob(input: {
  meetingNumber: string;
  jobId: string;
}): Promise<MinutesJobResponse> {
  return requestJSON<MinutesJobResponse>(
    `/api/meetings/${meetingPathSegment(input.meetingNumber)}/minutes/jobs/${input.jobId}`
  );
}

export async function fetchMeetingHistory(): Promise<MeetingHistoryResponse> {
  return requestJSON<MeetingHistoryResponse>("/api/users/me/meeting-history");
}

export async function fetchPersistentMinutes(input: {
  minutesId: string;
}): Promise<PersistentMinutesResponse> {
  return requestJSON<PersistentMinutesResponse>(`/api/meeting-minutes/${input.minutesId}`);
}

export async function shareMinutes(input: {
  minutesId: string;
  userId: string;
}): Promise<{ status: string }> {
  return requestJSON<{ status: string }>(`/api/meeting-minutes/${input.minutesId}/shares`, {
    method: "POST",
    body: JSON.stringify({
      userId: input.userId
    })
  });
}

export async function reportAudit(input: {
  meetingNumber: string;
  participantId: string;
  userId?: string;
  participantRole: Participant["role"];
  deviceType: string;
  latencyMs: number;
  packetLossRate: number;
  averageFps: number;
  averageBitrateKbps: number;
  details: Record<string, unknown>;
  clientProfile?: ClientProfile;
}): Promise<{ status: string }> {
  return requestJSON<{ status: string }>(
    `/api/meetings/${meetingPathSegment(input.meetingNumber)}/participants/${input.participantId}/audit`,
    {
      method: "POST",
      body: JSON.stringify({
        userId: input.userId ?? "",
        participantRole: input.participantRole,
        deviceType: input.deviceType,
        latencyMs: Math.round(input.latencyMs),
        packetLossRate: input.packetLossRate,
        averageFps: input.averageFps,
        averageBitrateKbps: Math.round(input.averageBitrateKbps),
        details: {
          ...input.details,
          clientProfile: input.clientProfile ?? {}
        }
      })
    }
  );
}
