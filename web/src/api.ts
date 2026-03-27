import { createClientLogger } from "./logger";
import type { ChatMessage, Meeting, Participant, ReadyCheckRound, WhiteboardAction } from "./types";

type CreateMeetingResponse = {
  meeting: Meeting;
  host: Participant;
};

type JoinMeetingResponse = {
  meeting: Meeting;
  participant: Participant;
};

type GetMeetingResponse = {
  meeting: Meeting;
};

export type MeetingMinutesSnapshot = {
  meetingId: string;
  title: string;
  chatMessages: ChatMessage[];
  whiteboardActions: WhiteboardAction[];
  temporaryMinutes: string[];
  activeReadyCheck?: ReadyCheckRound;
};

const logger = createClientLogger("frontend.api");

async function requestJSON<T>(input: RequestInfo, init?: RequestInit): Promise<T> {
  const method = init?.method ?? "GET";
  const url = typeof input === "string" ? input : input instanceof URL ? input.toString() : "request";
  logger.debug("request.started", {
    method,
    url
  });

  const response = await fetch(input, {
    ...init,
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
    throw new Error(data.error ?? `Request failed with status ${response.status}`);
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
  hostUserId: string;
  hostNickname: string;
  deviceType: string;
}): Promise<CreateMeetingResponse> {
  return requestJSON<CreateMeetingResponse>("/api/meetings", {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export async function joinMeeting(input: {
  meetingId: string;
  password: string;
  userId?: string;
  nickname: string;
  deviceType: string;
  isAnonymous: boolean;
  requestCameraEnabled?: boolean;
  requestMicrophoneEnabled?: boolean;
}): Promise<JoinMeetingResponse> {
  return requestJSON<JoinMeetingResponse>(`/api/meetings/${input.meetingId}/join`, {
    method: "POST",
    body: JSON.stringify({
      password: input.password,
      userId: input.userId ?? "",
      nickname: input.nickname,
      deviceType: input.deviceType,
      isAnonymous: input.isAnonymous,
      requestCameraEnabled: input.requestCameraEnabled,
      requestMicrophoneEnabled: input.requestMicrophoneEnabled
    })
  });
}

export async function getMeeting(input: { meetingId: string }): Promise<GetMeetingResponse> {
  return requestJSON<GetMeetingResponse>(`/api/meetings/${input.meetingId}`);
}

export async function endMeeting(input: {
  meetingId: string;
  hostParticipantId: string;
  deviceType: string;
}): Promise<{ status: string }> {
  return requestJSON<{ status: string }>(`/api/meetings/${input.meetingId}/end`, {
    method: "POST",
    body: JSON.stringify({
      hostParticipantId: input.hostParticipantId,
      deviceType: input.deviceType
    })
  });
}

export async function leaveMeeting(input: {
  meetingId: string;
  participantId: string;
  deviceType: string;
}): Promise<{ status: string }> {
  return requestJSON<{ status: string }>(
    `/api/meetings/${input.meetingId}/participants/${input.participantId}/leave`,
    {
      method: "POST",
      body: JSON.stringify({
        deviceType: input.deviceType
      })
    }
  );
}

export async function updateNickname(input: {
  meetingId: string;
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
  }>(`/api/meetings/${input.meetingId}/participants/${input.participantId}/nickname`, {
    method: "POST",
    body: JSON.stringify({
      nickname: input.nickname
    })
  });
}

export async function getMeetingMinutes(input: {
  meetingId: string;
  participantId: string;
}): Promise<MeetingMinutesSnapshot> {
  const url = new URL(`/api/meetings/${input.meetingId}/minutes`, window.location.origin);
  url.searchParams.set("participantId", input.participantId);
  return requestJSON<MeetingMinutesSnapshot>(url.toString());
}

export async function reportAudit(input: {
  meetingId: string;
  participantId: string;
  userId?: string;
  participantRole: Participant["role"];
  deviceType: string;
  latencyMs: number;
  packetLossRate: number;
  averageFps: number;
  averageBitrateKbps: number;
  details: Record<string, unknown>;
}): Promise<{ status: string }> {
  return requestJSON<{ status: string }>(
    `/api/meetings/${input.meetingId}/participants/${input.participantId}/audit`,
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
        details: input.details
      })
    }
  );
}
