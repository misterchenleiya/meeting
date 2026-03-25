export type Capability =
  | "chat"
  | "microphone"
  | "camera"
  | "whiteboard"
  | "screen_share"
  | "record"
  | "ready_check";

export type Participant = {
  id: string;
  userId?: string;
  nickname: string;
  role: "host" | "assistant" | "participant";
  isAnonymous: boolean;
  joinedAt: string;
  leftAt?: string;
  requestedMediaPreference: {
    cameraEnabled: boolean;
    microphoneEnabled: boolean;
  };
  effectiveMediaState: {
    cameraEnabled: boolean;
    microphoneEnabled: boolean;
  };
  grantedCapabilities: Record<string, unknown>;
};

export type ChatMessage = {
  id: string;
  participantId: string;
  nickname: string;
  message: string;
  sentAt: string;
};

export type WhiteboardPoint = {
  x: number;
  y: number;
};

export type WhiteboardAction = {
  id: string;
  kind: string;
  color: string;
  strokeWidth: number;
  points: WhiteboardPoint[];
  createdBy: string;
  createdAt: string;
};

export type ReadyCheckStatus = "pending" | "confirmed" | "cancelled" | "timeout";

export type ReadyCheckResult = {
  participantId: string;
  status: ReadyCheckStatus;
  updatedAt?: string;
};

export type ReadyCheckRound = {
  id: string;
  startedBy: string;
  timeoutSeconds: number;
  startedAt: string;
  deadlineAt: string;
  status: "active" | "completed";
  results: Record<string, ReadyCheckResult>;
};

export type Meeting = {
  id: string;
  joinCode: string;
  passwordRequired: boolean;
  title: string;
  hostParticipantId: string;
  status: "active" | "ended";
  createdAt: string;
  endedAt?: string;
  participants: Record<string, Participant>;
  chatMessages?: ChatMessage[];
  whiteboardActions?: WhiteboardAction[];
  activeReadyCheck?: ReadyCheckRound;
  temporaryMinutes?: string[];
};

export type EventRecord = {
  id: string;
  type: string;
  text: string;
  createdAt: string;
};
