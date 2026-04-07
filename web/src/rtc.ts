import { resolveIceServers } from "./runtime-config";

type MeshCallbacks = {
  onRemoteStream: (participantId: string, stream: MediaStream) => void;
  onRemoteStreamRemoved: (participantId: string) => void;
  onPeerStateChange: (participantId: string, state: RTCPeerConnectionState) => void;
  onError: (message: string) => void;
};

type SendSignal = (
  type: "signal.offer" | "signal.answer" | "signal.ice_candidate",
  payload: {
    targetParticipantId: string;
    data: RTCSessionDescriptionInit | RTCIceCandidateInit;
  }
) => void;

type PeerState = {
  participantId: string;
  pc: RTCPeerConnection;
  remoteStream: MediaStream;
  pendingCandidates: RTCIceCandidateInit[];
  makingOffer: boolean;
  ignoreOffer: boolean;
  polite: boolean;
  isSettingRemoteAnswerPending: boolean;
  iceRestartRequested: boolean;
  iceRestartAttempts: number;
  iceRestartTimer: number | null;
};

export type PeerStatsSnapshot = {
  participantId: string;
  connectionState: RTCPeerConnectionState;
  roundTripTimeMs: number | null;
  packetsLost: number;
  packetsTotal: number;
  framesPerSecond: number[];
  bytesSent: number;
  bytesReceived: number;
  availableOutgoingBitrateKbps: number | null;
  localCandidateType: string | null;
  remoteCandidateType: string | null;
};

export type MediaQualityProfile = "stable" | "balanced" | "conservative";

export type MediaQualityPolicy = {
  profile: MediaQualityProfile;
  maxVideoBitrateKbps: number;
  maxVideoFramerate: number;
};

type CandidateStatsLike = {
  candidateType?: string;
};

const defaultRTCConfiguration: RTCConfiguration = {
  iceServers: resolveIceServers()
};

export class PeerMesh {
  private readonly localParticipantId: string;
  private readonly sendSignal: SendSignal;
  private readonly callbacks: MeshCallbacks;
  private readonly peers = new Map<string, PeerState>();
  private localStream: MediaStream | null = null;

  constructor(localParticipantId: string, sendSignal: SendSignal, callbacks: MeshCallbacks) {
    this.localParticipantId = localParticipantId;
    this.sendSignal = sendSignal;
    this.callbacks = callbacks;
  }

  async setLocalStream(stream: MediaStream | null): Promise<void> {
    this.localStream = stream;

    for (const peer of this.peers.values()) {
      await this.syncPeerTracks(peer);
    }
  }

  async syncParticipants(participantIds: string[]): Promise<void> {
    const nextIDs = new Set(participantIds.filter((id) => id !== this.localParticipantId));

    for (const existingId of this.peers.keys()) {
      if (!nextIDs.has(existingId)) {
        this.removePeer(existingId);
      }
    }

    for (const participantId of nextIDs) {
      await this.ensurePeer(participantId);
    }
  }

  async handleSignal(
    type: "signal.offer" | "signal.answer" | "signal.ice_candidate",
    fromParticipantId: string,
    data: RTCSessionDescriptionInit | RTCIceCandidateInit
  ): Promise<void> {
    const peer = await this.ensurePeer(fromParticipantId);

    switch (type) {
      case "signal.offer":
      case "signal.answer":
        await this.handleDescription(peer, data as RTCSessionDescriptionInit);
        return;
      case "signal.ice_candidate":
        await this.handleCandidate(peer, data as RTCIceCandidateInit);
        return;
      default:
        this.callbacks.onError(`收到未知的信令类型: ${String(type)}`);
    }
  }

  removePeer(participantId: string): void {
    const peer = this.peers.get(participantId);
    if (!peer) {
      return;
    }

    peer.pc.close();
    this.peers.delete(participantId);
    this.callbacks.onRemoteStreamRemoved(participantId);
  }

  close(): void {
    for (const participantId of this.peers.keys()) {
      this.removePeer(participantId);
    }
  }

  async collectStats(): Promise<PeerStatsSnapshot[]> {
    const snapshots: PeerStatsSnapshot[] = [];

    for (const peer of this.peers.values()) {
      snapshots.push(await this.collectPeerStats(peer));
    }

    return snapshots;
  }

  async applyMediaPolicy(policy: MediaQualityPolicy): Promise<void> {
    for (const peer of this.peers.values()) {
      await this.applyPeerMediaPolicy(peer, policy);
    }
  }

  private async ensurePeer(participantId: string): Promise<PeerState> {
    const existing = this.peers.get(participantId);
    if (existing) {
      return existing;
    }

    const pc = new RTCPeerConnection(defaultRTCConfiguration);
    const remoteStream = new MediaStream();
    const peer: PeerState = {
      participantId,
      pc,
      remoteStream,
      pendingCandidates: [],
      makingOffer: false,
      ignoreOffer: false,
      polite: this.localParticipantId.localeCompare(participantId) > 0,
      isSettingRemoteAnswerPending: false,
      iceRestartRequested: false,
      iceRestartAttempts: 0,
      iceRestartTimer: null
    };

    pc.onicecandidate = (event) => {
      if (!event.candidate) {
        return;
      }

      this.sendSignal("signal.ice_candidate", {
        targetParticipantId: participantId,
        data: event.candidate.toJSON()
      });
    };

    pc.ontrack = (event) => {
      for (const track of event.streams[0]?.getTracks() ?? [event.track]) {
        const exists = remoteStream.getTracks().some((currentTrack) => currentTrack.id === track.id);
        if (!exists) {
          remoteStream.addTrack(track);
        }
      }
      this.callbacks.onRemoteStream(participantId, remoteStream);
    };

    pc.onconnectionstatechange = () => {
      this.callbacks.onPeerStateChange(participantId, pc.connectionState);
      if (pc.connectionState === "connected") {
        this.clearPeerRecovery(peer);
        return;
      }

      if (pc.connectionState === "closed") {
        this.clearPeerRecovery(peer);
        this.removePeer(participantId);
        return;
      }

      if (pc.connectionState === "failed" || pc.connectionState === "disconnected") {
        this.schedulePeerRecovery(peer);
      }
    };

    pc.oniceconnectionstatechange = () => {
      if (pc.iceConnectionState === "connected" || pc.iceConnectionState === "completed") {
        this.clearPeerRecovery(peer);
        return;
      }

      if (pc.iceConnectionState === "failed" || pc.iceConnectionState === "disconnected") {
        this.schedulePeerRecovery(peer);
      }
    };

    pc.onnegotiationneeded = () => {
      this.handleNegotiationNeeded(peer).catch((error) => {
        this.callbacks.onError(asMessage(error, `与 ${participantId} 协商失败`));
      });
    };

    this.peers.set(participantId, peer);
    await this.syncPeerTracks(peer);
    return peer;
  }

  private clearPeerRecovery(peer: PeerState): void {
    if (peer.iceRestartTimer !== null) {
      window.clearTimeout(peer.iceRestartTimer);
      peer.iceRestartTimer = null;
    }
    peer.iceRestartRequested = false;
    peer.iceRestartAttempts = 0;
  }

  private schedulePeerRecovery(peer: PeerState): void {
    if (
      peer.pc.connectionState === "closed" ||
      peer.pc.signalingState === "closed" ||
      peer.iceRestartTimer !== null ||
      peer.iceRestartRequested
    ) {
      return;
    }

    const delayMs =
      peer.pc.iceConnectionState === "failed"
        ? 0
        : Math.min(10000, 1000 * 2 ** peer.iceRestartAttempts);
    peer.iceRestartAttempts += 1;
    peer.iceRestartTimer = window.setTimeout(() => {
      peer.iceRestartTimer = null;
      if (
        peer.pc.connectionState === "closed" ||
        peer.pc.signalingState === "closed" ||
        peer.iceRestartRequested
      ) {
        return;
      }

      peer.iceRestartRequested = true;
      try {
        if (typeof peer.pc.restartIce === "function") {
          peer.pc.restartIce();
          return;
        }

        void this.handleNegotiationNeeded(peer, true);
      } catch (error) {
        peer.iceRestartRequested = false;
        this.callbacks.onError(
          asMessage(error, `无法重启与 ${peer.participantId} 的 ICE 连接`)
        );
      }
    }, delayMs);
  }

  private async syncPeerTracks(peer: PeerState): Promise<void> {
    const localStream = this.localStream;
    const desiredTracks = localStream?.getTracks() ?? [];
    const desiredByKind = new Map(desiredTracks.map((track) => [track.kind, track]));
    const boundKinds = new Set<string>();

    for (const sender of peer.pc.getSenders()) {
      const currentTrack = sender.track;
      if (!currentTrack) {
        continue;
      }

      const replacement = desiredByKind.get(currentTrack.kind);
      if (!replacement) {
        peer.pc.removeTrack(sender);
        continue;
      }

      if (replacement.id !== currentTrack.id) {
        await sender.replaceTrack(replacement);
      }
      boundKinds.add(replacement.kind);
    }

    if (!localStream) {
      return;
    }

    for (const track of desiredTracks) {
      if (!boundKinds.has(track.kind)) {
        peer.pc.addTrack(track, localStream);
      }
    }
  }

  private async handleNegotiationNeeded(peer: PeerState, iceRestart = false): Promise<void> {
    if (peer.pc.signalingState === "closed") {
      return;
    }

    peer.makingOffer = true;
    const useIceRestart = iceRestart || peer.iceRestartRequested;
    try {
      const offer = await peer.pc.createOffer(useIceRestart ? { iceRestart: true } : undefined);
      await peer.pc.setLocalDescription(offer);

      if (!peer.pc.localDescription) {
        throw new Error("本地 offer 为空");
      }

      this.sendSignal("signal.offer", {
        targetParticipantId: peer.participantId,
        data: {
          type: peer.pc.localDescription.type,
          sdp: peer.pc.localDescription.sdp ?? ""
        }
      });
    } finally {
      peer.makingOffer = false;
      if (useIceRestart) {
        peer.iceRestartRequested = false;
      }
    }
  }

  private async handleDescription(peer: PeerState, description: RTCSessionDescriptionInit): Promise<void> {
    const readyForOffer =
      !peer.makingOffer &&
      (peer.pc.signalingState === "stable" || peer.isSettingRemoteAnswerPending);
    const offerCollision = description.type === "offer" && !readyForOffer;

    peer.ignoreOffer = !peer.polite && offerCollision;
    if (peer.ignoreOffer) {
      return;
    }

    peer.isSettingRemoteAnswerPending = description.type === "answer";
    await peer.pc.setRemoteDescription(description);
    peer.isSettingRemoteAnswerPending = false;
    await this.flushPendingCandidates(peer);

    if (description.type !== "offer") {
      return;
    }

    await this.syncPeerTracks(peer);
    const answer = await peer.pc.createAnswer();
    await peer.pc.setLocalDescription(answer);

    if (!peer.pc.localDescription) {
      throw new Error("本地 answer 为空");
    }

    this.sendSignal("signal.answer", {
      targetParticipantId: peer.participantId,
      data: {
        type: peer.pc.localDescription.type,
        sdp: peer.pc.localDescription.sdp ?? ""
      }
    });
  }

  private async handleCandidate(peer: PeerState, candidate: RTCIceCandidateInit): Promise<void> {
    if (!peer.pc.remoteDescription) {
      peer.pendingCandidates.push(candidate);
      return;
    }

    await peer.pc.addIceCandidate(candidate);
  }

  private async flushPendingCandidates(peer: PeerState): Promise<void> {
    while (peer.pendingCandidates.length > 0) {
      const candidate = peer.pendingCandidates.shift();
      if (!candidate) {
        continue;
      }
      await peer.pc.addIceCandidate(candidate);
    }
  }

  private async collectPeerStats(peer: PeerState): Promise<PeerStatsSnapshot> {
    const stats = await peer.pc.getStats();
    let roundTripTimeMs: number | null = null;
    let packetsLost = 0;
    let packetsTotal = 0;
    const framesPerSecond: number[] = [];
    let bytesSent = 0;
    let bytesReceived = 0;
    let availableOutgoingBitrateKbps: number | null = null;
    let localCandidateType: string | null = null;
    let remoteCandidateType: string | null = null;

    let selectedLocalCandidateId = "";
    let selectedRemoteCandidateId = "";

    stats.forEach((report) => {
      if (report.type === "candidate-pair") {
        const candidatePair = report as RTCIceCandidatePairStats;
        const selected = Boolean((candidatePair as RTCIceCandidatePairStats & { selected?: boolean }).selected) ||
          Boolean(candidatePair.nominated);
        if (!selected) {
          return;
        }

        if (typeof candidatePair.currentRoundTripTime === "number") {
          roundTripTimeMs = candidatePair.currentRoundTripTime * 1000;
        }
        if (typeof candidatePair.availableOutgoingBitrate === "number") {
          availableOutgoingBitrateKbps = candidatePair.availableOutgoingBitrate / 1000;
        }
        selectedLocalCandidateId = candidatePair.localCandidateId ?? selectedLocalCandidateId;
        selectedRemoteCandidateId = candidatePair.remoteCandidateId ?? selectedRemoteCandidateId;
        return;
      }

      if (report.type === "local-candidate" && report.id === selectedLocalCandidateId) {
        const candidate = report as CandidateStatsLike;
        localCandidateType = candidate.candidateType ?? null;
        return;
      }

      if (report.type === "remote-candidate" && report.id === selectedRemoteCandidateId) {
        const candidate = report as CandidateStatsLike;
        remoteCandidateType = candidate.candidateType ?? null;
        return;
      }

      if (report.type !== "inbound-rtp" && report.type !== "outbound-rtp") {
        return;
      }

      const mediaReport = report as RTCInboundRtpStreamStats & RTCOutboundRtpStreamStats;
      if (typeof mediaReport.bytesSent === "number") {
        bytesSent += mediaReport.bytesSent;
      }
      if (typeof mediaReport.bytesReceived === "number") {
        bytesReceived += mediaReport.bytesReceived;
      }
      if (typeof mediaReport.framesPerSecond === "number" && mediaReport.framesPerSecond > 0) {
        framesPerSecond.push(mediaReport.framesPerSecond);
      }
      if (typeof mediaReport.packetsLost === "number") {
        packetsLost += mediaReport.packetsLost;
      }

      if (typeof mediaReport.packetsReceived === "number") {
        packetsTotal += mediaReport.packetsReceived;
      } else if (typeof mediaReport.packetsSent === "number") {
        packetsTotal += mediaReport.packetsSent;
      }
    });

    packetsTotal += packetsLost;

    return {
      participantId: peer.participantId,
      connectionState: peer.pc.connectionState,
      roundTripTimeMs,
      packetsLost,
      packetsTotal,
      framesPerSecond,
      bytesSent,
      bytesReceived,
      availableOutgoingBitrateKbps,
      localCandidateType,
      remoteCandidateType
    };
  }

  private async applyPeerMediaPolicy(peer: PeerState, policy: MediaQualityPolicy): Promise<void> {
    const senders = peer.pc.getSenders().filter((sender) => sender.track?.kind === "video");
    if (senders.length === 0) {
      return;
    }

    for (const sender of senders) {
      const parameters = sender.getParameters();
      const nextEncodings =
        parameters.encodings.length > 0
          ? parameters.encodings.map((encoding) => ({
              ...encoding,
              maxBitrate: policy.maxVideoBitrateKbps * 1000,
              maxFramerate: policy.maxVideoFramerate
            }))
          : [
              {
                maxBitrate: policy.maxVideoBitrateKbps * 1000,
                maxFramerate: policy.maxVideoFramerate
              }
            ];

      try {
        await sender.setParameters({
          ...parameters,
          encodings: nextEncodings
        });
      } catch (error) {
        this.callbacks.onError(
          asMessage(error, `无法应用视频策略 ${policy.profile} 到 ${peer.participantId}`)
        );
      }
    }
  }
}

function asMessage(error: unknown, fallback: string): string {
  if (error instanceof Error) {
    return `${fallback}: ${error.message}`;
  }

  return fallback;
}
