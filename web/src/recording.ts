export type RecordingKind = "meeting_video" | "audio_only";

export type RecordingAsset = {
  kind: RecordingKind;
  blob: Blob;
  objectUrl: string;
  mimeType: string;
  fileName: string;
  sizeBytes: number;
  durationMs: number;
  createdAt: string;
};

type RecordingSource = {
  title: string;
  kind: RecordingKind;
  localStream: MediaStream | null;
  remoteStreams: MediaStream[];
};

type SaveFilePickerWindow = Window & {
  showSaveFilePicker?: (options?: {
    suggestedName?: string;
    types?: Array<{
      description?: string;
      accept: Record<string, string[]>;
    }>;
  }) => Promise<{
    createWritable: () => Promise<{
      write: (data: Blob) => Promise<void>;
      close: () => Promise<void>;
    }>;
  }>;
};

export class LocalRecordingSession {
  private readonly recorder: MediaRecorder;
  private readonly cleanup: () => void;
  private readonly startedAt: number;
  private readonly createdAt: string;
  private readonly kind: RecordingKind;
  private readonly mimeType: string;
  private readonly title: string;
  private readonly chunks: Blob[] = [];
  private stopPromise: Promise<RecordingAsset> | null = null;

  constructor(
    recorder: MediaRecorder,
    cleanup: () => void,
    kind: RecordingKind,
    mimeType: string,
    title: string
  ) {
    this.recorder = recorder;
    this.cleanup = cleanup;
    this.kind = kind;
    this.mimeType = mimeType;
    this.title = title;
    this.startedAt = Date.now();
    this.createdAt = new Date().toISOString();

    recorder.ondataavailable = (event) => {
      if (event.data.size > 0) {
        this.chunks.push(event.data);
      }
    };
  }

  start(): void {
    this.recorder.start(1000);
  }

  async stop(): Promise<RecordingAsset> {
    if (this.stopPromise) {
      return this.stopPromise;
    }

    this.stopPromise = new Promise<RecordingAsset>((resolve, reject) => {
      this.recorder.onerror = () => {
        this.cleanup();
        reject(new Error("录制过程中发生错误"));
      };

      this.recorder.onstop = () => {
        const blob = new Blob(this.chunks, { type: this.mimeType });
        const objectUrl = URL.createObjectURL(blob);
        this.cleanup();
        resolve({
          kind: this.kind,
          blob,
          objectUrl,
          mimeType: this.mimeType,
          fileName: buildFileName(this.title, this.kind),
          sizeBytes: blob.size,
          durationMs: Date.now() - this.startedAt,
          createdAt: this.createdAt
        });
      };

      this.recorder.stop();
    });

    return this.stopPromise;
  }

  cancel(): void {
    if (this.recorder.state !== "inactive") {
      this.recorder.stop();
    }
    this.cleanup();
  }
}

export async function startLocalRecording(source: RecordingSource): Promise<LocalRecordingSession> {
  if (typeof MediaRecorder === "undefined") {
    throw new Error("当前浏览器不支持 MediaRecorder");
  }

  const mimeType = selectMimeType(source.kind);
  const videos = await prepareVideoElements(source.localStream, source.remoteStreams);
  const audioContext = new AudioContext();
  const audioDestination = audioContext.createMediaStreamDestination();
  const audioNodes: MediaStreamAudioSourceNode[] = [];

  for (const stream of [source.localStream, ...source.remoteStreams]) {
    if (!stream || stream.getAudioTracks().length === 0) {
      continue;
    }

    const audioNode = audioContext.createMediaStreamSource(stream);
    audioNode.connect(audioDestination);
    audioNodes.push(audioNode);
  }

  let cleanup = () => {
    for (const video of videos) {
      video.pause();
      video.srcObject = null;
    }
    for (const node of audioNodes) {
      node.disconnect();
    }
    void audioContext.close();
  };

  if (source.kind === "audio_only") {
    const recorder = new MediaRecorder(audioDestination.stream, { mimeType });
    return new LocalRecordingSession(recorder, cleanup, source.kind, mimeType, source.title);
  }

  const canvas = document.createElement("canvas");
  canvas.width = 1280;
  canvas.height = 720;
  const context = canvas.getContext("2d");
  if (!context) {
    cleanup();
    throw new Error("浏览器不支持 canvas 录制");
  }

  let animationFrame = 0;

  const render = () => {
    drawMeetingFrame(context, canvas, videos, source.title);
    animationFrame = window.requestAnimationFrame(render);
  };

  render();

  const canvasStream = canvas.captureStream(24);
  const composedStream = new MediaStream();
  for (const videoTrack of canvasStream.getVideoTracks()) {
    composedStream.addTrack(videoTrack);
  }
  for (const audioTrack of audioDestination.stream.getAudioTracks()) {
    composedStream.addTrack(audioTrack);
  }

  const previousCleanup = cleanup;
  cleanup = () => {
    window.cancelAnimationFrame(animationFrame);
    canvasStream.getTracks().forEach((track) => track.stop());
    previousCleanup();
  };

  const recorder = new MediaRecorder(composedStream, { mimeType });
  return new LocalRecordingSession(recorder, cleanup, source.kind, mimeType, source.title);
}

export async function downloadRecording(asset: RecordingAsset): Promise<void> {
  const saveWindow = window as SaveFilePickerWindow;

  if (saveWindow.showSaveFilePicker) {
    const handle = await saveWindow.showSaveFilePicker({
      suggestedName: asset.fileName,
      types: [
        {
          description: asset.kind === "audio_only" ? "Audio Recording" : "Video Recording",
          accept: {
            [asset.mimeType]: [fileExtension(asset.mimeType)]
          }
        }
      ]
    });
    const writable = await handle.createWritable();
    await writable.write(asset.blob);
    await writable.close();
    return;
  }

  const anchor = document.createElement("a");
  anchor.href = asset.objectUrl;
  anchor.download = asset.fileName;
  anchor.click();
}

export function discardRecording(asset: RecordingAsset | null): void {
  if (!asset) {
    return;
  }

  URL.revokeObjectURL(asset.objectUrl);
}

async function prepareVideoElements(
  localStream: MediaStream | null,
  remoteStreams: MediaStream[]
): Promise<HTMLVideoElement[]> {
  const streams: MediaStream[] = [];
  for (const stream of [localStream, ...remoteStreams]) {
    if (stream && stream.getVideoTracks().length > 0) {
      streams.push(stream);
    }
  }

  const videos = streams.map((stream) => {
    const video = document.createElement("video");
    video.srcObject = stream;
    video.playsInline = true;
    video.muted = true;
    return video;
  });

  await Promise.all(
    videos.map((video) =>
      video.play().catch(() => {
        // 浏览器可能在后台阻止自动播放，这里容忍并继续，首帧未就绪时会绘制占位画面。
      })
    )
  );

  return videos;
}

function drawMeetingFrame(
  context: CanvasRenderingContext2D,
  canvas: HTMLCanvasElement,
  videos: HTMLVideoElement[],
  title: string
): void {
  context.fillStyle = "#0f1729";
  context.fillRect(0, 0, canvas.width, canvas.height);

  const gradient = context.createLinearGradient(0, 0, canvas.width, canvas.height);
  gradient.addColorStop(0, "rgba(58, 134, 255, 0.25)");
  gradient.addColorStop(1, "rgba(17, 24, 39, 0.15)");
  context.fillStyle = gradient;
  context.fillRect(0, 0, canvas.width, canvas.height);

  context.fillStyle = "rgba(255,255,255,0.9)";
  context.font = "bold 32px Segoe UI";
  context.fillText(title, 36, 56);

  if (videos.length === 0) {
    context.fillStyle = "rgba(255,255,255,0.65)";
    context.font = "24px Segoe UI";
    context.fillText("暂无可录制视频流", 36, 110);
    return;
  }

  const layouts = computeLayout(videos.length, canvas.width, canvas.height);
  videos.forEach((video, index) => {
    const layout = layouts[index];
    if (!layout) {
      return;
    }

    context.fillStyle = "rgba(255,255,255,0.08)";
    roundRect(context, layout.x, layout.y, layout.width, layout.height, 24);
    context.fill();

    if (video.videoWidth > 0 && video.videoHeight > 0) {
      const ratio = Math.max(layout.width / video.videoWidth, layout.height / video.videoHeight);
      const drawWidth = video.videoWidth * ratio;
      const drawHeight = video.videoHeight * ratio;
      const dx = layout.x + (layout.width - drawWidth) / 2;
      const dy = layout.y + (layout.height - drawHeight) / 2;
      context.save();
      roundRect(context, layout.x, layout.y, layout.width, layout.height, 24);
      context.clip();
      context.drawImage(video, dx, dy, drawWidth, drawHeight);
      context.restore();
    }

    context.fillStyle = "rgba(15, 23, 41, 0.62)";
    roundRect(context, layout.x + 16, layout.y + layout.height - 52, 220, 36, 18);
    context.fill();
    context.fillStyle = "#f8fafc";
    context.font = "20px Segoe UI";
    context.fillText(`Stream ${index + 1}`, layout.x + 30, layout.y + layout.height - 28);
  });
}

function computeLayout(count: number, width: number, height: number) {
  if (count === 1) {
    return [{ x: 36, y: 96, width: width - 72, height: height - 132 }];
  }

  if (count === 2) {
    const itemWidth = (width - 108) / 2;
    return [
      { x: 36, y: 120, width: itemWidth, height: height - 156 },
      { x: 72 + itemWidth, y: 120, width: itemWidth, height: height - 156 }
    ];
  }

  const columns = 2;
  const rows = Math.ceil(Math.min(count, 4) / columns);
  const itemWidth = (width - 36 * (columns + 1)) / columns;
  const itemHeight = (height - 120 - 24 * (rows - 1) - 36 * rows) / rows;
  return Array.from({ length: Math.min(count, 4) }, (_, index) => {
    const row = Math.floor(index / columns);
    const column = index % columns;
    return {
      x: 36 + column * (itemWidth + 36),
      y: 96 + row * (itemHeight + 36),
      width: itemWidth,
      height: itemHeight
    };
  });
}

function roundRect(
  context: CanvasRenderingContext2D,
  x: number,
  y: number,
  width: number,
  height: number,
  radius: number
): void {
  context.beginPath();
  context.moveTo(x + radius, y);
  context.arcTo(x + width, y, x + width, y + height, radius);
  context.arcTo(x + width, y + height, x, y + height, radius);
  context.arcTo(x, y + height, x, y, radius);
  context.arcTo(x, y, x + width, y, radius);
  context.closePath();
}

function selectMimeType(kind: RecordingKind): string {
  const candidates =
    kind === "audio_only"
      ? ["audio/webm;codecs=opus", "audio/webm"]
      : ["video/webm;codecs=vp9,opus", "video/webm;codecs=vp8,opus", "video/webm"];

  const supported = candidates.find((candidate) => MediaRecorder.isTypeSupported(candidate));
  if (!supported) {
    throw new Error("当前浏览器不支持所需的录制格式");
  }

  return supported;
}

function fileExtension(mimeType: string): string {
  if (mimeType.includes("audio/webm") || mimeType.includes("video/webm")) {
    return ".webm";
  }
  return ".bin";
}

function buildFileName(title: string, kind: RecordingKind): string {
  const safeTitle = title.replace(/[^\p{L}\p{N}_-]+/gu, "-").replace(/^-+|-+$/g, "") || "meeting";
  const suffix = kind === "audio_only" ? "audio" : "meeting";
  const stamp = new Date().toISOString().replaceAll(":", "-");
  return `${safeTitle}-${suffix}-${stamp}.webm`;
}
