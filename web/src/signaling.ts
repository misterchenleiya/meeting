import { resolveSignalUrl } from "./runtime-config";

export type SignalEnvelope = {
  type: string;
  payload?: unknown;
};

type SignalClientOptions = {
  onOpen?: () => void;
  onClose?: () => void;
  onError?: (message: string) => void;
  onMessage?: (event: SignalEnvelope) => void;
};

export class SignalClient {
  private socket: WebSocket | null = null;

  connect(meetingId: string, participantId: string, options: SignalClientOptions): void {
    this.close();

    const url = resolveSignalUrl(`/ws/meetings/${meetingId}`);
    url.searchParams.set("participantId", participantId);

    const socket = new WebSocket(url.toString());
    this.socket = socket;

    socket.addEventListener("open", () => {
      if (this.socket !== socket) {
        return;
      }
      options.onOpen?.();
    });

    socket.addEventListener("close", () => {
      if (this.socket !== socket) {
        return;
      }
      this.socket = null;
      options.onClose?.();
    });

    socket.addEventListener("error", () => {
      if (this.socket !== socket) {
        return;
      }
      options.onError?.("WebSocket 连接失败");
    });

    socket.addEventListener("message", (messageEvent) => {
      if (this.socket !== socket) {
        return;
      }
      try {
        const event = JSON.parse(messageEvent.data) as SignalEnvelope;
        options.onMessage?.(event);
      } catch {
        options.onError?.("收到无法解析的信令消息");
      }
    });
  }

  send(type: string, payload?: unknown): void {
    if (!this.socket || this.socket.readyState !== WebSocket.OPEN) {
      throw new Error("WebSocket 尚未连接");
    }

    this.socket.send(JSON.stringify({ type, payload }));
  }

  close(): void {
    const socket = this.socket;
    this.socket = null;
    if (socket) {
      socket.close();
    }
  }
}
