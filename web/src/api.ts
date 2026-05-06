import { createClientLogger } from "./logger";
import { normalizeIceServers, resolveApiUrl, type RuntimeIceServer } from "./runtime-config";
import type { ClientProfile } from "./clientProfile";
import type {
  AuthUser,
  ChatMessage,
  Meeting,
  Participant,
  ReadyCheckRound,
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
