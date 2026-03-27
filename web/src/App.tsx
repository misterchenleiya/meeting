import { startTransition, useEffect, useEffectEvent, useRef, useState, type ReactNode } from "react";
import jsQR from "jsqr";
import QRCode from "qrcode";
import { createClientLogger, formatClientLogs } from "./logger";
import {
  createMeeting,
  endMeeting,
  getMeeting,
  joinMeeting,
  leaveMeeting,
  reportAudit,
  updateNickname
} from "./api";
import {
  discardRecording,
  downloadRecording,
  LocalRecordingSession,
  type RecordingAsset,
  type RecordingKind,
  startLocalRecording
} from "./recording";
import { PeerMesh, type PeerStatsSnapshot } from "./rtc";
import { SignalClient, type SignalEnvelope } from "./signaling";
import type {
  Capability,
  ChatMessage,
  EventRecord,
  Meeting,
  Participant,
  ReadyCheckRound,
  ReadyCheckStatus,
  WhiteboardAction
} from "./types";
import { WhiteboardPanel } from "./whiteboard";

const deviceType = "browser";
const requestableCapabilities: Capability[] = [
  "camera",
  "microphone",
  "whiteboard",
  "screen_share",
  "record",
  "ready_check"
];

const logger = createClientLogger("frontend.app");

type SessionState = {
  meeting: Meeting;
  participant: Participant;
};

type RemoteTile = {
  participantId: string;
  stream: MediaStream;
  connectionState: RTCPeerConnectionState;
};

type AuditCounter = {
  timestamp: number;
  bytes: number;
};

type AuditSummary = {
  latencyMs: number;
  packetLossRate: number;
  averageFps: number;
  averageBitrateKbps: number;
  peerCount: number;
  updatedAt: string;
};

type EntryView = "login" | "home" | "schedule" | "join";
type SidebarView = "none" | "members" | "chat";
type MenuView = "none" | "host" | "participant";
type AttachedPanelView = "none" | "settings" | "apps" | "end";
type ModalView =
  | "none"
  | "invite"
  | "record_request"
  | "meeting_ended"
  | "nickname"
  | "permissions"
  | "ready_check_panel"
  | "recording_panel"
  | "whiteboard_panel"
  | "audit_panel";

type StageItem = {
  id: string;
  participantId: string;
  role: Participant["role"];
  label: string;
  stream: MediaStream;
  variant: "camera" | "screen";
  isLocal: boolean;
  micEnabled: boolean;
  meta: string;
};

type LoginFormState = {
  email: string;
  password: string;
};

type ScheduleFormState = {
  title: string;
  scheduledAt: string;
  timezone: string;
  password: string;
};

type JoinFormState = {
  meetingId: string;
  password: string;
  nickname: string;
  requestCameraEnabled: boolean;
  requestMicrophoneEnabled: boolean;
};

type PersistedAppState = {
  isAuthenticated: boolean;
  entryView: EntryView;
  loginForm: LoginFormState;
  scheduleForm: ScheduleFormState;
  joinForm: JoinFormState;
  meetingAccessPassword: string;
  meetingSession: SessionState | null;
  returnAfterMeetingView: EntryView;
};

const appStateStorageKey = "meeting:app-state:v2";
const defaultEntryStatusMessage = "准备开始会议";
const defaultLoginForm: LoginFormState = {
  email: "",
  password: ""
};
const defaultJoinForm: JoinFormState = {
  meetingId: "",
  password: "",
  nickname: "匿名用户",
  requestCameraEnabled: false,
  requestMicrophoneEnabled: false
};

function App() {
  const initialPersistedState = useRef(readPersistedAppState()).current;
  const initialMeetingSession = initialPersistedState?.meetingSession ?? null;
  const signalClientRef = useRef<SignalClient | null>(null);
  const meshRef = useRef<PeerMesh | null>(null);
  const sessionRef = useRef<SessionState | null>(null);
  const localStreamRef = useRef<MediaStream | null>(null);
  const baseMediaStreamRef = useRef<MediaStream | null>(null);
  const screenShareStreamRef = useRef<MediaStream | null>(null);
  const recorderRef = useRef<LocalRecordingSession | null>(null);
  const auditBaselineRef = useRef<Map<string, AuditCounter>>(new Map());
  const joinScannerVideoRef = useRef<HTMLVideoElement | null>(null);
  const joinScannerCanvasRef = useRef<HTMLCanvasElement | null>(null);
  const joinScannerFileInputRef = useRef<HTMLInputElement | null>(null);
  const joinScannerStreamRef = useRef<MediaStream | null>(null);
  const joinScannerFrameRef = useRef<number | null>(null);
  const recordingAssetRef = useRef<RecordingAsset | null>(null);

  const [meetingSession, setMeetingSession] = useState<SessionState | null>(initialMeetingSession);
  const [pendingSignalSession, setPendingSignalSession] = useState<SessionState | null>(
    initialMeetingSession
  );
  const [events, setEvents] = useState<EventRecord[]>([]);
  const [onlineParticipantIds, setOnlineParticipantIds] = useState<string[]>([]);
  const [wsConnected, setWsConnected] = useState(false);
  const [statusMessage, setStatusMessage] = useState(
    initialMeetingSession ? "已恢复会议，正在重连信令" : defaultEntryStatusMessage
  );
  const [errorMessage, setErrorMessage] = useState("");
  const [chatInput, setChatInput] = useState("");
  const [nicknameDraft, setNicknameDraft] = useState("");
  const [capabilityToRequest, setCapabilityToRequest] = useState<Capability>("camera");
  const [grantTargetId, setGrantTargetId] = useState("");
  const [grantCapability, setGrantCapability] = useState<Capability>("camera");
  const [assistantTargetId, setAssistantTargetId] = useState("");
  const [readyCheckTimeoutSeconds, setReadyCheckTimeoutSeconds] = useState(15);
  const [localStream, setLocalStream] = useState<MediaStream | null>(null);
  const [remoteTiles, setRemoteTiles] = useState<RemoteTile[]>([]);
  const [chatMessages, setChatMessages] = useState<ChatMessage[]>(
    initialMeetingSession?.meeting.chatMessages ?? []
  );
  const [whiteboardActions, setWhiteboardActions] = useState<WhiteboardAction[]>(
    initialMeetingSession?.meeting.whiteboardActions ?? []
  );
  const [activeReadyCheck, setActiveReadyCheck] = useState<ReadyCheckRound | null>(
    initialMeetingSession?.meeting.activeReadyCheck ?? null
  );
  const [temporaryMinutes, setTemporaryMinutes] = useState<string[]>(
    initialMeetingSession?.meeting.temporaryMinutes ?? []
  );
  const [auditSummary, setAuditSummary] = useState<AuditSummary | null>(null);
  const [recordingKind, setRecordingKind] = useState<RecordingKind>("meeting_video");
  const [recordingAsset, setRecordingAsset] = useState<RecordingAsset | null>(null);
  const [recordingActive, setRecordingActive] = useState(false);
  const [screenSharing, setScreenSharing] = useState(false);
  const [captureSelection, setCaptureSelection] = useState({
    camera: false,
    microphone: false
  });
  const [isAuthenticated, setIsAuthenticated] = useState(initialPersistedState?.isAuthenticated ?? false);
  const [entryView, setEntryView] = useState<EntryView>(initialPersistedState?.entryView ?? "login");
  const [loginForm, setLoginForm] = useState<LoginFormState>(
    initialPersistedState?.loginForm ?? defaultLoginForm
  );
  const [scheduleForm, setScheduleForm] = useState<ScheduleFormState>(
    initialPersistedState?.scheduleForm ?? buildDefaultScheduleForm()
  );
  const [joinForm, setJoinForm] = useState<JoinFormState>(
    initialPersistedState?.joinForm ?? defaultJoinForm
  );
  const [joinLookupMeeting, setJoinLookupMeeting] = useState<Meeting | null>(null);
  const [showJoinPasswordModal, setShowJoinPasswordModal] = useState(false);
  const [currentSidebar, setCurrentSidebar] = useState<SidebarView>("none");
  const [currentModal, setCurrentModal] = useState<ModalView>("none");
  const [currentMenu, setCurrentMenu] = useState<MenuView>("none");
  const [currentAttachedPanel, setCurrentAttachedPanel] = useState<AttachedPanelView>("none");
  const [featuredStageId, setFeaturedStageId] = useState<string | null>(null);
  const [meetingAccessPassword, setMeetingAccessPassword] = useState(
    initialPersistedState?.meetingAccessPassword ?? ""
  );
  const [returnAfterMeetingView, setReturnAfterMeetingView] = useState<EntryView>(
    initialPersistedState?.returnAfterMeetingView ?? "home"
  );
  const [showJoinScanModal, setShowJoinScanModal] = useState(false);
  const [joinScanStatus, setJoinScanStatus] = useState("请将二维码对准取景框");
  const [joinScanError, setJoinScanError] = useState("");
  const [shareQrDataUrl, setShareQrDataUrl] = useState("");
  const [endingMeetingPending, setEndingMeetingPending] = useState(false);
  const [fullscreenActive, setFullscreenActive] = useState(false);
  const endingMeetingRef = useRef(false);
  const meetingEndSummaryPreparedRef = useRef(false);

  useEffect(() => {
    sessionRef.current = meetingSession;
  }, [meetingSession]);

  useEffect(() => {
    localStreamRef.current = localStream;
  }, [localStream]);

  useEffect(() => {
    if (!meetingSession) {
      return;
    }

    setCaptureSelection({
      camera: meetingSession.participant.requestedMediaPreference.cameraEnabled,
      microphone: meetingSession.participant.requestedMediaPreference.microphoneEnabled
    });
  }, [meetingSession?.participant.id]);

  useEffect(() => {
    setNicknameDraft(meetingSession?.participant.nickname ?? "");
  }, [meetingSession?.participant.id, meetingSession?.participant.nickname]);

  useEffect(() => {
    recordingAssetRef.current = recordingAsset;
  }, [recordingAsset]);

  useEffect(() => {
    if (typeof document === "undefined") {
      return;
    }

    const syncFullscreenState = () => {
      setFullscreenActive(Boolean(document.fullscreenElement));
    };

    syncFullscreenState();
    document.addEventListener("fullscreenchange", syncFullscreenState);
    return () => {
      document.removeEventListener("fullscreenchange", syncFullscreenState);
    };
  }, []);

  useEffect(() => {
    if (currentModal === "none") {
      return;
    }

    setCurrentMenu("none");
    setCurrentAttachedPanel("none");
  }, [currentModal]);

  useEffect(() => {
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key !== "Escape") {
        return;
      }

      if (currentAttachedPanel !== "none") {
        setCurrentAttachedPanel("none");
        return;
      }

      if (currentMenu !== "none") {
        setCurrentMenu("none");
        return;
      }

      if (currentSidebar !== "none") {
        setCurrentSidebar("none");
        return;
      }

      if (currentModal !== "none" && currentModal !== "meeting_ended") {
        setCurrentModal("none");
      }
    };

    window.addEventListener("keydown", handleEscape);
    return () => {
      window.removeEventListener("keydown", handleEscape);
    };
  }, [currentAttachedPanel, currentMenu, currentModal, currentSidebar]);

  function stopJoinScanner() {
    if (joinScannerFrameRef.current !== null) {
      window.cancelAnimationFrame(joinScannerFrameRef.current);
      joinScannerFrameRef.current = null;
    }
    joinScannerStreamRef.current?.getTracks().forEach((track) => track.stop());
    joinScannerStreamRef.current = null;
    if (joinScannerVideoRef.current) {
      joinScannerVideoRef.current.srcObject = null;
    }
  }

  function openJoinScanModal() {
    logger.info("join.scan_modal_opened", {
      entryView
    });
    setJoinScanStatus("请将二维码对准取景框");
    setJoinScanError("");
    setShowJoinScanModal(true);
  }

  useEffect(() => {
    writePersistedAppState({
      isAuthenticated,
      entryView,
      loginForm,
      scheduleForm,
      joinForm,
      meetingAccessPassword,
      meetingSession: currentModal === "meeting_ended" ? null : meetingSession,
      returnAfterMeetingView
    });
  }, [
    currentModal,
    entryView,
    isAuthenticated,
    joinForm,
    loginForm,
    meetingAccessPassword,
    meetingSession,
    returnAfterMeetingView,
    scheduleForm
  ]);

  useEffect(() => {
    if (currentModal !== "invite" || !meetingSession) {
      setShareQrDataUrl("");
      return;
    }

    let cancelled = false;
    void QRCode.toDataURL(buildMeetingQRCodePayload(meetingSession.meeting, meetingAccessPassword), {
      margin: 1,
      width: 240,
      color: {
        dark: "#f5f5f7",
        light: "#00000000"
      }
    })
      .then((dataUrl) => {
        if (!cancelled) {
          setShareQrDataUrl(dataUrl);
        }
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setShareQrDataUrl("");
          setErrorMessage(asMessage(error));
        }
      });

    return () => {
      cancelled = true;
    };
  }, [currentModal, meetingAccessPassword, meetingSession]);

  const appendEvent = useEffectEvent((type: string, text: string) => {
    startTransition(() => {
      setEvents((current) => [
        {
          id: createLocalEventID(),
          type,
          text,
          createdAt: new Date().toLocaleTimeString("zh-CN", {
            hour12: false
          })
        },
        ...current
      ].slice(0, 80));
    });
  });

  const appendTemporaryMinute = useEffectEvent((text: string, timestamp: string | Date = new Date()) => {
    const date = typeof timestamp === "string" ? new Date(timestamp) : timestamp;
    const safeDate = Number.isNaN(date.getTime()) ? new Date() : date;
    const formatted = `[${safeDate.toLocaleTimeString("zh-CN", { hour12: false })}] ${text}`;
    setTemporaryMinutes((current) => [...current, formatted].slice(-160));
  });

  const replaceMeetingSnapshot = useEffectEvent((meeting: Meeting) => {
    setMeetingSession((current) => {
      if (!current) {
        return current;
      }

      return {
        meeting,
        participant: meeting.participants[current.participant.id] ?? current.participant
      };
    });
    setChatMessages(meeting.chatMessages ?? []);
    setWhiteboardActions(meeting.whiteboardActions ?? []);
    setActiveReadyCheck(meeting.activeReadyCheck ?? null);
    setTemporaryMinutes(meeting.temporaryMinutes ?? []);
  });

  const upsertParticipant = useEffectEvent((participant: Participant) => {
    setMeetingSession((current) => {
      if (!current) {
        return current;
      }

      return {
        meeting: {
          ...current.meeting,
          participants: {
            ...current.meeting.participants,
            [participant.id]: participant
          }
        },
        participant: current.participant.id === participant.id ? participant : current.participant
      };
    });
  });

  const syncParticipantCapability = useEffectEvent((participantId: string, capability: string) => {
    setMeetingSession((current) => {
      if (!current) {
        return current;
      }

      const participant = current.meeting.participants[participantId];
      if (!participant) {
        return current;
      }

      const nextParticipant = {
        ...participant,
        grantedCapabilities: {
          ...participant.grantedCapabilities,
          [capability]: {}
        }
      };

      return {
        meeting: {
          ...current.meeting,
          participants: {
            ...current.meeting.participants,
            [participantId]: nextParticipant
          }
        },
        participant:
          current.participant.id === participantId ? nextParticipant : current.participant
      };
    });
  });

  const applyNicknameUpdate = useEffectEvent((payload: {
    participant: Participant;
    previousNickname: string;
    systemMessage?: ChatMessage;
  }) => {
    upsertParticipant(payload.participant);
    setNicknameDraft((current) => {
      const localParticipantId = sessionRef.current?.participant.id;
      if (localParticipantId !== payload.participant.id) {
        return current;
      }
      return payload.participant.nickname;
    });

    if (payload.systemMessage) {
      const systemMessage = payload.systemMessage;
      setChatMessages((current) =>
        current.some((message) => message.id === systemMessage.id)
          ? current
          : [...current, systemMessage]
      );
      appendTemporaryMinute(systemMessage.message, systemMessage.sentAt);
    }

    appendEvent(
      "participant.nickname_updated",
      `${payload.previousNickname} 已将昵称修改为 ${payload.participant.nickname}`
    );
  });

  const ensurePeerMesh = useEffectEvent((session: SessionState) => {
    const currentMesh = meshRef.current;
    if (currentMesh && sessionRef.current?.participant.id === session.participant.id) {
      return currentMesh;
    }

    currentMesh?.close();
    setRemoteTiles([]);

    const mesh = new PeerMesh(
      session.participant.id,
      (type, payload) => {
        signalClientRef.current?.send(type, payload);
      },
      {
        onRemoteStream: (participantId, stream) => {
          setRemoteTiles((current) => {
            const existing = current.find((tile) => tile.participantId === participantId);
            const next = current.filter((tile) => tile.participantId !== participantId);
            next.push({
              participantId,
              stream,
              connectionState: existing?.connectionState ?? "new"
            });
            return next;
          });
        },
        onRemoteStreamRemoved: (participantId) => {
          setRemoteTiles((current) => current.filter((tile) => tile.participantId !== participantId));
        },
        onPeerStateChange: (participantId, connectionState) => {
          setRemoteTiles((current) => {
            const tile = current.find((item) => item.participantId === participantId);
            if (!tile) {
              return current;
            }
            return current.map((item) =>
              item.participantId === participantId ? { ...item, connectionState } : item
            );
          });
          appendEvent("rtc.state", `${participantId} 连接状态: ${connectionState}`);
        },
        onError: (message) => {
          setErrorMessage(message);
          appendEvent("rtc.error", message);
        }
      }
    );

    meshRef.current = mesh;
    mesh.setLocalStream(localStreamRef.current).catch((error) => {
      setErrorMessage(asMessage(error));
    });
    return mesh;
  });

  const disposeRtc = useEffectEvent((stopLocalTracks: boolean) => {
    meshRef.current?.close();
    meshRef.current = null;
    setRemoteTiles([]);

    if (stopLocalTracks) {
      baseMediaStreamRef.current?.getTracks().forEach((track) => track.stop());
      screenShareStreamRef.current?.getTracks().forEach((track) => track.stop());
      recorderRef.current?.cancel();
      recorderRef.current = null;
      setRecordingActive(false);
      localStreamRef.current = null;
      baseMediaStreamRef.current = null;
      screenShareStreamRef.current = null;
      setLocalStream(null);
      setScreenSharing(false);
      setCaptureSelection({
        camera: false,
        microphone: false
      });
    }
  });

  const exitMeetingShell = useEffectEvent((nextStatus: string, nextEntryView?: EntryView) => {
    sessionRef.current = null;
    endingMeetingRef.current = false;
    meetingEndSummaryPreparedRef.current = false;
    setEndingMeetingPending(false);
    setPendingSignalSession(null);
    signalClientRef.current?.close();
    signalClientRef.current = null;
    disposeRtc(true);
    setMeetingSession(null);
    setChatMessages([]);
    setWhiteboardActions([]);
    setActiveReadyCheck(null);
    setTemporaryMinutes([]);
    setAuditSummary(null);
    setEvents([]);
    setOnlineParticipantIds([]);
    setCurrentSidebar("none");
    setCurrentModal("none");
    setFeaturedStageId(null);
    setJoinLookupMeeting(null);
    setShowJoinPasswordModal(false);
    setWsConnected(false);
    setMeetingAccessPassword("");
    setJoinForm((current) => ({
      ...current,
      password: ""
    }));
    setStatusMessage(nextStatus);
    setErrorMessage("");
    setEntryView(nextEntryView ?? (isAuthenticated ? "home" : "login"));
    scrollViewportToTop();
  });

  const preparePostEndSummary = useEffectEvent(() => {
    if (meetingEndSummaryPreparedRef.current) {
      logger.debug("meeting.end_summary_skipped", {
        reason: "already_prepared"
      });
      return;
    }

    meetingEndSummaryPreparedRef.current = true;
    endingMeetingRef.current = false;
    logger.info("meeting.end_summary_preparing", {
      meetingId: sessionRef.current?.meeting.id ?? meetingSession?.meeting.id ?? "",
      participantId: sessionRef.current?.participant.id ?? meetingSession?.participant.id ?? ""
    });
    setEndingMeetingPending(false);
    sessionRef.current = null;
    setPendingSignalSession(null);
    signalClientRef.current?.close();
    signalClientRef.current = null;
    disposeRtc(true);
    setMeetingSession((current) =>
      current
        ? {
            ...current,
            meeting: {
              ...current.meeting,
              status: "ended",
              endedAt: new Date().toISOString()
            }
          }
        : current
    );
    setWsConnected(false);
    setCurrentSidebar("none");
    setCurrentModal("meeting_ended");
    setStatusMessage("会议已结束，请选择保存当前会议纪要或返回");
    setErrorMessage("");
  });

  const onSignalMessage = useEffectEvent((event: SignalEnvelope) => {
    switch (event.type) {
      case "session.welcome": {
        const payload = event.payload as {
          meeting: Meeting;
          onlineParticipantIds: string[];
        };
        setOnlineParticipantIds(payload.onlineParticipantIds);
        replaceMeetingSnapshot(payload.meeting);
        const currentSession = sessionRef.current;
        if (currentSession) {
          const mesh = ensurePeerMesh(currentSession);
          mesh
            .syncParticipants(
              payload.onlineParticipantIds.filter((id) => id !== currentSession.participant.id)
            )
            .catch((error) => {
              setErrorMessage(asMessage(error));
            });
        }
        appendEvent("session.welcome", "信令连接建立，已收到房间快照");
        return;
      }
      case "participant.joined": {
        const payload = event.payload as { participant: Participant };
        upsertParticipant(payload.participant);
        appendEvent("participant.joined", `${payload.participant.nickname} 已加入会议`);
        appendTemporaryMinute(`${payload.participant.nickname} 加入会议。`, payload.participant.joinedAt);
        return;
      }
      case "participant.left": {
        const payload = event.payload as { participantId: string };
        const participantLabel = findParticipantLabel(
          Object.values(sessionRef.current?.meeting.participants ?? {}),
          payload.participantId
        );
        setMeetingSession((current) => {
          if (!current) {
            return current;
          }
          const nextParticipants = { ...current.meeting.participants };
          delete nextParticipants[payload.participantId];
          return {
            ...current,
            meeting: {
              ...current.meeting,
              participants: nextParticipants
            }
          };
        });
        meshRef.current?.removePeer(payload.participantId);
        setOnlineParticipantIds((current) => current.filter((id) => id !== payload.participantId));
        appendEvent("participant.left", `${participantLabel} 已离开会议`);
        appendTemporaryMinute(`${participantLabel} 离开会议。`);
        return;
      }
      case "participant.online": {
        const payload = event.payload as { participantId: string };
        setOnlineParticipantIds((current) => {
          const next = Array.from(new Set([...current, payload.participantId]));
          const currentSession = sessionRef.current;
          if (currentSession && payload.participantId !== currentSession.participant.id) {
            meshRef.current
              ?.syncParticipants(next.filter((id) => id !== currentSession.participant.id))
              .catch((error) => {
                setErrorMessage(asMessage(error));
              });
          }
          return next;
        });
        appendEvent("participant.online", `${payload.participantId} 信令在线`);
        return;
      }
      case "participant.offline": {
        const payload = event.payload as { participantId: string };
        setOnlineParticipantIds((current) => current.filter((id) => id !== payload.participantId));
        appendEvent("participant.offline", `${payload.participantId} 信令离线`);
        return;
      }
      case "capability.requested": {
        const payload = event.payload as { fromParticipantId: string; capability: string };
        appendEvent("capability.requested", `${payload.fromParticipantId} 请求 ${payload.capability} 权限`);
        return;
      }
      case "capability.granted": {
        const payload = event.payload as {
          targetParticipantId: string;
          grantedBy: string;
          capability: string;
        };
        const currentParticipants = Object.values(sessionRef.current?.meeting.participants ?? {});
        syncParticipantCapability(payload.targetParticipantId, payload.capability);
        appendEvent(
          "capability.granted",
          `${payload.grantedBy} 已向 ${payload.targetParticipantId} 授权 ${payload.capability}`
        );
        appendTemporaryMinute(
          `${findParticipantLabel(currentParticipants, payload.grantedBy)} 已向 ${findParticipantLabel(currentParticipants, payload.targetParticipantId)} 授权 ${payload.capability}。`
        );
        return;
      }
      case "role.assistant_assigned": {
        const payload = event.payload as { participant: Participant; assignedBy: string };
        const currentParticipants = Object.values(sessionRef.current?.meeting.participants ?? {});
        upsertParticipant(payload.participant);
        appendEvent(
          "role.assistant_assigned",
          `${payload.assignedBy} 已将 ${payload.participant.nickname} 设为助理`
        );
        appendTemporaryMinute(
          `${findParticipantLabel(currentParticipants, payload.assignedBy)} 已将 ${payload.participant.nickname} 设为助理。`
        );
        return;
      }
      case "participant.nickname_updated": {
        const payload = event.payload as {
          participant: Participant;
          previousNickname: string;
          systemMessage?: ChatMessage;
        };
        applyNicknameUpdate(payload);
        return;
      }
      case "whiteboard.action": {
        const payload = event.payload as { action: WhiteboardAction };
        setWhiteboardActions((current) =>
          current.some((action) => action.id === payload.action.id)
            ? current
            : [...current, payload.action]
        );
        appendEvent("whiteboard.action", `${payload.action.createdBy} 添加了一条白板笔迹`);
        return;
      }
      case "whiteboard.cleared": {
        setWhiteboardActions([]);
        appendEvent("whiteboard.cleared", "白板已清空");
        appendTemporaryMinute("白板已清空。");
        return;
      }
      case "ready_check.started":
      case "ready_check.updated":
      case "ready_check.finished": {
        const payload = event.payload as { round: ReadyCheckRound };
        const currentParticipants = Object.values(sessionRef.current?.meeting.participants ?? {});
        setActiveReadyCheck(payload.round);
        appendEvent(event.type, `就位确认轮次 ${payload.round.id} 已更新`);
        if (event.type === "ready_check.started") {
          appendTemporaryMinute(
            `${findParticipantLabel(currentParticipants, payload.round.startedBy)} 发起了就位确认，超时时间 ${payload.round.timeoutSeconds} 秒。`,
            payload.round.startedAt
          );
        }
        if (event.type === "ready_check.finished") {
          appendTemporaryMinute(`就位确认已结束：${summarizeReadyCheckRound(payload.round)}。`);
        }
        return;
      }
      case "chat.message": {
        const payload = event.payload as { message: ChatMessage };
        setChatMessages((current) =>
          current.some((message) => message.id === payload.message.id)
            ? current
            : [...current, payload.message]
        );
        appendEvent("chat.message", `${payload.message.nickname}: ${payload.message.message}`);
        appendTemporaryMinute(`${payload.message.nickname}：${payload.message.message}`, payload.message.sentAt);
        return;
      }
      case "meeting.ended": {
        appendEvent("meeting.ended", "会议已结束，运行态将被清理");
        logger.info("signal.meeting_ended_received", {
          meetingId: sessionRef.current?.meeting.id ?? "",
          participantId: sessionRef.current?.participant.id ?? "",
          participantRole: sessionRef.current?.participant.role ?? "",
          endingMeetingFlow: endingMeetingRef.current
        });
        if (endingMeetingRef.current && sessionRef.current?.participant.role === "host") {
          endingMeetingRef.current = false;
          preparePostEndSummary();
          return;
        }
        exitMeetingShell("会议已结束");
        return;
      }
      case "signal.offer":
      case "signal.answer":
      case "signal.ice_candidate": {
        const payload = event.payload as {
          fromParticipantId: string;
          data: RTCSessionDescriptionInit | RTCIceCandidateInit;
        };
        meshRef.current
          ?.handleSignal(event.type, payload.fromParticipantId, payload.data)
          .catch((error) => {
            setErrorMessage(asMessage(error));
          });
        appendEvent(event.type, `收到来自 ${payload.fromParticipantId} 的 ${event.type}`);
        return;
      }
      case "error": {
        const payload = event.payload as { message: string };
        setErrorMessage(payload.message);
        appendEvent("error", payload.message);
        return;
      }
      default:
        appendEvent(event.type, "收到未处理的信令事件");
    }
  });

  useEffect(() => {
    return () => {
      stopJoinScanner();
      signalClientRef.current?.close();
      disposeRtc(true);
      discardRecording(recordingAssetRef.current);
    };
  }, []);

  const submitAuditReport = useEffectEvent(async () => {
    const currentSession = sessionRef.current;
    if (!currentSession || !wsConnected) {
      return;
    }

    const snapshots = (await meshRef.current?.collectStats()) ?? [];
    const aggregated = aggregatePeerStats(snapshots, auditBaselineRef.current);

    setAuditSummary({
      latencyMs: aggregated.latencyMs,
      packetLossRate: aggregated.packetLossRate,
      averageFps: aggregated.averageFps,
      averageBitrateKbps: aggregated.averageBitrateKbps,
      peerCount: aggregated.peerCount,
      updatedAt: new Date().toLocaleTimeString("zh-CN", { hour12: false })
    });

    await reportAudit({
      meetingId: currentSession.meeting.id,
      participantId: currentSession.participant.id,
      userId: currentSession.participant.userId,
      participantRole: currentSession.participant.role,
      deviceType,
      latencyMs: aggregated.latencyMs,
      packetLossRate: aggregated.packetLossRate,
      averageFps: aggregated.averageFps,
      averageBitrateKbps: aggregated.averageBitrateKbps,
      details: {
        peerCount: aggregated.peerCount,
        screenSharing,
        localVideoTracks: localStreamRef.current?.getVideoTracks().length ?? 0,
        localAudioTracks: localStreamRef.current?.getAudioTracks().length ?? 0,
        peers: aggregated.perPeer
      }
    });
  });

  useEffect(() => {
    if (!meetingSession || !wsConnected) {
      auditBaselineRef.current.clear();
      setAuditSummary(null);
      return;
    }

    let cancelled = false;
    const run = async () => {
      try {
        await submitAuditReport();
      } catch (error) {
        if (cancelled) {
          return;
        }
        setErrorMessage(asMessage(error));
        appendEvent("audit.error", `基础审计上报失败: ${asMessage(error)}`);
      }
    };

    void run();
    const timer = window.setInterval(() => {
      void run();
    }, 15000);

    return () => {
      cancelled = true;
      window.clearInterval(timer);
      auditBaselineRef.current.clear();
    };
  }, [
    meetingSession?.meeting.id,
    meetingSession?.participant.id,
    meetingSession?.participant.role,
    meetingSession?.participant.userId,
    screenSharing,
    wsConnected
  ]);

  const applyOutboundStream = useEffectEvent(async (stream: MediaStream | null) => {
    localStreamRef.current = stream;
    setLocalStream(stream);
    await meshRef.current?.setLocalStream(stream);
  });

  const rebuildOutboundStream = useEffectEvent(async () => {
    const screenShareStream = screenShareStreamRef.current;
    if (screenShareStream) {
      const outbound = new MediaStream();
      for (const track of screenShareStream.getVideoTracks()) {
        outbound.addTrack(track);
      }
      for (const track of screenShareStream.getAudioTracks()) {
        outbound.addTrack(track);
      }
      for (const track of baseMediaStreamRef.current?.getAudioTracks() ?? []) {
        const duplicate = outbound.getAudioTracks().some((audioTrack) => audioTrack.id === track.id);
        if (!duplicate) {
          outbound.addTrack(track);
        }
      }
      await applyOutboundStream(outbound);
      return;
    }

    await applyOutboundStream(baseMediaStreamRef.current);
  });

  const connectSignalForSession = useEffectEvent((session: SessionState) => {
    signalClientRef.current?.close();

    const client = new SignalClient();
    signalClientRef.current = client;
    ensurePeerMesh(session);
    client.connect(session.meeting.id, session.participant.id, {
      onOpen: () => {
        if (signalClientRef.current !== client) {
          return;
        }
        logger.info("signal.connected", {
          meetingId: session.meeting.id,
          participantId: session.participant.id
        });
        setWsConnected(true);
        setStatusMessage("WSS 信令已连接");
      },
      onClose: () => {
        if (signalClientRef.current !== client) {
          return;
        }
        logger.warn("signal.closed", {
          meetingId: session.meeting.id,
          participantId: session.participant.id,
          endingMeetingFlow: endingMeetingRef.current,
          summaryPrepared: meetingEndSummaryPreparedRef.current
        });
        signalClientRef.current = null;
        setWsConnected(false);
        if (
          endingMeetingRef.current &&
          session.participant.role === "host" &&
          !meetingEndSummaryPreparedRef.current
        ) {
          endingMeetingRef.current = false;
          preparePostEndSummary();
          return;
        }
        if (sessionRef.current) {
          setStatusMessage("WSS 信令已断开");
        }
        disposeRtc(false);
      },
      onError: (message) => {
        if (signalClientRef.current !== client) {
          return;
        }
        logger.error("signal.error", {
          meetingId: session.meeting.id,
          participantId: session.participant.id,
          error: message
        });
        setErrorMessage(message);
      },
      onMessage: (event) => {
        if (signalClientRef.current !== client) {
          return;
        }
        onSignalMessage(event);
      }
    });
  });

  useEffect(() => {
    if (!meetingSession || !pendingSignalSession) {
      return;
    }

    if (
      pendingSignalSession.meeting.id !== meetingSession.meeting.id ||
      pendingSignalSession.participant.id !== meetingSession.participant.id
    ) {
      return;
    }

    connectSignalForSession(meetingSession);
    setPendingSignalSession(null);
  }, [meetingSession, pendingSignalSession]);

  const enterMeetingSession = useEffectEvent((meeting: Meeting, participant: Participant, nextStatus: string) => {
    const nextSession = { meeting, participant };
    sessionRef.current = nextSession;
    setMeetingSession(nextSession);
    setPendingSignalSession(nextSession);
    setChatMessages(meeting.chatMessages ?? []);
    setWhiteboardActions(meeting.whiteboardActions ?? []);
    setActiveReadyCheck(meeting.activeReadyCheck ?? null);
    setTemporaryMinutes(meeting.temporaryMinutes ?? []);
    setAuditSummary(null);
    setEvents([]);
    setCurrentSidebar("none");
    setCurrentModal("none");
    setFeaturedStageId(null);
    setJoinLookupMeeting(null);
    setShowJoinPasswordModal(false);
    setStatusMessage(nextStatus);
    setErrorMessage("");
    scrollViewportToTop();
  });

  const syncBaseMediaPreference = useEffectEvent(async (nextPreference: { camera: boolean; microphone: boolean }) => {
    if (!meetingSession) {
      setErrorMessage("请先进入会议");
      return;
    }

    if (nextPreference.camera && !hasCapability(meetingSession, "camera")) {
      setErrorMessage("当前账号还没有摄像头权限");
      return;
    }

    if (nextPreference.microphone && !hasCapability(meetingSession, "microphone")) {
      setErrorMessage("当前账号还没有麦克风权限");
      return;
    }

    if ((nextPreference.camera || nextPreference.microphone) && !supportsUserMediaCapture()) {
      setErrorMessage(describeUserMediaError(undefined));
      return;
    }

    try {
      if (!nextPreference.camera && !nextPreference.microphone) {
        baseMediaStreamRef.current?.getTracks().forEach((track) => track.stop());
        baseMediaStreamRef.current = null;
        setCaptureSelection(nextPreference);
        await rebuildOutboundStream();
        setStatusMessage("本地媒体已关闭");
        appendEvent("rtc.local_media_stopped", "本地摄像头和麦克风均已关闭");
        return;
      }

      const mediaDevices = getMediaDevices();
      if (!mediaDevices || typeof mediaDevices.getUserMedia !== "function") {
        setErrorMessage(describeUserMediaError(undefined));
        return;
      }

      const stream = await mediaDevices.getUserMedia({
        video: nextPreference.camera,
        audio: nextPreference.microphone
      });

      baseMediaStreamRef.current?.getTracks().forEach((track) => track.stop());
      baseMediaStreamRef.current = stream;
      setCaptureSelection(nextPreference);
      await rebuildOutboundStream();
      setStatusMessage("本地媒体已更新");
      appendEvent(
        "rtc.local_media_started",
        `本地媒体已更新: ${describeCaptureSelection(nextPreference)}`
      );
    } catch (error) {
      setErrorMessage(describeUserMediaError(error));
    }
  });

  const handleLoginSubmit = useEffectEvent((event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const email = loginForm.email.trim();
    const password = loginForm.password.trim();
    if (!email) {
      setErrorMessage("请输入邮箱");
      return;
    }

    if (!password) {
      setErrorMessage("请输入密码");
      return;
    }

    const hostIdentity = buildHostIdentity(email);
    setIsAuthenticated(true);
    setEntryView("home");
    setJoinForm((current) => ({
      ...current,
      nickname: hostIdentity.nickname
    }));
    logger.info("auth.login_succeeded", {
      email,
      nickname: hostIdentity.nickname
    });
    setStatusMessage(`欢迎回来，${hostIdentity.nickname}`);
    setErrorMessage("");
  });

  const handleRequestTemporaryCode = useEffectEvent(() => {
    if (!loginForm.email.trim()) {
      setErrorMessage("请先输入邮箱后再获取临时验证码");
      return;
    }

    setStatusMessage("当前版本尚未接入真实验证码服务，已保留交互入口");
    setErrorMessage("");
  });

  const handlePasswordLoginModeHint = useEffectEvent(() => {
    setStatusMessage("当前仍为测试密码登录，验证码入口已保留");
    setErrorMessage("");
  });

  const handleWechatLoginHint = useEffectEvent(() => {
    setStatusMessage("当前版本尚未接入微信登录，已保留交互入口");
    setErrorMessage("");
  });

  const handleForgotPasswordHint = useEffectEvent(() => {
    setStatusMessage("忘记密码入口已保留，后续可接真实身份体系");
    setErrorMessage("");
  });

  const handleScheduleSubmit = useEffectEvent(async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const hostIdentity = buildHostIdentity(loginForm.email);
    const title = scheduleForm.title.trim();
    if (!title) {
      setErrorMessage("请输入会议主题");
      return;
    }

    if (!isAuthenticated) {
      setErrorMessage("请先登录后再创建会议");
      return;
    }

    const password = scheduleForm.password.trim();

    try {
      logger.info("meeting.schedule_create_requested", {
        title,
        passwordRequired: password !== "",
        hostUserId: hostIdentity.userId
      });
      const response = await createMeeting({
        title,
        password,
        hostUserId: hostIdentity.userId,
        hostNickname: hostIdentity.nickname,
        deviceType
      });
      setReturnAfterMeetingView("schedule");
      setMeetingAccessPassword(password);
      setJoinForm((current) => ({
        ...current,
        meetingId: getMeetingPublicNumber(response.meeting),
        password
      }));
      enterMeetingSession(response.meeting, response.host, "会议已创建，正在接入信令");
      appendEvent("meeting.created", `会议 ${response.meeting.title} 已创建`);
      logger.info("meeting.schedule_create_succeeded", {
        meetingId: response.meeting.id,
        passwordRequired: response.meeting.passwordRequired
      });
    } catch (error) {
      logger.error("meeting.schedule_create_failed", {
        title,
        error
      });
      setErrorMessage(asMessage(error));
    }
  });

  const handleStartQuickMeeting = useEffectEvent(async () => {
    if (!isAuthenticated) {
      setErrorMessage("请先登录后再发起快速会议");
      return;
    }

    const hostIdentity = buildHostIdentity(loginForm.email);
    const title = `${hostIdentity.nickname} 的快速会议`;
    const password = "";

    try {
      logger.info("meeting.quick_create_requested", {
        title,
        hostUserId: hostIdentity.userId
      });
      const response = await createMeeting({
        title,
        password,
        hostUserId: hostIdentity.userId,
        hostNickname: hostIdentity.nickname,
        deviceType
      });
      setReturnAfterMeetingView("home");
      setMeetingAccessPassword(password);
      setJoinForm((current) => ({
        ...current,
        meetingId: getMeetingPublicNumber(response.meeting),
        password
      }));
      enterMeetingSession(response.meeting, response.host, "快速会议已创建，正在接入信令");
      appendEvent("meeting.created", `快速会议 ${response.meeting.title} 已创建`);
      logger.info("meeting.quick_create_succeeded", {
        meetingId: response.meeting.id,
        passwordRequired: response.meeting.passwordRequired
      });
    } catch (error) {
      logger.error("meeting.quick_create_failed", {
        title,
        error
      });
      setErrorMessage(asMessage(error));
    }
  });

  const performJoinMeeting = useEffectEvent(async (meeting: Meeting, password: string) => {
    logger.info("meeting.join_requested", {
      meetingId: meeting.id,
      passwordProvided: password !== "",
      nickname: joinForm.nickname.trim()
    });
    const response = await joinMeeting({
      meetingId: meeting.id,
      password,
      userId: isAuthenticated ? buildHostIdentity(loginForm.email).userId : "",
      nickname: joinForm.nickname.trim(),
      deviceType,
      isAnonymous: !isAuthenticated,
      requestCameraEnabled: joinForm.requestCameraEnabled,
      requestMicrophoneEnabled: joinForm.requestMicrophoneEnabled
    });
    setMeetingAccessPassword(password);
    enterMeetingSession(response.meeting, response.participant, "已加入会议，正在接入信令");
    appendEvent("meeting.joined", `${response.participant.nickname} 已加入会议`);
    logger.info("meeting.join_succeeded", {
      meetingId: response.meeting.id,
      participantId: response.participant.id,
      participantRole: response.participant.role
    });
  });

  const handleLookupMeeting = useEffectEvent(async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const meetingId = normalizeMeetingLookupValue(joinForm.meetingId);
    if (!meetingId) {
      setErrorMessage("请输入会议号");
      return;
    }

    if (!joinForm.nickname.trim()) {
      setErrorMessage("请输入昵称");
      return;
    }

    try {
      logger.info("meeting.lookup_requested", {
        meetingId,
        nickname: joinForm.nickname.trim()
      });
      const response = await getMeeting({ meetingId });
      setJoinLookupMeeting(response.meeting);
      if (response.meeting.passwordRequired) {
        logger.info("meeting.lookup_requires_password", {
          meetingId: response.meeting.id
        });
        setShowJoinPasswordModal(true);
        setStatusMessage("会议号已验证，请继续输入会议密码");
        setErrorMessage("");
        return;
      }

      setJoinForm((current) => ({
        ...current,
        meetingId,
        password: ""
      }));
      setShowJoinPasswordModal(false);
      setStatusMessage("会议号已验证，该会议无需密码，正在加入");
      setErrorMessage("");
      await performJoinMeeting(response.meeting, "");
    } catch (error) {
      logger.error("meeting.lookup_failed", {
        meetingId,
        error
      });
      setJoinLookupMeeting(null);
      setShowJoinPasswordModal(false);
      setErrorMessage(asMessage(error));
    }
  });

  const handleConfirmJoinMeeting = useEffectEvent(async () => {
    if (!joinLookupMeeting) {
      setErrorMessage("请先验证会议号");
      return;
    }

    if (joinLookupMeeting.passwordRequired && !joinForm.password.trim()) {
      setErrorMessage("请输入会议密码");
      return;
    }

    try {
      await performJoinMeeting(joinLookupMeeting, joinForm.password.trim());
    } catch (error) {
      setErrorMessage(asMessage(error));
    }
  });

  const handleScannedJoinQRCode = useEffectEvent((payloadText: string) => {
    const payload = parseMeetingQRCodePayload(payloadText);
    if (!payload.meetingId) {
      logger.warn("join.scan_decoded_invalid_payload", {
        payloadText
      });
      setJoinScanError("未识别到有效的会议二维码");
      return;
    }

    logger.info("join.scan_decoded", {
      meetingId: payload.meetingId,
      passwordProvided: payload.password !== ""
    });
    stopJoinScanner();
    setShowJoinScanModal(false);
    setJoinLookupMeeting(null);
    setShowJoinPasswordModal(false);
    setJoinScanError("");
    setJoinScanStatus("二维码已识别");
    setJoinForm((current) => ({
      ...current,
      meetingId: normalizeMeetingLookupValue(payload.meetingId),
      password: payload.password ?? current.password
    }));
    setStatusMessage(
      payload.password ? "已扫码识别会议，会议号和密码已自动填入" : "已扫码识别会议，会议号已自动填入"
    );
  });

  const handlePickJoinQRCodeImage = useEffectEvent(() => {
    logger.info("join.scan_pick_image_requested");
    joinScannerFileInputRef.current?.click();
  });

  const handleJoinQRCodeFileSelected = useEffectEvent(
    async (event: React.ChangeEvent<HTMLInputElement>) => {
      const file = event.target.files?.[0];
      event.target.value = "";
      if (!file) {
        return;
      }

      try {
        setJoinScanError("");
        setJoinScanStatus("正在识别二维码图片...");
        logger.info("join.scan_image_selected", {
          fileName: file.name,
          fileSize: file.size
        });
        const payload = await decodeQRCodeFromFile(file);
        handleScannedJoinQRCode(payload);
      } catch (error) {
        logger.error("join.scan_image_decode_failed", {
          error
        });
        setJoinScanError(asMessage(error));
        setJoinScanStatus("图片识别失败");
      }
    }
  );

  useEffect(() => {
    if (!showJoinScanModal) {
      stopJoinScanner();
      return;
    }

    let cancelled = false;
    const startScanner = async () => {
      setJoinScanError("");
      if (!supportsLiveJoinScanner()) {
        logger.warn("join.scan_live_unsupported", {
          secureContext: typeof window !== "undefined" ? window.isSecureContext : false,
          hasMediaDevices:
            typeof navigator !== "undefined" ? Boolean(navigator.mediaDevices?.getUserMedia) : false
        });
        setJoinScanStatus("当前环境不支持直接摄像头扫码");
        setJoinScanError("当前页面无法直接调用摄像头。局域网 HTTP 页面在移动端通常需要 HTTPS，或使用下方拍照/选图识别。");
        return;
      }
      setJoinScanStatus("请将二维码对准取景框");

      try {
        logger.info("join.scan_live_requested");
        const stream = await navigator.mediaDevices.getUserMedia({
          video: {
            facingMode: {
              ideal: "environment"
            }
          },
          audio: false
        });
        if (cancelled) {
          stream.getTracks().forEach((track) => track.stop());
          return;
        }

        joinScannerStreamRef.current = stream;
        const videoElement = joinScannerVideoRef.current;
        if (!videoElement) {
          throw new Error("扫码视频预览初始化失败");
        }

        videoElement.srcObject = stream;
        await videoElement.play();

        const scanFrame = () => {
          if (cancelled) {
            return;
          }

          const video = joinScannerVideoRef.current;
          const canvas = joinScannerCanvasRef.current;
          if (!video || !canvas || video.readyState < HTMLMediaElement.HAVE_CURRENT_DATA) {
            joinScannerFrameRef.current = window.requestAnimationFrame(scanFrame);
            return;
          }

          const context = canvas.getContext("2d");
          if (!context) {
            setJoinScanError("扫码画布初始化失败");
            return;
          }

          canvas.width = video.videoWidth;
          canvas.height = video.videoHeight;
          context.drawImage(video, 0, 0, canvas.width, canvas.height);
          const imageData = context.getImageData(0, 0, canvas.width, canvas.height);
          const result = jsQR(imageData.data, imageData.width, imageData.height);
          if (result?.data) {
            handleScannedJoinQRCode(result.data);
            return;
          }

          joinScannerFrameRef.current = window.requestAnimationFrame(scanFrame);
        };

        joinScannerFrameRef.current = window.requestAnimationFrame(scanFrame);
      } catch (error) {
        if (cancelled) {
          return;
        }
        logger.error("join.scan_live_failed", {
          error
        });
        setJoinScanError(describeJoinScannerError(error));
        setJoinScanStatus("无法启动扫码");
      }
    };

    void startScanner();
    return () => {
      cancelled = true;
      stopJoinScanner();
    };
  }, [showJoinScanModal]);

  function handleConnectSignal() {
    if (!meetingSession) {
      return;
    }

    connectSignalForSession(meetingSession);
  }

  function handleDisconnectSignal() {
    sessionRef.current = meetingSession;
    signalClientRef.current?.close();
    signalClientRef.current = null;
    setWsConnected(false);
    setStatusMessage("WSS 信令已手动断开");
    disposeRtc(false);
  }

  function handleToggleCamera() {
    const currentPreference = resolveBaseCapturePreference(baseMediaStreamRef.current);
    void syncBaseMediaPreference({
      camera: !currentPreference.camera,
      microphone: currentPreference.microphone
    });
  }

  function handleToggleMicrophone() {
    const currentPreference = resolveBaseCapturePreference(baseMediaStreamRef.current);
    void syncBaseMediaPreference({
      camera: currentPreference.camera,
      microphone: !currentPreference.microphone
    });
  }

  async function handleStartScreenShare() {
    if (!meetingSession) {
      setErrorMessage("请先进入会议");
      return;
    }

    if (!hasCapability(meetingSession, "screen_share")) {
      setErrorMessage("当前账号还没有屏幕共享权限");
      return;
    }

    if (!supportsDisplayCapture()) {
      setErrorMessage(describeDisplayCaptureError(undefined));
      return;
    }

    try {
      const mediaDevices = getMediaDevices();
      if (!mediaDevices || typeof mediaDevices.getDisplayMedia !== "function") {
        setErrorMessage(describeDisplayCaptureError(undefined));
        return;
      }

      const displayStream = await mediaDevices.getDisplayMedia({
        video: {
          frameRate: 24
        },
        audio: false
      });

      displayStream.getVideoTracks()[0]?.addEventListener("ended", () => {
        void handleStopScreenShare();
      });

      screenShareStreamRef.current?.getTracks().forEach((track) => track.stop());
      screenShareStreamRef.current = displayStream;
      setScreenSharing(true);
      await rebuildOutboundStream();
      appendEvent("rtc.screen_share_started", "屏幕共享已启动");
      setStatusMessage("屏幕共享中");
    } catch (error) {
      setErrorMessage(describeDisplayCaptureError(error));
    }
  }

  async function handleStopScreenShare() {
    screenShareStreamRef.current?.getTracks().forEach((track) => track.stop());
    screenShareStreamRef.current = null;
    setScreenSharing(false);

    try {
      await rebuildOutboundStream();
      appendEvent("rtc.screen_share_stopped", "屏幕共享已停止");
      setStatusMessage("已切回常规媒体流");
    } catch (error) {
      setErrorMessage(asMessage(error));
    }
  }

  async function handleStartRecording() {
    if (!meetingSession) {
      setErrorMessage("请先进入会议");
      return;
    }

    if (!hasCapability(meetingSession, "record")) {
      setCurrentModal("record_request");
      return;
    }

    if (recordingActive) {
      setErrorMessage("当前已有录制任务在进行中");
      return;
    }

    discardRecording(recordingAsset);
    setRecordingAsset(null);

    try {
      const recording = await startLocalRecording({
        title: meetingSession.meeting.title,
        kind: recordingKind,
        localStream: localStreamRef.current,
        remoteStreams: remoteTiles.map((tile) => tile.stream)
      });
      recorderRef.current = recording;
      recording.start();
      setRecordingActive(true);
      appendEvent("recording.started", `本地录制已开始: ${recordingKind}`);
      setStatusMessage("本地录制中");
    } catch (error) {
      setErrorMessage(asMessage(error));
    }
  }

  async function handleStopRecording() {
    if (!recorderRef.current) {
      return;
    }

    try {
      const asset = await recorderRef.current.stop();
      recorderRef.current = null;
      setRecordingActive(false);
      setRecordingAsset(asset);
      appendEvent("recording.stopped", `本地录制已停止，缓存大小 ${formatBytes(asset.sizeBytes)}`);
      setStatusMessage("录制缓存已生成");
    } catch (error) {
      setRecordingActive(false);
      recorderRef.current = null;
      setErrorMessage(asMessage(error));
    }
  }

  async function handleDownloadRecording() {
    if (!recordingAsset) {
      return;
    }

    try {
      await downloadRecording(recordingAsset);
      appendEvent("recording.downloaded", `录制文件已保存: ${recordingAsset.fileName}`);
    } catch (error) {
      setErrorMessage(asMessage(error));
    }
  }

  function handleDiscardRecording() {
    discardRecording(recordingAsset);
    setRecordingAsset(null);
    appendEvent("recording.discarded", "已丢弃本地录制缓存");
  }

  function handleSendChat(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!chatInput.trim()) {
      return;
    }

    try {
      signalClientRef.current?.send("chat.message", {
        message: chatInput.trim()
      });
      setChatInput("");
    } catch (error) {
      setErrorMessage(asMessage(error));
    }
  }

  function requestCapability(capability: Capability) {
    try {
      signalClientRef.current?.send("capability.request", {
        capability
      });
      appendEvent("capability.request", `已请求 ${capability} 权限`);
      setStatusMessage(`已向主持人申请 ${capability} 权限`);
    } catch (error) {
      setErrorMessage(asMessage(error));
    }
  }

  function handleRequestCapability() {
    requestCapability(capabilityToRequest);
  }

  function handleRequestRecordPermission() {
    requestCapability("record");
    setCurrentModal("none");
  }

  function handleGrantCapability() {
    if (!grantTargetId.trim()) {
      setErrorMessage("请先填写目标 participantId");
      return;
    }

    try {
      signalClientRef.current?.send("capability.grant", {
        targetParticipantId: grantTargetId.trim(),
        capability: grantCapability
      });
    } catch (error) {
      setErrorMessage(asMessage(error));
    }
  }

  function handleAssignAssistant() {
    if (!assistantTargetId.trim()) {
      setErrorMessage("请先填写助理 participantId");
      return;
    }

    try {
      signalClientRef.current?.send("role.assign_assistant", {
        targetParticipantId: assistantTargetId.trim()
      });
    } catch (error) {
      setErrorMessage(asMessage(error));
    }
  }

  function handleSubmitWhiteboardAction(action: Omit<WhiteboardAction, "id" | "createdBy" | "createdAt">) {
    try {
      signalClientRef.current?.send("whiteboard.draw", { action });
    } catch (error) {
      setErrorMessage(asMessage(error));
    }
  }

  function handleClearWhiteboard() {
    try {
      signalClientRef.current?.send("whiteboard.clear");
    } catch (error) {
      setErrorMessage(asMessage(error));
    }
  }

  function handleStartReadyCheck() {
    try {
      signalClientRef.current?.send("ready_check.start", {
        timeoutSeconds: readyCheckTimeoutSeconds
      });
    } catch (error) {
      setErrorMessage(asMessage(error));
    }
  }

  function handleRespondReadyCheck(status: ReadyCheckStatus) {
    try {
      signalClientRef.current?.send("ready_check.respond", { status });
    } catch (error) {
      setErrorMessage(asMessage(error));
    }
  }

  async function handleUpdateNickname() {
    if (!meetingSession) {
      return;
    }

    const trimmedNickname = nicknameDraft.trim();
    if (!trimmedNickname) {
      setErrorMessage("昵称不能为空");
      return;
    }

    try {
      const response = await updateNickname({
        meetingId: meetingSession.meeting.id,
        participantId: meetingSession.participant.id,
        nickname: trimmedNickname
      });
      if (!wsConnected) {
        applyNicknameUpdate(response);
      }
    } catch (error) {
      setErrorMessage(asMessage(error));
    }
  }

  async function handleLeaveMeeting() {
    if (!meetingSession) {
      return;
    }

    try {
      await leaveMeeting({
        meetingId: meetingSession.meeting.id,
        participantId: meetingSession.participant.id,
        deviceType
      });
      exitMeetingShell("你已离开会议");
    } catch (error) {
      setErrorMessage(asMessage(error));
    }
  }

  async function handleConfirmEndMeeting() {
    if (!meetingSession) {
      return;
    }

    try {
      endingMeetingRef.current = true;
      meetingEndSummaryPreparedRef.current = false;
      setEndingMeetingPending(true);
      setStatusMessage("正在结束会议...");
      logger.info("meeting.end_requested", {
        meetingId: meetingSession.meeting.id,
        participantId: meetingSession.participant.id
      });
      await endMeeting({
        meetingId: meetingSession.meeting.id,
        hostParticipantId: meetingSession.participant.id,
        deviceType
      });
      logger.info("meeting.end_request_succeeded", {
        meetingId: meetingSession.meeting.id,
        participantId: meetingSession.participant.id
      });
      preparePostEndSummary();
    } catch (error) {
      endingMeetingRef.current = false;
      setEndingMeetingPending(false);
      logger.error("meeting.end_request_failed", {
        meetingId: meetingSession.meeting.id,
        participantId: meetingSession.participant.id,
        error
      });
      setErrorMessage(asMessage(error));
    }
  }

  async function handleCopyInvite() {
    if (!meetingSession) {
      return;
    }

    const text = buildInviteText(
      meetingSession.meeting,
      meetingAccessPassword,
      meetingSession.participant.nickname
    );
    try {
      await copyText(text);
      setStatusMessage("已复制全部会议信息");
    } catch (error) {
      setErrorMessage(asMessage(error));
    }
  }

  async function handleCopyMeetingID() {
    if (!meetingSession) {
      return;
    }

    try {
      await copyText(formatMeetingNumberDisplay(getMeetingPublicNumber(meetingSession.meeting)));
      setStatusMessage("已复制会议号");
    } catch (error) {
      setErrorMessage(asMessage(error));
    }
  }

  async function handleCopyInviteQRCode() {
    if (!shareQrDataUrl) {
      setErrorMessage("会议二维码尚未生成完成，请稍后重试");
      return;
    }

    try {
      await copyImageDataURL(shareQrDataUrl);
      setStatusMessage("已复制会议二维码");
    } catch (error) {
      setErrorMessage(asMessage(error));
    }
  }

  async function handleCopyClientLogs() {
    const content = formatClientLogs();
    if (!content) {
      setStatusMessage("当前还没有可复制的前端日志");
      return;
    }

    try {
      await copyText(content);
      logger.info("logs.copied", {
        lineCount: content.split("\n").length
      });
      setStatusMessage("前端调试日志已复制");
    } catch (error) {
      logger.error("logs.copy_failed", {
        error
      });
      setErrorMessage(asMessage(error));
    }
  }

  function handleExportMinutes() {
    if (!meetingSession) {
      return;
    }

    const lines = [
      `会议标题: ${meetingSession.meeting.title}`,
      `会议号: ${formatMeetingNumberDisplay(getMeetingPublicNumber(meetingSession.meeting))}`,
      `当前导出人: ${meetingSession.participant.nickname}`,
      `导出时间: ${new Date().toLocaleString("zh-CN", { hour12: false })}`,
      "",
      "[临时纪要]",
      ...(temporaryMinutes.length > 0 ? temporaryMinutes : ["暂无纪要"]),
      "",
      "[聊天记录]",
      ...(chatMessages.length > 0
        ? chatMessages.map(
            (message) =>
              `[${new Date(message.sentAt).toLocaleTimeString("zh-CN", { hour12: false })}] ${message.nickname}: ${message.message}`
          )
        : ["暂无聊天记录"]),
      "",
      "[白板笔迹数量]",
      String(whiteboardActions.length),
      "",
      "[就位确认状态]",
      activeReadyCheck
        ? `${activeReadyCheck.status} / ${summarizeReadyCheckRound(activeReadyCheck)}`
        : "暂无就位确认"
    ];

    const blob = new Blob([lines.join("\n")], { type: "text/plain;charset=utf-8" });
    const objectUrl = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = objectUrl;
    anchor.download = buildMinutesFileName(meetingSession.meeting.title);
    anchor.click();
    URL.revokeObjectURL(objectUrl);
    appendEvent("meeting.minutes_exported", "临时纪要已导出到本地文件");
    setStatusMessage("当前会议纪要已导出");
  }

  function handleReturnAfterMeeting() {
    exitMeetingShell(
      "会议已结束",
      returnAfterMeetingView === "schedule" ? "schedule" : isAuthenticated ? "home" : "login"
    );
  }

  function closeAttachedWindows() {
    setCurrentMenu("none");
    setCurrentAttachedPanel("none");
  }

  function toggleMenuWindow(nextMenu: MenuView) {
    if (nextMenu === "none") {
      setCurrentMenu("none");
      return;
    }

    setCurrentAttachedPanel("none");
    setCurrentMenu((current) => (current === nextMenu ? "none" : nextMenu));
  }

  function toggleAttachedWindow(nextPanel: AttachedPanelView) {
    if (nextPanel === "none") {
      setCurrentAttachedPanel("none");
      return;
    }

    setCurrentMenu("none");
    setCurrentAttachedPanel((current) => (current === nextPanel ? "none" : nextPanel));
  }

  function openMeetingModal(nextModal: ModalView) {
    closeAttachedWindows();
    setCurrentModal(nextModal);
  }

  function toggleSidebarDrawer(nextSidebar: SidebarView) {
    closeAttachedWindows();
    setCurrentSidebar((current) => (current === nextSidebar ? "none" : nextSidebar));
  }

  async function handleToggleFullscreenMode() {
    if (typeof document === "undefined") {
      return;
    }

    try {
      if (document.fullscreenElement) {
        await document.exitFullscreen();
      } else {
        await document.documentElement.requestFullscreen();
      }
    } catch (error) {
      setErrorMessage(asMessage(error));
    }
  }

  const isHost = meetingSession?.participant.role === "host";
  const canAccessHostTools =
    meetingSession?.participant.role === "host" || meetingSession?.participant.role === "assistant";
  const participants = sortParticipantsByJoinOrder(Object.values(meetingSession?.meeting.participants ?? {}));
  const stageParticipants = participants.filter(
    (participant) =>
      participant.id === meetingSession?.participant.id || onlineParticipantIds.includes(participant.id)
  );
  const canUseCamera = meetingSession ? hasCapability(meetingSession, "camera") : false;
  const canUseMicrophone = meetingSession ? hasCapability(meetingSession, "microphone") : false;
  const canScreenShare = meetingSession ? hasCapability(meetingSession, "screen_share") : false;
  const canRecord = meetingSession ? hasCapability(meetingSession, "record") : false;
  const canWhiteboard = meetingSession ? hasCapability(meetingSession, "whiteboard") : false;
  const canReadyCheck = meetingSession ? hasCapability(meetingSession, "ready_check") : false;
  const localReadyCheckStatus = activeReadyCheck && meetingSession
    ? activeReadyCheck.results[meetingSession.participant.id]?.status
    : undefined;

  const basePreference = resolveBaseCapturePreference(baseMediaStreamRef.current);
  const localMicEnabled = Boolean(
    baseMediaStreamRef.current?.getAudioTracks().some((track) => track.enabled && track.readyState === "live")
  );

  const stageItems = buildStageItems({
    session: meetingSession,
    localStream,
    remoteTiles,
    screenSharing,
    localMicEnabled
  });

  useEffect(() => {
    const nextFeatured = chooseFeaturedStageId(
      stageItems,
      featuredStageId,
      meetingSession?.meeting.hostParticipantId
    );
    if (nextFeatured !== featuredStageId) {
      setFeaturedStageId(nextFeatured);
    }
  }, [featuredStageId, meetingSession?.meeting.hostParticipantId, stageItems]);

  const featuredStageItem =
    stageItems.find((item) => item.id === featuredStageId) ?? stageItems[0] ?? null;
  const thumbnailItems = featuredStageItem
    ? stageItems.filter((item) => item.id !== featuredStageItem.id)
    : stageItems;
  const roomClockLabel = meetingSession ? formatElapsedClock(meetingSession.meeting.createdAt) : "00:00";
  const connectionLabel = wsConnected ? "WSS 已连接" : "WSS 已断开";
  const networkStatusLabel = errorMessage ? "异常" : wsConnected ? "正常" : "断开";
  const networkSummaryLabel = `${networkStatusLabel} · ${connectionLabel}`;
  const networkLatencyLabel = auditSummary ? `${auditSummary.latencyMs.toFixed(0)} ms` : "--";
  const networkPacketLossLabel = auditSummary ? `${(auditSummary.packetLossRate * 100).toFixed(2)}%` : "--";
  const networkThroughputLabel = auditSummary
    ? `${(auditSummary.averageBitrateKbps / 8000).toFixed(2)} MB/s`
    : "--";
  const inviteMeetingNumberLabel = meetingSession
    ? formatMeetingNumberDisplay(getMeetingPublicNumber(meetingSession.meeting))
    : "";
  const inviteJoinURL = meetingSession ? buildMeetingJoinURL(meetingSession.meeting).toString() : "";
  const inviteMeetingTimeLabel = meetingSession ? formatInviteMeetingTime(meetingSession.meeting.createdAt) : "";
  const isLoginEntry = entryView === "login";
  const showLoginFeedback = isLoginEntry && (errorMessage || statusMessage !== defaultEntryStatusMessage);
  const showEntryFeedback =
    !isLoginEntry && entryView !== "home" && (errorMessage || statusMessage !== defaultEntryStatusMessage);

  if (!meetingSession) {
    return (
      <main className="page-shell auth-page auth-page-login">
        <section className="auth-frame" data-auth-view={entryView}>
          <section className="brand-stage">
            <div className="brand-copy">
              <h1 className="wordmark">
                <span>meeting</span>
              </h1>
            </div>
          </section>

          <aside className="auth-panel">
            {entryView === "login" ? (
              <section className="panel auth-card" data-view="login">
                <form className="form-grid login-form" onSubmit={handleLoginSubmit}>
                  <div className="login-header-copy">
                    <div className="login-mode-title">邮箱验证码登录</div>
                    <button className="login-switch-link" onClick={handlePasswordLoginModeHint} type="button">
                      使用密码登录 &gt;
                    </button>
                  </div>
                  <input
                    aria-label="邮箱"
                    onChange={(event) =>
                      setLoginForm((current) => ({ ...current, email: event.target.value }))
                    }
                    placeholder="请输入邮箱"
                    type="email"
                    value={loginForm.email}
                  />
                  <div className="field-shell">
                    <input
                      aria-label="密码"
                      onChange={(event) =>
                        setLoginForm((current) => ({ ...current, password: event.target.value }))
                      }
                      placeholder="请输入密码"
                      type="password"
                      value={loginForm.password}
                    />
                    <button className="field-inline-action" onClick={handleRequestTemporaryCode} type="button">
                      获取临时验证码
                    </button>
                  </div>
                  <div className="login-footer">
                    <button onClick={handleForgotPasswordHint} type="button">
                      忘记密码
                    </button>
                  </div>
                  {showLoginFeedback ? (
                    <div className="status-stack compact">
                      {statusMessage !== defaultEntryStatusMessage ? (
                        <div className="status-pill">{statusMessage}</div>
                      ) : null}
                      {errorMessage ? <div className="status-error">{errorMessage}</div> : null}
                    </div>
                  ) : null}
                  <div className="button-stack login-actions">
                    <button className="primary-button" type="submit">
                      登录
                    </button>
                    <button className="secondary-button wechat-button" onClick={handleWechatLoginHint} type="button">
                      <svg className="wechat-icon" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                        <circle cx="9" cy="10" r="6.5" fill="#09b83e" />
                        <circle cx="15.2" cy="13.6" r="5.4" fill="#09b83e" />
                        <circle cx="7.3" cy="9.7" r="0.9" fill="#ffffff" />
                        <circle cx="10.4" cy="9.7" r="0.9" fill="#ffffff" />
                        <circle cx="13.8" cy="13.2" r="0.75" fill="#ffffff" />
                        <circle cx="16.6" cy="13.2" r="0.75" fill="#ffffff" />
                        <path
                          d="M8.3 14.2 7.1 15.4v-1.7"
                          fill="none"
                          stroke="#ffffff"
                          strokeLinecap="round"
                          strokeLinejoin="round"
                          strokeWidth="1"
                        />
                      </svg>
                    </button>
                  </div>
                  <div className="auth-divider" aria-hidden="true" />
                  <button className="secondary-button join-button" onClick={() => setEntryView("join")} type="button">
                    加入会议
                  </button>
                </form>
              </section>
            ) : null}

            {entryView === "home" ? (
              <section className="panel auth-card" data-view="home">
                <div className="login-header-copy">
                  <div className="login-mode-title">预定会议 / 快速会议</div>
                  <button className="login-switch-link" onClick={() => setEntryView("login")} type="button">
                    返回登录 &gt;
                  </button>
                </div>
                <div className="quick-list">
                  <article className="quick-card">
                    <strong>预定会议</strong>
                    <span>填写主题、时间、时区和可选密码，适合提前安排的正式会议。</span>
                    <button className="secondary-button" onClick={() => setEntryView("schedule")} type="button">
                      进入预定流程
                    </button>
                  </article>
                  <article className="quick-card">
                    <strong>快速会议</strong>
                    <span>立即创建并开始会议，适合站会、短会和临时沟通。</span>
                    <button className="primary-button" onClick={() => void handleStartQuickMeeting()} type="button">
                      立即开始
                    </button>
                  </article>
                </div>
                <button className="ghost-button" onClick={() => setEntryView("join")} type="button">
                  已有会议号？加入会议
                </button>
              </section>
            ) : null}

            {entryView === "schedule" ? (
              <section className="panel auth-card" data-view="schedule">
                <div>
                  <p className="eyebrow">Schedule meeting</p>
                  <h2>预定会议</h2>
                </div>
                <p className="section-copy">
                  当前后端还没有真正的预约会议模型，本轮会按预定表单创建可立即进入的会议，同时保留未来扩展所需字段。
                </p>
                {showEntryFeedback ? (
                  <div className="status-stack compact">
                    {statusMessage !== defaultEntryStatusMessage ? (
                      <div className="status-pill">{statusMessage}</div>
                    ) : null}
                    {errorMessage ? <div className="status-error">{errorMessage}</div> : null}
                  </div>
                ) : null}
                <form className="form-grid" onSubmit={handleScheduleSubmit}>
                  <label>
                    会议主题
                    <input
                      onChange={(event) =>
                        setScheduleForm((current) => ({ ...current, title: event.target.value }))
                      }
                      value={scheduleForm.title}
                    />
                  </label>
                  <label>
                    会议时间
                    <input
                      onChange={(event) =>
                        setScheduleForm((current) => ({ ...current, scheduledAt: event.target.value }))
                      }
                      type="datetime-local"
                      value={scheduleForm.scheduledAt}
                    />
                  </label>
                  <label>
                    时区
                    <select
                      onChange={(event) =>
                        setScheduleForm((current) => ({ ...current, timezone: event.target.value }))
                      }
                      value={scheduleForm.timezone}
                    >
                      <option value={scheduleForm.timezone}>{scheduleForm.timezone}</option>
                      <option value="UTC">UTC</option>
                      <option value="America/Los_Angeles">America/Los_Angeles</option>
                    </select>
                  </label>
                  <label>
                    会议密码（可选）
                    <input
                      onChange={(event) =>
                        setScheduleForm((current) => ({ ...current, password: event.target.value }))
                      }
                      placeholder="留空则无需密码"
                      value={scheduleForm.password}
                    />
                  </label>
                  <div className="button-row">
                    <button className="primary-button" type="submit">
                      预定会议
                    </button>
                    <button className="ghost-button" onClick={() => setEntryView("home")} type="button">
                      返回
                    </button>
                  </div>
                </form>
              </section>
            ) : null}

            {entryView === "join" ? (
              <section className="panel auth-card auth-card-join" data-view="join">
                <div>
                  <p className="eyebrow">Join meeting</p>
                  <h2>加入会议</h2>
                </div>
                <p className="section-copy">
                  先输入会议号和昵称，确认会议存在且可加入后，再弹出固定悬浮窗继续输入密码。
                </p>
                {showEntryFeedback ? (
                  <div className="status-stack compact">
                    {statusMessage !== defaultEntryStatusMessage ? (
                      <div className="status-pill">{statusMessage}</div>
                    ) : null}
                    {errorMessage ? <div className="status-error">{errorMessage}</div> : null}
                  </div>
                ) : null}
                <form className="form-grid" onSubmit={handleLookupMeeting}>
                  <label>
                    会议号 / Meeting ID
                    <div className="field-shell">
                      <input
                        onChange={(event) => {
                          setJoinForm((current) => ({ ...current, meetingId: event.target.value }));
                          setJoinLookupMeeting(null);
                          setShowJoinPasswordModal(false);
                        }}
                        value={joinForm.meetingId}
                      />
                      <button
                        className="mini-action-button"
                        onClick={openJoinScanModal}
                        type="button"
                      >
                        扫码
                      </button>
                    </div>
                  </label>
                  <label>
                    昵称
                    <input
                      onChange={(event) =>
                        setJoinForm((current) => ({ ...current, nickname: event.target.value }))
                      }
                      value={joinForm.nickname}
                    />
                  </label>
                  <label className="checkbox-row">
                    <input
                      checked={joinForm.requestCameraEnabled}
                      onChange={(event) =>
                        setJoinForm((current) => ({
                          ...current,
                          requestCameraEnabled: event.target.checked
                        }))
                      }
                      type="checkbox"
                    />
                    入会时希望开启摄像头
                  </label>
                  <label className="checkbox-row">
                    <input
                      checked={joinForm.requestMicrophoneEnabled}
                      onChange={(event) =>
                        setJoinForm((current) => ({
                          ...current,
                          requestMicrophoneEnabled: event.target.checked
                        }))
                      }
                      type="checkbox"
                    />
                    入会时希望开启麦克风
                  </label>
                  <div className="button-row">
                    <button className="primary-button" type="submit">
                      验证会议号
                    </button>
                    <button
                      className="ghost-button"
                      onClick={() => setEntryView(isAuthenticated ? "home" : "login")}
                      type="button"
                    >
                      返回
                    </button>
                  </div>
                </form>

                {showJoinPasswordModal ? (
                  <div className="inline-modal-layer">
                    <div className="inline-modal-card">
                      <span className="meeting-badge">
                        会议号 {joinLookupMeeting ? formatMeetingNumberDisplay(getMeetingPublicNumber(joinLookupMeeting)) : ""} 已确认
                      </span>
                      <div>
                        <h3>请输入会议密码</h3>
                        <p className="section-copy">
                          当前会议需要密码，继续输入后将基于现有后端接口使用 `会议号 + 密码` 加入会议。
                        </p>
                      </div>
                      <label>
                        会议密码
                        <input
                          onChange={(event) =>
                            setJoinForm((current) => ({ ...current, password: event.target.value }))
                          }
                          type="password"
                          value={joinForm.password}
                        />
                      </label>
                      <div className="button-row">
                        <button className="primary-button" onClick={() => void handleConfirmJoinMeeting()} type="button">
                          加入并进入会议
                        </button>
                        <button className="ghost-button" onClick={() => setShowJoinPasswordModal(false)} type="button">
                          返回
                        </button>
                      </div>
                    </div>
                  </div>
                ) : null}

                {showJoinScanModal ? (
                  <div className="modal-layer">
                    <div className="modal-card">
                      <div>
                        <h3>扫码加入会议</h3>
                        <p>将分享二维码放到摄像头前方，识别后会自动回填会议号和密码。</p>
                      </div>
                      <input
                        accept="image/*"
                        capture="environment"
                        hidden
                        onChange={(event) => void handleJoinQRCodeFileSelected(event)}
                        ref={joinScannerFileInputRef}
                        type="file"
                      />
                      <div className="scanner-card">
                        {supportsLiveJoinScanner() ? (
                          <video autoPlay muted playsInline ref={joinScannerVideoRef} />
                        ) : (
                          <div className="scanner-placeholder">当前环境不支持直接摄像头扫码</div>
                        )}
                        <canvas className="scanner-canvas" ref={joinScannerCanvasRef} />
                      </div>
                      <div className="scanner-copy">
                        <span>{joinScanStatus}</span>
                        {joinScanError ? <strong>{joinScanError}</strong> : null}
                      </div>
                      <button className="secondary-button" onClick={handlePickJoinQRCodeImage} type="button">
                        拍照 / 选图识别二维码
                      </button>
                      <button className="ghost-button" onClick={() => setShowJoinScanModal(false)} type="button">
                        关闭扫码，改为手动输入
                      </button>
                    </div>
                  </div>
                ) : null}
              </section>
            ) : null}
          </aside>
        </section>
      </main>
    );
  }

  return (
    <main className="page-shell room-page">
      {meetingSession && activeReadyCheck?.status === "active" && localReadyCheckStatus === "pending" ? (
        <ReadyCheckOverlay
          round={activeReadyCheck}
          onRespond={handleRespondReadyCheck}
          participant={meetingSession.participant}
        />
      ) : null}

      <section className="room-shell room-shell--immersive">
        <header className="room-topbar">
          <div className="topbar-left">
            <div className="room-brand">
              <div className="room-meta">
                <strong>会议详情</strong>
                <span>{roomClockLabel}</span>
              </div>
            </div>

            <div className="topbar-status" aria-label="连接与分享">
              <div className="network-hover-anchor">
                <button
                  className="topbar-action topbar-action--plain icon-only"
                  type="button"
                  aria-label="网络状态"
                >
                  <MeetingIcon name="signal" />
                </button>
                <div className="network-hover-card">
                  <div className="network-hover-row">
                    <strong>网络状态</strong>
                    <span>{networkSummaryLabel}</span>
                  </div>
                  <div className="network-hover-row">
                    <strong>延迟</strong>
                    <span>{networkLatencyLabel}</span>
                  </div>
                  <div className="network-hover-row">
                    <strong>丢包率</strong>
                    <span>{networkPacketLossLabel}</span>
                  </div>
                  <div className="network-hover-row">
                    <strong>传输速率</strong>
                    <span>{networkThroughputLabel}</span>
                  </div>
                </div>
              </div>
              <button
                className="topbar-action topbar-action--plain icon-only"
                onClick={() => openMeetingModal("invite")}
                type="button"
                aria-label="分享会议"
              >
                <MeetingIcon name="share" />
              </button>
            </div>
          </div>

          <div className="topbar-right">
            {canAccessHostTools ? (
              <div className="attached-anchor attached-anchor--top">
                <button
                  className={`topbar-action host-tools ${currentMenu === "host" ? "is-open" : ""}`}
                  onClick={() => toggleMenuWindow("host")}
                  type="button"
                >
                  <span className="topbar-icon">
                    <MeetingIcon name="person" />
                  </span>
                  <span>主持人工具</span>
                  <small>▾</small>
                </button>
                {currentMenu === "host" ? (
                  <div className="attached-panel attached-panel--top attached-panel--menu">
                    <div className="attached-panel-header">
                      <strong>主持人工具</strong>
                      <span>{isHost ? "主持人" : "助理"}</span>
                    </div>
                    <div className="attached-action-list">
                      <AttachedActionButton
                        description="处理权限申请、主持人授权与助理设置。"
                        icon="person"
                        onClick={() => openMeetingModal("permissions")}
                        title="授权与角色"
                      />
                      <AttachedActionButton
                        description="发起点名并查看当前轮次状态。"
                        icon="members"
                        onClick={() => openMeetingModal("ready_check_panel")}
                        title="就位确认"
                      />
                      <AttachedActionButton
                        description="控制本地录制并导出临时纪要。"
                        icon="record"
                        onClick={() => openMeetingModal("recording_panel")}
                        title="录制与纪要"
                      />
                      <AttachedActionButton
                        description="打开沉浸式白板面板。"
                        icon="apps"
                        onClick={() => openMeetingModal("whiteboard_panel")}
                        title="白板"
                      />
                      <AttachedActionButton
                        description="查看最近的媒体审计摘要和事件流。"
                        icon="layout"
                        onClick={() => openMeetingModal("audit_panel")}
                        title="审计与事件"
                      />
                    </div>
                  </div>
                ) : null}
              </div>
            ) : null}

            <div className="attached-anchor attached-anchor--top">
              <button
                className={`topbar-action meeting-tools ${currentMenu === "participant" ? "is-open" : ""}`}
                onClick={() => toggleMenuWindow("participant")}
                type="button"
              >
                <span className="topbar-icon">
                  <MeetingIcon name="apps" />
                </span>
                <span>会议工具</span>
                <small>▾</small>
              </button>
              {currentMenu === "participant" ? (
                <div className="attached-panel attached-panel--top attached-panel--menu">
                  <div className="attached-panel-header">
                    <strong>会议工具</strong>
                    <span>{meetingSession.participant.nickname}</span>
                  </div>
                  <div className="attached-action-list">
                    <AttachedActionButton
                      description="查看 Join Code、二维码和链接复制入口。"
                      icon="invite"
                      onClick={() => openMeetingModal("invite")}
                      title="邀请参会者"
                    />
                    <AttachedActionButton
                      description="修改你当前在会议中的显示昵称。"
                      icon="edit"
                      onClick={() => openMeetingModal("nickname")}
                      title="修改昵称"
                    />
                    <AttachedActionButton
                      description="向主持人申请摄像头、麦克风、录制等能力。"
                      icon="person"
                      onClick={() => openMeetingModal("permissions")}
                      title="申请权限"
                    />
                    <AttachedActionButton
                      description="保留会议运行态，当前账号退出房间。"
                      icon="end"
                      onClick={() => void handleLeaveMeeting()}
                      title="离开会议"
                    />
                  </div>
                </div>
              ) : null}
            </div>

            <div className="attached-anchor attached-anchor--top">
              <button
                className={`topbar-action topbar-action--plain utility-tools ${currentAttachedPanel === "settings" ? "is-open" : ""}`}
                onClick={() => toggleAttachedWindow("settings")}
                type="button"
              >
                <span className="topbar-icon">
                  <MeetingIcon name="settings" />
                </span>
                <span>设置</span>
              </button>
              {currentAttachedPanel === "settings" ? (
                <div className="attached-panel attached-panel--top attached-panel--settings">
                  <div className="attached-panel-header">
                    <strong>设置</strong>
                    <span>{formatMeetingStatus(meetingSession.meeting.status)}</span>
                  </div>
                  <div className="attached-stat-grid">
                    <div>
                      <strong>信令</strong>
                      <span>{connectionLabel}</span>
                    </div>
                    <div>
                      <strong>身份</strong>
                      <span>{meetingSession.participant.nickname} / {formatParticipantRole(meetingSession.participant.role)}</span>
                    </div>
                  </div>
                  <div className="attached-button-stack">
                    <button className="secondary-button" onClick={handleConnectSignal} type="button">
                      重连信令
                    </button>
                    <button className="ghost-button" onClick={handleDisconnectSignal} type="button">
                      断开信令
                    </button>
                    <button className="ghost-button" onClick={() => void handleCopyClientLogs()} type="button">
                      复制前端日志
                    </button>
                  </div>
                </div>
              ) : null}
            </div>

            <button
              className={`topbar-action icon-only utility-tools ${fullscreenActive ? "is-active" : ""}`}
              onClick={() => void handleToggleFullscreenMode()}
              type="button"
              aria-label={fullscreenActive ? "退出全屏模式" : "进入全屏模式"}
            >
              <MeetingIcon name={fullscreenActive ? "fullscreen-exit" : "fullscreen"} />
            </button>
          </div>
        </header>

        <section className={`stage-shell ${currentSidebar !== "none" ? "has-side-drawer" : ""}`}>
          {featuredStageItem ? (
            <div className="active-stage-layout">
              <section className="featured-panel">
                <div className="featured-header">
                  <div>
                    <strong>{featuredStageItem.label}</strong>
                    <span>{featuredStageItem.meta}</span>
                  </div>
                  <div className="meeting-pill-row">
                    <span className="meeting-pill">{featuredStageItem.variant === "screen" ? "Screen share" : "Camera on"}</span>
                    <span className="meeting-pill">{featuredStageItem.micEnabled ? "Mic on" : "Mic off"}</span>
                  </div>
                </div>
                <div className={`featured-canvas ${featuredStageItem.variant === "screen" ? "is-screen" : ""}`}>
                  <StreamFrame
                    className="featured-stream"
                    muted={featuredStageItem.isLocal}
                    placeholder={renderStreamFallback(featuredStageItem.label, featuredStageItem.variant)}
                    stream={featuredStageItem.stream}
                  />
                  <div className="featured-overlay">
                    <strong>{featuredStageItem.variant === "screen" ? "共享内容主舞台" : "视频主舞台"}</strong>
                    <span>双击右侧缩略窗可以切换主画面。默认优先展示主持人的活动画面。</span>
                  </div>
                </div>
              </section>

              <aside className="thumbnail-rail">
                {thumbnailItems.length === 0 ? (
                  <div className="thumbnail-empty">当前只有一张活动画面。</div>
                ) : (
                  thumbnailItems.map((item) => (
                    <button
                      className="thumbnail-card"
                      key={item.id}
                      onDoubleClick={() => setFeaturedStageId(item.id)}
                      type="button"
                    >
                      <div className={`thumbnail-preview ${item.variant === "screen" ? "is-screen" : ""}`}>
                        <StreamFrame
                          className="thumbnail-stream"
                          muted={item.isLocal}
                          placeholder={renderStreamFallback(item.label, item.variant)}
                          stream={item.stream}
                        />
                      </div>
                      <div className="thumbnail-copy">
                        <strong>{item.label}</strong>
                        <span>{item.meta}</span>
                      </div>
                    </button>
                  ))
                )}
              </aside>
            </div>
          ) : (
            <div className="idle-stage">
              <div className="avatar-wall room-avatar-wall">
                {stageParticipants.map((participant, index) => {
                  const micEnabled = resolveParticipantMicEnabled(
                    participant,
                    meetingSession.participant.id,
                    localMicEnabled
                  );
                  const isLocalParticipant = participant.id === meetingSession.participant.id;

                  return (
                    <article className={`avatar-card room-avatar-card avatar-tone-${(index % 4) + 1}`} key={participant.id}>
                      <div className="avatar-badge">{initialsFromName(participant.nickname)}</div>
                      <div className="room-avatar-copy">
                        <div className="room-name-row">
                          <span
                            aria-label={micEnabled ? "发言中" : "静音"}
                            className={`room-mic-chip ${micEnabled ? "" : "is-off"}`}
                          >
                            <MeetingIcon name={micEnabled ? "mic-on" : "mic-off"} />
                          </span>
                          <strong>{participant.nickname}</strong>
                          {isLocalParticipant ? (
                            <button
                              aria-label="编辑昵称"
                              className="nickname-edit-inline"
                              onClick={() => openMeetingModal("nickname")}
                              type="button"
                            >
                              <MeetingIcon name="edit" />
                            </button>
                          ) : null}
                        </div>
                        <span>{buildJoinOrderLabel(stageParticipants, participant.id)} · {formatParticipantRole(participant.role)}</span>
                      </div>
                    </article>
                  );
                })}
              </div>
            </div>
          )}

          {currentSidebar === "members" ? (
            <aside className="side-drawer">
              <div className="drawer-header">
                <div>
                  <h3>成员</h3>
                  <p>默认隐藏，打开后从右侧抽出。</p>
                </div>
                <span className="drawer-pill">{onlineParticipantIds.length} online</span>
              </div>
              <ul className="member-list">
                {participants.map((participant) => (
                  <li className="member-row" key={participant.id}>
                    <strong>{participant.nickname}</strong>
                    <div className="member-meta">
                      <span>{formatParticipantRole(participant.role)}</span>
                      <span>{onlineParticipantIds.includes(participant.id) ? "online" : "offline"}</span>
                    </div>
                  </li>
                ))}
              </ul>
              <button className="ghost-button" onClick={() => setCurrentSidebar("none")} type="button">
                收起成员列表
              </button>
            </aside>
          ) : null}

          {currentSidebar === "chat" ? (
            <aside className="side-drawer">
              <div className="drawer-header">
                <div>
                  <h3>聊天</h3>
                  <p>默认隐藏，打开后贴在舞台右侧。</p>
                </div>
                <span className="drawer-pill">{chatMessages.length} 条</span>
              </div>
              <ul className="chat-list">
                {chatMessages.length === 0 ? (
                  <li className="chat-row empty">当前还没有聊天消息。</li>
                ) : (
                  chatMessages
                    .slice()
                    .reverse()
                    .map((message) => (
                      <li className="chat-row" key={message.id}>
                        <strong>{message.nickname}</strong>
                        <div className="chat-meta">
                          <span>{message.message}</span>
                          <span>{new Date(message.sentAt).toLocaleTimeString("zh-CN", { hour12: false })}</span>
                        </div>
                      </li>
                    ))
                )}
              </ul>
              <form className="chat-composer" onSubmit={handleSendChat}>
                <textarea
                  onChange={(event) => setChatInput(event.target.value)}
                  placeholder="输入一条聊天消息"
                  value={chatInput}
                />
                <button className="secondary-button" type="submit">
                  发送消息
                </button>
              </form>
            </aside>
          ) : null}

          {currentModal === "invite" ? (
            <div className="modal-layer">
              <div className="modal-card modal-card--wide room-share-modal">
                <button
                  aria-label="关闭分享窗口"
                  className="modal-card-close"
                  onClick={() => setCurrentModal("none")}
                  type="button"
                >
                  <span aria-hidden="true">×</span>
                </button>

                <div className="invite-head">
                  <div className="invite-title-row">
                      <div className="invite-title-copy">
                        <div className="invite-eyebrow">meeting</div>
                        <div className="invite-id-row">
                          <h3>会议号：{inviteMeetingNumberLabel}</h3>
                          <button
                            aria-label="复制会议号"
                            className="meeting-id-copy"
                          onClick={() => void handleCopyMeetingID()}
                          type="button"
                        >
                          <MeetingIcon name="copy" />
                        </button>
                      </div>
                    </div>
                  </div>
                </div>

                <div className="invite-layout">
                  <div className="invite-copy-flow">
                    <p>{meetingSession.participant.nickname} 邀请您参加 meeting 快速会议</p>
                    <p>会议主题：{meetingSession.meeting.title}</p>
                    <p>会议时间：{inviteMeetingTimeLabel}</p>
                    <div aria-hidden="true" className="invite-divider"></div>
                    <p>点击链接直接加入会议：</p>
                    <p className="invite-link">{inviteJoinURL}</p>
                    <p>会议号：{inviteMeetingNumberLabel}</p>
                  </div>

                  <div className="invite-qr-panel">
                    {shareQrDataUrl ? (
                      <img alt="会议分享二维码" className="qr-share-image room-share-qr-image" src={shareQrDataUrl} />
                    ) : (
                      <div className="qr-share-placeholder room-share-qr-placeholder">二维码生成中...</div>
                    )}
                    <div className="qr-caption">会议ID：{inviteMeetingNumberLabel}</div>
                  </div>
                </div>

                <div className="button-row invite-actions">
                  <button className="secondary-button" onClick={() => void handleCopyInvite()} type="button">
                    复制全部信息
                  </button>
                  <button className="primary-button" onClick={() => void handleCopyInviteQRCode()} type="button">
                    复制会议二维码
                  </button>
                </div>
              </div>
            </div>
          ) : null}

          {currentModal === "record_request" ? (
            <div className="modal-layer">
              <div className="modal-card">
                <div>
                  <h3>申请录制权限</h3>
                  <p>当前账号没有录制权限。确认后会向主持人发送权限申请，而不是直接开始录制。</p>
                </div>
                <div className="info-grid dense">
                  <div>
                    <strong>当前状态</strong>
                    <span>record capability unavailable</span>
                  </div>
                  <div>
                    <strong>发起对象</strong>
                    <span>{findParticipantLabel(participants, meetingSession.meeting.hostParticipantId)}</span>
                  </div>
                </div>
                <div className="button-row">
                  <button className="primary-button" onClick={handleRequestRecordPermission} type="button">
                    向主持人申请
                  </button>
                  <button className="ghost-button" onClick={() => setCurrentModal("none")} type="button">
                    取消
                  </button>
                </div>
              </div>
            </div>
          ) : null}

          {currentModal === "nickname" ? (
            <div className="modal-layer">
              <div className="modal-card modal-card--compact">
                <div>
                  <h3>修改昵称</h3>
                  <p>输入框默认加载当前昵称，保存后会同步更新当前预览中的昵称文本。</p>
                </div>
                <label className="form-grid">
                  <span>昵称</span>
                  <input onChange={(event) => setNicknameDraft(event.target.value)} value={nicknameDraft} />
                </label>
                <div className="button-row">
                  <button className="primary-button" onClick={() => void handleUpdateNickname()} type="button">
                    保存
                  </button>
                  <button className="ghost-button" onClick={() => setCurrentModal("none")} type="button">
                    取消
                  </button>
                </div>
              </div>
            </div>
          ) : null}

          {currentModal === "permissions" ? (
            <div className="modal-layer">
              <div className="modal-card modal-card--wide">
                <div>
                  <h3>权限与角色</h3>
                  <p>参会者默认只拥有基础聊天能力，更多能力由主持人或助理管理。</p>
                </div>
                <div className="form-grid">
                  <label>
                    我想申请
                    <select
                      onChange={(event) => setCapabilityToRequest(event.target.value as Capability)}
                      value={capabilityToRequest}
                    >
                      {requestableCapabilities.map((capability) => (
                        <option key={capability} value={capability}>
                          {capability}
                        </option>
                      ))}
                    </select>
                  </label>
                  <button className="primary-button" onClick={handleRequestCapability} type="button">
                    发起权限申请
                  </button>
                  {isHost ? (
                    <>
                      <label>
                        授权目标 participantId
                        <input onChange={(event) => setGrantTargetId(event.target.value)} value={grantTargetId} />
                      </label>
                      <label>
                        授权能力
                        <select
                          onChange={(event) => setGrantCapability(event.target.value as Capability)}
                          value={grantCapability}
                        >
                          {requestableCapabilities.map((capability) => (
                            <option key={capability} value={capability}>
                              {capability}
                            </option>
                          ))}
                        </select>
                      </label>
                      <button className="secondary-button" onClick={handleGrantCapability} type="button">
                        主持人授权
                      </button>
                      <label>
                        助理 participantId
                        <input
                          onChange={(event) => setAssistantTargetId(event.target.value)}
                          value={assistantTargetId}
                        />
                      </label>
                      <button className="ghost-button" onClick={handleAssignAssistant} type="button">
                        设为助理
                      </button>
                    </>
                  ) : null}
                </div>
                <button className="ghost-button" onClick={() => setCurrentModal("none")} type="button">
                  关闭
                </button>
              </div>
            </div>
          ) : null}

          {currentModal === "ready_check_panel" ? (
            <div className="modal-layer">
              <div className="modal-card modal-card--wide">
                <div>
                  <h3>就位确认</h3>
                  <p>发起后会向当前在线成员广播点名提示，并记录每个人的响应状态。</p>
                </div>
                <div className="form-grid">
                  <label>
                    超时时间（秒）
                    <input
                      min={5}
                      onChange={(event) => setReadyCheckTimeoutSeconds(Number(event.target.value))}
                      type="number"
                      value={readyCheckTimeoutSeconds}
                    />
                  </label>
                  <button
                    className="primary-button"
                    disabled={!canReadyCheck || !wsConnected}
                    onClick={handleStartReadyCheck}
                    type="button"
                  >
                    发起就位确认
                  </button>
                  {activeReadyCheck ? (
                    <div className="ready-check-card">
                      <strong>轮次 {activeReadyCheck.id}</strong>
                      <span>
                        截止时间：{new Date(activeReadyCheck.deadlineAt).toLocaleTimeString("zh-CN", { hour12: false })}
                      </span>
                      <ul className="ready-check-list">
                        {Object.values(activeReadyCheck.results).map((result) => (
                          <li key={result.participantId}>
                            <span>{findParticipantLabel(participants, result.participantId)}</span>
                            <strong>{result.status}</strong>
                          </li>
                        ))}
                      </ul>
                    </div>
                  ) : (
                    <p className="empty-copy">还没有开始就位确认。</p>
                  )}
                </div>
                <button className="ghost-button" onClick={() => setCurrentModal("none")} type="button">
                  关闭
                </button>
              </div>
            </div>
          ) : null}

          {currentModal === "recording_panel" ? (
            <div className="modal-layer">
              <div className="modal-card modal-card--wide">
                <div>
                  <h3>录制与纪要</h3>
                  <p>在单屏布局下，录制控制和纪要导出收纳到一个覆盖层中处理。</p>
                </div>
                <div className="form-grid">
                  <label>
                    录制模式
                    <select
                      disabled={!canRecord || recordingActive}
                      onChange={(event) => setRecordingKind(event.target.value as RecordingKind)}
                      value={recordingKind}
                    >
                      <option value="meeting_video">会议画面</option>
                      <option value="audio_only">仅录音</option>
                    </select>
                  </label>
                  <div className="button-row">
                    <button className="primary-button" onClick={() => void handleStartRecording()} type="button">
                      开始录制
                    </button>
                    <button
                      className="ghost-button"
                      disabled={!recordingActive}
                      onClick={() => void handleStopRecording()}
                      type="button"
                    >
                      停止录制
                    </button>
                  </div>
                  {recordingAsset ? (
                    <div className="recording-card">
                      <strong>{recordingAsset.fileName}</strong>
                      <span>
                        {recordingAsset.kind} / {formatBytes(recordingAsset.sizeBytes)} /{" "}
                        {formatDuration(recordingAsset.durationMs)}
                      </span>
                      <div className="button-row">
                        <button className="primary-button" onClick={() => void handleDownloadRecording()} type="button">
                          下载录制
                        </button>
                        <button className="ghost-button" onClick={handleDiscardRecording} type="button">
                          丢弃缓存
                        </button>
                      </div>
                    </div>
                  ) : null}
                  <div className="button-row">
                    <button
                      className="secondary-button"
                      disabled={temporaryMinutes.length === 0}
                      onClick={handleExportMinutes}
                      type="button"
                    >
                      导出临时纪要
                    </button>
                  </div>
                  <ul className="minutes-list">
                    {temporaryMinutes.length === 0 ? (
                      <li className="empty-copy">临时纪要还没有内容。</li>
                    ) : (
                      temporaryMinutes
                        .slice()
                        .reverse()
                        .map((minute, index) => <li key={`${minute}-${index}`}>{minute}</li>)
                    )}
                  </ul>
                </div>
                <button className="ghost-button" onClick={() => setCurrentModal("none")} type="button">
                  关闭
                </button>
              </div>
            </div>
          ) : null}

          {currentModal === "whiteboard_panel" ? (
            <div className="modal-layer">
              <div className="modal-card modal-card--whiteboard">
                <WhiteboardPanel
                  actions={whiteboardActions}
                  canDraw={canWhiteboard}
                  onClear={handleClearWhiteboard}
                  onSubmitAction={handleSubmitWhiteboardAction}
                  participants={participants}
                />
                <button className="ghost-button" onClick={() => setCurrentModal("none")} type="button">
                  关闭白板
                </button>
              </div>
            </div>
          ) : null}

          {currentModal === "audit_panel" ? (
            <div className="modal-layer">
              <div className="modal-card modal-card--wide">
                <div>
                  <h3>审计与事件</h3>
                  <p>用于查看最近 80 条关键事件，以及当前媒体上报的基础指标。</p>
                </div>
                {auditSummary ? (
                  <div className="audit-grid">
                    <div>
                      <strong>平均延迟</strong>
                      <span>{auditSummary.latencyMs.toFixed(0)} ms</span>
                    </div>
                    <div>
                      <strong>丢包率</strong>
                      <span>{(auditSummary.packetLossRate * 100).toFixed(2)}%</span>
                    </div>
                    <div>
                      <strong>平均帧率</strong>
                      <span>{auditSummary.averageFps.toFixed(1)} fps</span>
                    </div>
                    <div>
                      <strong>平均码率</strong>
                      <span>{auditSummary.averageBitrateKbps.toFixed(1)} kbps</span>
                    </div>
                    <div>
                      <strong>对端连接数</strong>
                      <span>{auditSummary.peerCount}</span>
                    </div>
                    <div>
                      <strong>最近上报</strong>
                      <span>{auditSummary.updatedAt}</span>
                    </div>
                  </div>
                ) : (
                  <p className="empty-copy">接入 WSS 并建立 P2P 连接后，会按周期上报基础审计数据。</p>
                )}
                <ul className="event-list">
                  {events.length === 0 ? (
                    <li className="empty-copy">暂无事件</li>
                  ) : (
                    events.map((event) => (
                      <li key={event.id}>
                        <div>
                          <strong>{event.type}</strong>
                          <span>{event.text}</span>
                        </div>
                        <time>{event.createdAt}</time>
                      </li>
                    ))
                  )}
                </ul>
                <button className="ghost-button" onClick={() => setCurrentModal("none")} type="button">
                  关闭
                </button>
              </div>
            </div>
          ) : null}

          {currentModal === "meeting_ended" ? (
            <div className="modal-layer">
              <div className="modal-card">
                <div>
                  <h3>会议已结束</h3>
                  <p>你可以先导出当前会议纪要，再返回{describeReturnAfterMeeting(returnAfterMeetingView)}。</p>
                </div>
                <div className="info-grid dense">
                  <div>
                    <strong>会议标题</strong>
                    <span>{meetingSession.meeting.title}</span>
                  </div>
                  <div>
                    <strong>返回位置</strong>
                    <span>{describeReturnAfterMeeting(returnAfterMeetingView)}</span>
                  </div>
                  <div>
                    <strong>临时纪要</strong>
                    <span>{temporaryMinutes.length} 条</span>
                  </div>
                  <div>
                    <strong>聊天消息</strong>
                    <span>{chatMessages.length} 条</span>
                  </div>
                </div>
                <div className="button-row">
                  <button className="primary-button" onClick={handleExportMinutes} type="button">
                    保存当前会议纪要
                  </button>
                  <button className="secondary-button" onClick={handleReturnAfterMeeting} type="button">
                    返回{describeReturnAfterMeeting(returnAfterMeetingView)}
                  </button>
                </div>
              </div>
            </div>
          ) : null}
        </section>

        <footer className="room-toolbar">
          <div className="toolbar-center">
            <div className="tool-rack">
              <button
                className={`meeting-tool ${basePreference.microphone ? "is-active" : "is-muted"}`}
                onClick={handleToggleMicrophone}
                type="button"
              >
                <span className="tool-icon">
                  <MeetingIcon name={basePreference.microphone ? "mic-on" : "mic-off"} />
                </span>
                <span className="tool-label">{basePreference.microphone ? "静音" : "解除静音"}</span>
              </button>
              <button
                className={`meeting-tool ${basePreference.camera ? "is-active" : "is-camera-off"}`}
                onClick={handleToggleCamera}
                type="button"
              >
                <span className="tool-icon">
                  <MeetingIcon name={basePreference.camera ? "camera-on" : "camera-off"} />
                </span>
                <span className="tool-label">{basePreference.camera ? "关闭视频" : "开启视频"}</span>
              </button>
              <button
                className={`meeting-tool ${screenSharing ? "is-active" : ""}`}
                disabled={!screenSharing && !canScreenShare}
                onClick={() => void (screenSharing ? handleStopScreenShare() : handleStartScreenShare())}
                type="button"
              >
                <span className="tool-icon">
                  <MeetingIcon name="share" />
                </span>
                <span className="tool-label">{screenSharing ? "停止共享" : "共享屏幕"}</span>
              </button>
              <button className="meeting-tool" onClick={() => openMeetingModal("invite")} type="button">
                <span className="tool-icon">
                  <MeetingIcon name="invite" />
                </span>
                <span className="tool-label">邀请</span>
              </button>
              <button
                className={`meeting-tool ${currentSidebar === "members" ? "is-active" : ""}`}
                onClick={() => toggleSidebarDrawer("members")}
                type="button"
              >
                <span className="tool-icon">
                  <MeetingIcon name="members" />
                </span>
                <span className="tool-label">成员({onlineParticipantIds.length})</span>
              </button>
              <button
                className={`meeting-tool ${currentSidebar === "chat" ? "is-active" : ""}`}
                onClick={() => toggleSidebarDrawer("chat")}
                type="button"
              >
                <span className="tool-icon">
                  <MeetingIcon name="chat" />
                </span>
                <span className="tool-label">聊天</span>
              </button>
              <button
                className={`meeting-tool ${recordingActive ? "is-active is-danger" : ""}`}
                onClick={() => void (recordingActive ? handleStopRecording() : handleStartRecording())}
                type="button"
              >
                <span className="tool-icon">
                  <MeetingIcon name="record" />
                </span>
                <span className="tool-label">{recordingActive ? "停止录制" : "录制"}</span>
              </button>

              <div className="attached-anchor attached-anchor--bottom">
                <button
                  className={`meeting-tool apps-tool ${currentAttachedPanel === "apps" ? "is-active" : ""}`}
                  onClick={() => toggleAttachedWindow("apps")}
                  type="button"
                >
                  <span className="tool-icon">
                    <MeetingIcon name="apps" />
                  </span>
                  <span className="tool-label">应用</span>
                  <span className="indicator-dot" />
                </button>
                {currentAttachedPanel === "apps" ? (
                  <div className="attached-panel attached-panel--bottom attached-panel--apps">
                    <div className="attached-panel-header">
                      <strong>应用</strong>
                      <span>打开扩展能力面板</span>
                    </div>
                    <div className="attached-action-list">
                      <AttachedActionButton
                        description="打开共享白板并保留当前会议舞台。"
                        icon="apps"
                        onClick={() => openMeetingModal("whiteboard_panel")}
                        title="白板"
                      />
                      <AttachedActionButton
                        description="查看本地录制状态和临时纪要。"
                        icon="record"
                        onClick={() => openMeetingModal("recording_panel")}
                        title="录制与纪要"
                      />
                      <AttachedActionButton
                        description="打开媒体统计和事件日志。"
                        icon="layout"
                        onClick={() => openMeetingModal("audit_panel")}
                        title="审计与事件"
                      />
                    </div>
                  </div>
                ) : null}
              </div>
            </div>
          </div>

          <div className="toolbar-end">
            {isHost ? (
              <div className="attached-anchor attached-anchor--bottom attached-anchor--danger">
                <button
                  className={`end-call-button ${currentAttachedPanel === "end" ? "cancel-mode" : ""}`}
                  onClick={() => toggleAttachedWindow("end")}
                  type="button"
                >
                  <span className="tool-icon">
                    <MeetingIcon name="end" />
                  </span>
                  <span className="tool-label">{currentAttachedPanel === "end" ? "取消" : "结束会议"}</span>
                </button>
                {currentAttachedPanel === "end" ? (
                  <div className="attached-panel attached-panel--bottom attached-panel--end">
                    <AttachedActionButton
                      danger
                      description="结束后所有参会者都会退出当前会议。"
                      disabled={endingMeetingPending}
                      icon="end"
                      onClick={() => void handleConfirmEndMeeting()}
                      title={endingMeetingPending ? "正在结束会议..." : "全员结束会议"}
                    />
                    <AttachedActionButton
                      description="保留会议继续进行，仅当前账号退出。"
                      icon="end"
                      onClick={() => void handleLeaveMeeting()}
                      title="离开会议"
                    />
                  </div>
                ) : null}
              </div>
            ) : (
              <button className="end-call-button leave-mode" onClick={() => void handleLeaveMeeting()} type="button">
                <span className="tool-icon">
                  <MeetingIcon name="end" />
                </span>
                <span className="tool-label">离开会议</span>
              </button>
            )}
          </div>
        </footer>

        <MeetingIconSprite />
      </section>
    </main>
  );
}

type MeetingIconName =
  | "copy"
  | "edit"
  | "signal"
  | "share"
  | "settings"
  | "fullscreen"
  | "fullscreen-exit"
  | "mic-off"
  | "mic-on"
  | "camera-off"
  | "camera-on"
  | "invite"
  | "members"
  | "chat"
  | "record"
  | "apps"
  | "end"
  | "person"
  | "layout";

function MeetingIcon(props: { name: MeetingIconName }) {
  return (
    <svg aria-hidden="true" focusable="false" viewBox="0 0 24 24">
      <use href={`#icon-${props.name}`} />
    </svg>
  );
}

function MeetingIconSprite() {
  return (
    <svg aria-hidden="true" className="icon-sprite" focusable="false">
      <symbol id="icon-layout" viewBox="0 0 24 24">
        <rect height="7" rx="1.5" width="7" x="4" y="4" />
        <rect height="7" rx="1.5" width="7" x="13" y="4" />
        <rect height="7" rx="1.5" width="7" x="4" y="13" />
        <rect height="7" rx="1.5" width="7" x="13" y="13" />
      </symbol>
      <symbol id="icon-settings" viewBox="0 0 24 24">
        <circle cx="12" cy="12" r="3.2" />
        <path d="M12 2.8v2.1M12 19.1v2.1M4.9 4.9l1.5 1.5M17.6 17.6l1.5 1.5M2.8 12h2.1M19.1 12h2.1M4.9 19.1l1.5-1.5M17.6 6.4l1.5-1.5" />
      </symbol>
      <symbol id="icon-fullscreen" viewBox="0 0 24 24">
        <path d="M8 3H3v5M16 3h5v5M21 16v5h-5M3 16v5h5" />
      </symbol>
      <symbol id="icon-fullscreen-exit" viewBox="0 0 24 24">
        <path d="M8 3H3v5M16 3h5v5M21 16v5h-5M3 16v5h5" />
        <path d="M9 9H5V5M15 9h4V5M9 15H5v4M15 15h4v4" />
      </symbol>
      <symbol id="icon-signal" viewBox="0 0 24 24">
        <path d="M5 18v-4M10 18v-8M15 18v-11M20 18v-14" />
      </symbol>
      <symbol id="icon-share" viewBox="0 0 24 24">
        <rect height="10" rx="1.5" width="14" x="4" y="6" />
        <path d="M14 5h5v5M19 5 12 12" />
      </symbol>
      <symbol id="icon-mic-off" viewBox="0 0 24 24">
        <path d="M9 11v1a3 3 0 0 0 6 0v-5a3 3 0 0 0-6 0" />
        <path d="M5 11a7 7 0 0 0 14 0" />
        <path d="M12 18v3" />
        <path d="M8 21h8" />
        <path d="M4 4 20 20" />
      </symbol>
      <symbol id="icon-mic-on" viewBox="0 0 24 24">
        <path d="M9 9.5V11a3 3 0 0 0 6 0V7a3 3 0 0 0-6 0Z" />
        <path d="M5 11a7 7 0 0 0 14 0" />
        <path d="M12 18v3" />
        <path d="M8 21h8" />
      </symbol>
      <symbol id="icon-camera-off" viewBox="0 0 24 24">
        <path d="M4 8h11v8H4z" />
        <path d="m15 11 5-3v8l-5-3Z" />
        <path d="M3 3 21 21" />
      </symbol>
      <symbol id="icon-camera-on" viewBox="0 0 24 24">
        <rect height="8" rx="1.5" width="11" x="4" y="8" />
        <path d="m15 11 5-3v8l-5-3Z" />
        <circle cx="9.5" cy="12" r="2.2" />
      </symbol>
      <symbol id="icon-invite" viewBox="0 0 24 24">
        <circle cx="10" cy="8" r="3" />
        <path d="M4.5 20c0-3 2.5-5 5.5-5s5.5 2 5.5 5" />
        <path d="M17 8v6M14 11h6" />
      </symbol>
      <symbol id="icon-members" viewBox="0 0 24 24">
        <circle cx="9" cy="8" r="2.4" />
        <circle cx="16.5" cy="10" r="2" />
        <path d="M4.5 20a4.5 4.5 0 0 1 9 0" />
        <path d="M13.5 20a3.5 3.5 0 0 1 7 0" />
      </symbol>
      <symbol id="icon-chat" viewBox="0 0 24 24">
        <path d="M4 5h16v10H9l-5 4z" />
      </symbol>
      <symbol id="icon-record" viewBox="0 0 24 24">
        <circle cx="12" cy="12" r="5.2" />
      </symbol>
      <symbol id="icon-apps" viewBox="0 0 24 24">
        <rect height="6" rx="1.2" width="6" x="4" y="4" />
        <rect height="6" rx="1.2" width="6" x="14" y="4" />
        <rect height="6" rx="1.2" width="6" x="4" y="14" />
        <rect height="6" rx="1.2" width="6" x="14" y="14" />
      </symbol>
      <symbol id="icon-end" viewBox="0 0 24 24">
        <path d="M5 12a7 7 0 0 1 14 0" />
        <path d="M8 12h8" />
        <path d="M12 8v4" />
        <path d="M9 15.5c1 .7 2.1 1 3 1s2-.3 3-1" />
      </symbol>
      <symbol id="icon-person" viewBox="0 0 24 24">
        <circle cx="12" cy="8" r="3.2" />
        <path d="M5 20a7 7 0 0 1 14 0" />
      </symbol>
      <symbol id="icon-copy" viewBox="0 0 24 24">
        <rect height="10" rx="2" width="10" x="9" y="9" />
        <path d="M7 15H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h7a2 2 0 0 1 2 2v1" />
      </symbol>
      <symbol id="icon-edit" viewBox="0 0 24 24">
        <path d="M4 16.5V20h3.5L18.5 9.5 15 6 4 16.5Z" />
        <path d="m14.5 6.5 3 3" />
      </symbol>
    </svg>
  );
}

function AttachedActionButton(props: {
  description: string;
  disabled?: boolean;
  danger?: boolean;
  icon: MeetingIconName;
  onClick: () => void;
  title: string;
}) {
  return (
    <button
      className={`attached-action-item ${props.danger ? "is-danger" : ""}`}
      disabled={props.disabled}
      onClick={props.onClick}
      type="button"
    >
      <span className="attached-action-glyph">
        <MeetingIcon name={props.icon} />
      </span>
      <span className="attached-action-copy">
        <strong>{props.title}</strong>
        <span>{props.description}</span>
      </span>
    </button>
  );
}

function StreamFrame(props: {
  stream: MediaStream | null;
  muted: boolean;
  className?: string;
  placeholder?: ReactNode;
}) {
  const videoRef = useRef<HTMLVideoElement | null>(null);

  useEffect(() => {
    if (!videoRef.current) {
      return;
    }

    videoRef.current.srcObject = props.stream;
  }, [props.stream]);

  if (!props.stream) {
    return <div className={props.className}>{props.placeholder ?? <div className="media-fallback">暂无媒体流</div>}</div>;
  }

  return (
    <div className={props.className}>
      <video autoPlay muted={props.muted} playsInline ref={videoRef} />
    </div>
  );
}

function ReadyCheckOverlay(props: {
  round: ReadyCheckRound;
  participant: Participant;
  onRespond: (status: ReadyCheckStatus) => void;
}) {
  return (
    <div className="ready-check-overlay">
      <div className="ready-check-modal">
        <p className="eyebrow">Ready Check</p>
        <h2>{props.participant.nickname}，请确认你仍在设备前</h2>
        <p className="section-copy">
          本轮将在 {new Date(props.round.deadlineAt).toLocaleTimeString("zh-CN", { hour12: false })} 超时。
        </p>
        <div className="button-row">
          <button className="primary-button" onClick={() => props.onRespond("confirmed")} type="button">
            确认
          </button>
          <button className="danger-button" onClick={() => props.onRespond("cancelled")} type="button">
            取消
          </button>
        </div>
      </div>
    </div>
  );
}

function hasCapability(session: SessionState, capability: Capability): boolean {
  return Object.prototype.hasOwnProperty.call(session.participant.grantedCapabilities, capability);
}

function findParticipantLabel(participants: Participant[], participantId: string): string {
  return participants.find((participant) => participant.id === participantId)?.nickname ?? participantId;
}

function resolveBaseCapturePreference(stream: MediaStream | null) {
  return {
    camera: Boolean(stream?.getVideoTracks().some((track) => track.readyState === "live")),
    microphone: Boolean(stream?.getAudioTracks().some((track) => track.readyState === "live"))
  };
}

function describeCaptureSelection(preference: { camera: boolean; microphone: boolean }): string {
  if (preference.camera && preference.microphone) {
    return "camera + microphone";
  }
  if (preference.camera) {
    return "camera";
  }
  if (preference.microphone) {
    return "microphone";
  }
  return "none";
}

function buildInitialScheduleTime(): string {
  const nextHour = new Date();
  nextHour.setMinutes(0, 0, 0);
  nextHour.setHours(nextHour.getHours() + 1);
  return toDatetimeLocalValue(nextHour);
}

function buildDefaultScheduleForm(): ScheduleFormState {
  return {
    title: "全球产品评审会",
    scheduledAt: buildInitialScheduleTime(),
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
    password: ""
  };
}

function toDatetimeLocalValue(date: Date): string {
  const year = date.getFullYear();
  const month = `${date.getMonth() + 1}`.padStart(2, "0");
  const day = `${date.getDate()}`.padStart(2, "0");
  const hour = `${date.getHours()}`.padStart(2, "0");
  const minute = `${date.getMinutes()}`.padStart(2, "0");
  return `${year}-${month}-${day}T${hour}:${minute}`;
}

function buildHostIdentity(email: string) {
  const trimmed = email.trim().toLowerCase();
  if (!trimmed) {
    return {
      userId: "host-demo",
      nickname: "主持人"
    };
  }

  return {
    userId: trimmed.replace(/[^a-z0-9]+/g, "-"),
    nickname: "主持人"
  };
}

function sortParticipantsByJoinOrder(participants: Participant[]) {
  return [...participants].sort((left, right) => {
    const leftTimestamp = Date.parse(left.joinedAt);
    const rightTimestamp = Date.parse(right.joinedAt);
    return leftTimestamp - rightTimestamp;
  });
}

function resolveParticipantMicEnabled(
  participant: Participant,
  localParticipantId: string,
  localMicEnabled: boolean
): boolean {
  if (participant.id === localParticipantId) {
    return localMicEnabled;
  }

  return (
    participant.effectiveMediaState.microphoneEnabled ||
    participant.requestedMediaPreference.microphoneEnabled
  );
}

function buildJoinOrderLabel(participants: Participant[], participantId: string) {
  const index = participants.findIndex((participant) => participant.id === participantId);
  return `第 ${index + 1} 位入会`;
}

function scrollViewportToTop() {
  if (typeof window === "undefined") {
    return;
  }

  window.scrollTo({
    top: 0,
    behavior: "auto"
  });
}

function createLocalEventID() {
  if (typeof globalThis.crypto?.randomUUID === "function") {
    return globalThis.crypto.randomUUID();
  }

  return `event-${Date.now()}-${Math.random().toString(16).slice(2, 10)}`;
}

function formatMeetingStatus(status: Meeting["status"]) {
  return status === "active" ? "会议进行中" : "会议已结束";
}

function formatParticipantRole(role: Participant["role"]) {
  if (role === "host") {
    return "主持人";
  }

  if (role === "assistant") {
    return "助理";
  }

  return "参会者";
}

function formatElapsedClock(startedAt: string) {
  const startedAtMs = Date.parse(startedAt);
  if (Number.isNaN(startedAtMs)) {
    return "00:00";
  }

  const elapsedMs = Math.max(0, Date.now() - startedAtMs);
  const totalMinutes = Math.floor(elapsedMs / 60000);
  const hours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;

  return `${String(hours).padStart(2, "0")}:${String(minutes).padStart(2, "0")}`;
}

function initialsFromName(name: string) {
  const trimmed = name.trim();
  if (!trimmed) {
    return "ME";
  }

  const latin = trimmed
    .split(/\s+/)
    .map((part) => part[0])
    .join("")
    .slice(0, 2)
    .toUpperCase();

  if (latin) {
    return latin;
  }

  return trimmed.slice(0, 2).toUpperCase();
}

function renderStreamFallback(label: string, variant: "camera" | "screen") {
  return (
    <div className={`media-fallback ${variant === "screen" ? "is-screen" : ""}`}>
      <span>{label}</span>
    </div>
  );
}

function buildStageItems(input: {
  session: SessionState | null;
  localStream: MediaStream | null;
  remoteTiles: RemoteTile[];
  screenSharing: boolean;
  localMicEnabled: boolean;
}): StageItem[] {
  if (!input.session) {
    return [];
  }

  const participantsById = new Map(
    Object.values(input.session.meeting.participants).map((participant) => [participant.id, participant])
  );
  const items: StageItem[] = [];
  const localHasVideo = Boolean(input.localStream?.getVideoTracks().length);

  if (localHasVideo && input.localStream) {
    items.push({
      id: `local:${input.session.participant.id}`,
      participantId: input.session.participant.id,
      role: input.session.participant.role,
      label: input.session.participant.nickname,
      stream: input.localStream,
      variant: input.screenSharing ? "screen" : "camera",
      isLocal: true,
      micEnabled: input.localMicEnabled,
      meta: `${input.session.participant.role} · ${input.screenSharing ? "共享屏幕中" : "本地视频"}`
    });
  }

  for (const tile of input.remoteTiles) {
    if (!tile.stream.getVideoTracks().length) {
      continue;
    }

    const participant = participantsById.get(tile.participantId);
    if (!participant) {
      continue;
    }

    items.push({
      id: `remote:${participant.id}`,
      participantId: participant.id,
      role: participant.role,
      label: participant.nickname,
      stream: tile.stream,
      variant: "camera",
      isLocal: false,
      micEnabled:
        participant.effectiveMediaState.microphoneEnabled ||
        participant.requestedMediaPreference.microphoneEnabled,
      meta: `${participant.role} · ${tile.connectionState}`
    });
  }

  return items.sort((left, right) => {
    if (left.participantId === input.session?.meeting.hostParticipantId) {
      return -1;
    }
    if (right.participantId === input.session?.meeting.hostParticipantId) {
      return 1;
    }

    const leftJoinedAt = Date.parse(participantsById.get(left.participantId)?.joinedAt ?? "");
    const rightJoinedAt = Date.parse(participantsById.get(right.participantId)?.joinedAt ?? "");
    return leftJoinedAt - rightJoinedAt;
  });
}

function chooseFeaturedStageId(
  items: StageItem[],
  currentFeaturedStageId: string | null,
  hostParticipantId: string | undefined
) {
  if (currentFeaturedStageId && items.some((item) => item.id === currentFeaturedStageId)) {
    return currentFeaturedStageId;
  }

  const hostItem = items.find((item) => item.participantId === hostParticipantId);
  return hostItem?.id ?? items[0]?.id ?? null;
}

function buildInviteText(
  meeting: Meeting,
  password: string,
  inviterNickname: string
) {
  const joinURL = buildMeetingJoinURL(meeting);
  const meetingNumber = formatMeetingNumberDisplay(getMeetingPublicNumber(meeting));

  const content = [
    `${inviterNickname} 邀请您参加 meeting 快速会议`,
    `会议主题：${meeting.title}`,
    `会议时间：${formatInviteMeetingTime(meeting.createdAt)}`,
    "",
    "点击链接直接加入会议：",
    joinURL.toString(),
    `会议号：${meetingNumber}`
  ];

  if (meeting.passwordRequired) {
    content.push(`会议密码：${formatMeetingPassword(meeting, password)}`);
  }

  return content.join("\n");
}

function buildMeetingJoinURL(meeting: Meeting) {
  const joinURL = new URL(window.location.origin);
  joinURL.searchParams.set("meetingNumber", getMeetingPublicNumber(meeting));
  return joinURL;
}

function buildMeetingQRCodePayload(meeting: Meeting, password: string) {
  const joinURL = buildMeetingJoinURL(meeting);
  joinURL.searchParams.set("joinCode", meeting.joinCode);
  if (meeting.passwordRequired && password) {
    joinURL.searchParams.set("password", password);
  }
  return joinURL.toString();
}

function parseMeetingQRCodePayload(payloadText: string) {
  const trimmed = payloadText.trim();
  if (!trimmed) {
    return {
      meetingId: "",
      password: ""
    };
  }

  try {
    const url = new URL(trimmed);
    return {
      meetingId: normalizeMeetingLookupValue(
        url.searchParams.get("meetingNumber") ?? url.searchParams.get("meetingId") ?? ""
      ),
      password: url.searchParams.get("password") ?? ""
    };
  } catch {
    return {
      meetingId: normalizeMeetingLookupValue(trimmed),
      password: ""
    };
  }
}

function getMeetingPublicNumber(meeting: Pick<Meeting, "meetingNumber" | "id">) {
  return meeting.meetingNumber || meeting.id;
}

function normalizeMeetingLookupValue(value: string) {
  const trimmed = value.trim();
  if (!trimmed) {
    return "";
  }

  const meetingNumber = normalizeMeetingNumberValue(trimmed);
  if (/^\d{9}$/.test(meetingNumber)) {
    return meetingNumber;
  }

  return trimmed;
}

function normalizeMeetingNumberValue(value: string) {
  const withoutHyphen = value.replaceAll("-", "");
  return withoutHyphen.replace(/\s+/g, "");
}

function formatMeetingNumberDisplay(value: string) {
  const meetingNumber = normalizeMeetingNumberValue(value);
  if (!/^\d{9}$/.test(meetingNumber)) {
    return value.trim();
  }

  return `${meetingNumber.slice(0, 3)} ${meetingNumber.slice(3, 6)} ${meetingNumber.slice(6, 9)}`;
}

function getMediaDevices() {
  if (typeof navigator === "undefined") {
    return null;
  }

  return navigator.mediaDevices ?? null;
}

function supportsUserMediaCapture() {
  return Boolean(typeof window !== "undefined" && window.isSecureContext && getMediaDevices()?.getUserMedia);
}

function supportsDisplayCapture() {
  return Boolean(typeof window !== "undefined" && window.isSecureContext && getMediaDevices()?.getDisplayMedia);
}

function supportsLiveJoinScanner() {
  return supportsUserMediaCapture();
}

function describeUserMediaError(error: unknown) {
  if (!supportsUserMediaCapture()) {
    return "当前页面无法直接打开摄像头或麦克风。请优先通过 HTTPS 或 localhost 访问，再重试。";
  }

  if (error instanceof DOMException) {
    if (error.name === "NotAllowedError") {
      return "浏览器未授予摄像头或麦克风权限，请允许访问后重试。";
    }
    if (error.name === "NotFoundError") {
      return "未检测到可用的摄像头或麦克风，请检查设备后重试。";
    }
    if (error.name === "NotReadableError") {
      return "摄像头或麦克风当前不可用，可能被其他应用占用，请稍后重试。";
    }
  }

  return asMessage(error);
}

function describeDisplayCaptureError(error: unknown) {
  if (!supportsDisplayCapture()) {
    return "当前页面无法直接发起屏幕共享。请改用支持该能力的桌面浏览器，并优先通过 HTTPS 或 localhost 访问。";
  }

  if (error instanceof DOMException) {
    if (error.name === "NotAllowedError") {
      return "你已取消屏幕共享，或浏览器未授予共享权限。";
    }
    if (error.name === "NotReadableError") {
      return "屏幕共享当前不可用，可能被系统或其他应用占用，请稍后重试。";
    }
  }

  return asMessage(error);
}

function describeJoinScannerError(error: unknown) {
  if (!supportsUserMediaCapture()) {
    return "当前浏览器环境未提供摄像头扫码能力。局域网 HTTP 页面在移动端通常需要 HTTPS，或改用拍照/选图识别。";
  }

  if (error instanceof DOMException) {
    if (error.name === "NotAllowedError") {
      return "浏览器未授予摄像头权限，请允许访问摄像头后重试，或改用拍照/选图识别。";
    }
    if (error.name === "NotFoundError") {
      return "未检测到可用摄像头，请改用拍照/选图识别。";
    }
    if (error.name === "NotReadableError") {
      return "摄像头当前不可用，可能被其他应用占用，请稍后重试。";
    }
  }

  return asMessage(error);
}

async function decodeQRCodeFromFile(file: File) {
  const imageUrl = URL.createObjectURL(file);
  try {
    const image = await loadImageElement(imageUrl);
    const canvas = document.createElement("canvas");
    const context = canvas.getContext("2d");
    if (!context) {
      throw new Error("图片识别画布初始化失败");
    }

    const sourceWidth = image.naturalWidth || image.width;
    const sourceHeight = image.naturalHeight || image.height;
    const scales = [1, 1600 / Math.max(sourceWidth, sourceHeight), 1200 / Math.max(sourceWidth, sourceHeight), 800 / Math.max(sourceWidth, sourceHeight)]
      .filter((scale) => Number.isFinite(scale) && scale > 0)
      .map((scale) => Math.min(1, scale));

    for (const scale of Array.from(new Set(scales))) {
      canvas.width = Math.max(1, Math.round(sourceWidth * scale));
      canvas.height = Math.max(1, Math.round(sourceHeight * scale));
      context.clearRect(0, 0, canvas.width, canvas.height);
      context.drawImage(image, 0, 0, canvas.width, canvas.height);
      const imageData = context.getImageData(0, 0, canvas.width, canvas.height);
      const result = jsQR(imageData.data, imageData.width, imageData.height);
      if (result?.data) {
        return result.data;
      }
    }

    throw new Error("未识别到有效的会议二维码，请让二维码更清晰、更靠近镜头，或改用二维码截图再试");
  } finally {
    URL.revokeObjectURL(imageUrl);
  }
}

function loadImageElement(url: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image();
    image.onload = () => resolve(image);
    image.onerror = () => reject(new Error("二维码图片加载失败"));
    image.src = url;
  });
}

function formatMeetingPassword(meeting: Meeting, password: string) {
  if (!meeting.passwordRequired) {
    return "无需密码";
  }
  return password || "未记录";
}

function describeReturnAfterMeeting(entryView: EntryView) {
  return entryView === "schedule" ? "预约会议" : "快速会议";
}

async function copyText(text: string) {
  if (!navigator.clipboard) {
    throw new Error("当前浏览器不支持剪贴板写入");
  }

  await navigator.clipboard.writeText(text);
}

async function copyImageDataURL(dataURL: string) {
  if (!navigator.clipboard?.write || typeof ClipboardItem === "undefined") {
    throw new Error("当前浏览器不支持图片写入剪贴板");
  }

  const response = await fetch(dataURL);
  if (!response.ok) {
    throw new Error("读取会议二维码失败");
  }

  const blob = await response.blob();
  const type = blob.type || "image/png";
  await navigator.clipboard.write([new ClipboardItem({ [type]: blob })]);
}

function formatInviteMeetingTime(createdAt: string) {
  const start = new Date(createdAt);
  const end = new Date();
  const datePart = [
    start.getFullYear(),
    `${start.getMonth() + 1}`.padStart(2, "0"),
    `${start.getDate()}`.padStart(2, "0")
  ].join("/");
  const startTime = `${start.getHours().toString().padStart(2, "0")}:${start.getMinutes().toString().padStart(2, "0")}`;
  const endTime = `${end.getHours().toString().padStart(2, "0")}:${end.getMinutes().toString().padStart(2, "0")}`;
  return `${datePart} ${startTime}–${endTime} (${formatGMTOffset(start)})`;
}

function formatGMTOffset(date: Date) {
  const offsetMinutes = -date.getTimezoneOffset();
  const sign = offsetMinutes >= 0 ? "+" : "-";
  const absoluteMinutes = Math.abs(offsetMinutes);
  const hours = Math.floor(absoluteMinutes / 60)
    .toString()
    .padStart(2, "0");
  const minutes = (absoluteMinutes % 60).toString().padStart(2, "0");
  return `GMT${sign}${hours}:${minutes}`;
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) {
    return `${bytes} B`;
  }

  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`;
  }

  return `${(bytes / (1024 * 1024)).toFixed(2)} MB`;
}

function formatDuration(durationMs: number): string {
  const seconds = Math.max(1, Math.round(durationMs / 1000));
  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = seconds % 60;
  return `${minutes.toString().padStart(2, "0")}:${remainingSeconds.toString().padStart(2, "0")}`;
}

function buildMinutesFileName(title: string): string {
  const safeTitle = title.trim().replace(/[^\w\u4e00-\u9fa5-]+/g, "_") || "meeting";
  const timestamp = new Date().toISOString().replace(/[:.]/g, "-");
  return `${safeTitle}_minutes_${timestamp}.txt`;
}

function summarizeReadyCheckRound(round: ReadyCheckRound): string {
  const counts = {
    confirmed: 0,
    cancelled: 0,
    timeout: 0
  };

  for (const result of Object.values(round.results)) {
    if (result.status === "confirmed") {
      counts.confirmed += 1;
      continue;
    }
    if (result.status === "cancelled") {
      counts.cancelled += 1;
      continue;
    }
    if (result.status === "timeout") {
      counts.timeout += 1;
    }
  }

  return `confirmed=${counts.confirmed}, cancelled=${counts.cancelled}, timeout=${counts.timeout}`;
}

function aggregatePeerStats(snapshots: PeerStatsSnapshot[], baseline: Map<string, AuditCounter>) {
  const now = Date.now();
  let latencyTotal = 0;
  let latencyCount = 0;
  let packetsLost = 0;
  let packetsTotal = 0;
  let fpsSamples = 0;
  let fpsTotal = 0;
  let bitrateTotal = 0;
  let bitrateCount = 0;

  const nextBaseline = new Map<string, AuditCounter>();

  for (const snapshot of snapshots) {
    if (snapshot.roundTripTimeMs !== null) {
      latencyTotal += snapshot.roundTripTimeMs;
      latencyCount += 1;
    }

    packetsLost += snapshot.packetsLost;
    packetsTotal += snapshot.packetsTotal;

    for (const fps of snapshot.framesPerSecond) {
      fpsTotal += fps;
      fpsSamples += 1;
    }

    const totalBytes = snapshot.bytesSent + snapshot.bytesReceived;
    const previous = baseline.get(snapshot.participantId);
    if (previous && now > previous.timestamp && totalBytes >= previous.bytes) {
      const durationSeconds = (now - previous.timestamp) / 1000;
      const bitrateKbps = ((totalBytes - previous.bytes) * 8) / durationSeconds / 1000;
      bitrateTotal += bitrateKbps;
      bitrateCount += 1;
    } else if (snapshot.availableOutgoingBitrateKbps !== null) {
      bitrateTotal += snapshot.availableOutgoingBitrateKbps;
      bitrateCount += 1;
    }

    nextBaseline.set(snapshot.participantId, {
      timestamp: now,
      bytes: totalBytes
    });
  }

  baseline.clear();
  for (const [participantId, counter] of nextBaseline) {
    baseline.set(participantId, counter);
  }

  return {
    latencyMs: latencyCount === 0 ? 0 : latencyTotal / latencyCount,
    packetLossRate: packetsTotal === 0 ? 0 : packetsLost / packetsTotal,
    averageFps: fpsSamples === 0 ? 0 : fpsTotal / fpsSamples,
    averageBitrateKbps: bitrateCount === 0 ? 0 : bitrateTotal / bitrateCount,
    peerCount: snapshots.length,
    perPeer: snapshots.map((snapshot) => ({
      participantId: snapshot.participantId,
      connectionState: snapshot.connectionState,
      roundTripTimeMs: snapshot.roundTripTimeMs,
      packetsLost: snapshot.packetsLost,
      packetsTotal: snapshot.packetsTotal,
      framesPerSecond: snapshot.framesPerSecond,
      bytesSent: snapshot.bytesSent,
      bytesReceived: snapshot.bytesReceived,
      availableOutgoingBitrateKbps: snapshot.availableOutgoingBitrateKbps,
      localCandidateType: snapshot.localCandidateType,
      remoteCandidateType: snapshot.remoteCandidateType
    }))
  };
}

function asMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return "发生未知错误";
}

function readPersistedAppState(): PersistedAppState | null {
  if (typeof window === "undefined") {
    return null;
  }

  try {
    const raw = window.sessionStorage.getItem(appStateStorageKey);
    if (!raw) {
      return null;
    }

    const parsed = JSON.parse(raw) as Partial<PersistedAppState>;
    const defaultScheduleForm = buildDefaultScheduleForm();
    return {
      isAuthenticated: parsed.isAuthenticated === true,
      entryView: normalizeEntryView(parsed.entryView, parsed.isAuthenticated ? "home" : "login"),
      loginForm: {
        email: parsed.loginForm?.email ?? defaultLoginForm.email,
        password: parsed.loginForm?.password ?? defaultLoginForm.password
      },
      scheduleForm: {
        title: parsed.scheduleForm?.title ?? defaultScheduleForm.title,
        scheduledAt: parsed.scheduleForm?.scheduledAt ?? defaultScheduleForm.scheduledAt,
        timezone: parsed.scheduleForm?.timezone ?? defaultScheduleForm.timezone,
        password: parsed.scheduleForm?.password ?? defaultScheduleForm.password
      },
      joinForm: {
        meetingId: parsed.joinForm?.meetingId ?? defaultJoinForm.meetingId,
        password: parsed.joinForm?.password ?? defaultJoinForm.password,
        nickname: parsed.joinForm?.nickname ?? defaultJoinForm.nickname,
        requestCameraEnabled:
          parsed.joinForm?.requestCameraEnabled ?? defaultJoinForm.requestCameraEnabled,
        requestMicrophoneEnabled:
          parsed.joinForm?.requestMicrophoneEnabled ?? defaultJoinForm.requestMicrophoneEnabled
      },
      meetingAccessPassword: parsed.meetingAccessPassword ?? "",
      meetingSession: isPersistedSessionState(parsed.meetingSession) ? parsed.meetingSession : null,
      returnAfterMeetingView: parsed.returnAfterMeetingView === "schedule" ? "schedule" : "home"
    };
  } catch {
    return null;
  }
}

function writePersistedAppState(state: PersistedAppState) {
  if (typeof window === "undefined") {
    return;
  }

  try {
    window.sessionStorage.setItem(appStateStorageKey, JSON.stringify(state));
  } catch {
    // Ignore session storage write failures and fall back to in-memory state only.
  }
}

function normalizeEntryView(value: EntryView | undefined, fallback: EntryView): EntryView {
  if (value === "login" || value === "home" || value === "schedule" || value === "join") {
    return value;
  }
  return fallback;
}

function isPersistedSessionState(value: unknown): value is SessionState {
  if (!value || typeof value !== "object") {
    return false;
  }

  const candidate = value as Partial<SessionState>;
  return Boolean(
    candidate.meeting &&
      typeof candidate.meeting === "object" &&
      candidate.participant &&
      typeof candidate.participant === "object"
  );
}

export default App;
