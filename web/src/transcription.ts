type UploadChunk = {
  blob: Blob;
  sequence: number;
  startedAt: string;
  endedAt: string;
  sampleRate: number;
  mimeType: string;
};

type TranscriptionUploaderOptions = {
  stream: MediaStream;
  chunkMs?: number;
  targetSampleRate?: number;
  onChunk: (chunk: UploadChunk) => Promise<void>;
  onError?: (message: string) => void;
};

export class TranscriptionUploader {
  private readonly stream: MediaStream;
  private readonly chunkMs: number;
  private readonly targetSampleRate: number;
  private readonly onChunk: (chunk: UploadChunk) => Promise<void>;
  private readonly onError?: (message: string) => void;
  private audioContext: AudioContext | null = null;
  private source: MediaStreamAudioSourceNode | null = null;
  private processor: ScriptProcessorNode | null = null;
  private silentGain: GainNode | null = null;
  private buffers: Float32Array[] = [];
  private bufferedSamples = 0;
  private sequence = 0;
  private chunkStartedAt = new Date();
  private uploadChain: Promise<void> = Promise.resolve();

  constructor(options: TranscriptionUploaderOptions) {
    this.stream = options.stream;
    this.chunkMs = options.chunkMs ?? 4000;
    this.targetSampleRate = options.targetSampleRate ?? 16000;
    this.onChunk = options.onChunk;
    this.onError = options.onError;
  }

  async start(): Promise<void> {
    if (this.audioContext) {
      return;
    }
    const audioWindow = window as AudioContextWindow;
    const AudioContextCtor = window.AudioContext || audioWindow.webkitAudioContext;
    if (!AudioContextCtor) {
      throw new Error("当前浏览器不支持实时音频采集");
    }

    this.audioContext = new AudioContextCtor();
    this.source = this.audioContext.createMediaStreamSource(this.stream);
    this.processor = this.audioContext.createScriptProcessor(4096, 1, 1);
    this.silentGain = this.audioContext.createGain();
    this.silentGain.gain.value = 0;
    this.chunkStartedAt = new Date();

    this.processor.onaudioprocess = (event) => {
      const samples = event.inputBuffer.getChannelData(0);
      this.buffers.push(new Float32Array(samples));
      this.bufferedSamples += samples.length;
      if (this.bufferedSamples >= (this.audioContext?.sampleRate ?? this.targetSampleRate) * (this.chunkMs / 1000)) {
        this.flush(false);
      }
    };

    this.source.connect(this.processor);
    this.processor.connect(this.silentGain);
    this.silentGain.connect(this.audioContext.destination);
  }

  async stop(): Promise<void> {
    this.flush(true);
    this.processor?.disconnect();
    this.source?.disconnect();
    this.silentGain?.disconnect();
    this.processor = null;
    this.source = null;
    this.silentGain = null;
    const context = this.audioContext;
    this.audioContext = null;
    if (context) {
      await context.close();
    }
    await this.uploadChain;
  }

  private flush(force: boolean): void {
    const context = this.audioContext;
    if (!context || this.bufferedSamples === 0) {
      return;
    }
    if (!force && this.bufferedSamples < context.sampleRate * 0.8) {
      return;
    }

    const startedAt = this.chunkStartedAt;
    const endedAt = new Date();
    const samples = mergeSamples(this.buffers, this.bufferedSamples);
    this.buffers = [];
    this.bufferedSamples = 0;
    this.chunkStartedAt = endedAt;

    const downsampled = downsample(samples, context.sampleRate, this.targetSampleRate);
    const wavBytes = encodeWav(downsampled, this.targetSampleRate);
    const chunk: UploadChunk = {
      blob: new Blob([wavBytes], { type: "audio/wav" }),
      sequence: ++this.sequence,
      startedAt: startedAt.toISOString(),
      endedAt: endedAt.toISOString(),
      sampleRate: this.targetSampleRate,
      mimeType: "audio/wav"
    };

    this.uploadChain = this.uploadChain
      .then(() => this.onChunk(chunk))
      .catch((error: unknown) => {
        this.onError?.(error instanceof Error ? error.message : String(error));
      });
  }
}

type AudioContextWindow = Window & {
  webkitAudioContext?: typeof AudioContext;
};

function mergeSamples(buffers: Float32Array[], totalSamples: number): Float32Array {
  const merged = new Float32Array(totalSamples);
  let offset = 0;
  for (const buffer of buffers) {
    merged.set(buffer, offset);
    offset += buffer.length;
  }
  return merged;
}

function downsample(samples: Float32Array, sourceRate: number, targetRate: number): Float32Array {
  if (targetRate >= sourceRate) {
    return samples;
  }

  const ratio = sourceRate / targetRate;
  const outputLength = Math.floor(samples.length / ratio);
  const output = new Float32Array(outputLength);
  for (let index = 0; index < outputLength; index += 1) {
    const start = Math.floor(index * ratio);
    const end = Math.min(Math.floor((index + 1) * ratio), samples.length);
    let sum = 0;
    for (let sampleIndex = start; sampleIndex < end; sampleIndex += 1) {
      sum += samples[sampleIndex];
    }
    output[index] = sum / Math.max(1, end - start);
  }
  return output;
}

function encodeWav(samples: Float32Array, sampleRate: number): ArrayBuffer {
  const bytesPerSample = 2;
  const buffer = new ArrayBuffer(44 + samples.length * bytesPerSample);
  const view = new DataView(buffer);

  writeString(view, 0, "RIFF");
  view.setUint32(4, 36 + samples.length * bytesPerSample, true);
  writeString(view, 8, "WAVE");
  writeString(view, 12, "fmt ");
  view.setUint32(16, 16, true);
  view.setUint16(20, 1, true);
  view.setUint16(22, 1, true);
  view.setUint32(24, sampleRate, true);
  view.setUint32(28, sampleRate * bytesPerSample, true);
  view.setUint16(32, bytesPerSample, true);
  view.setUint16(34, 16, true);
  writeString(view, 36, "data");
  view.setUint32(40, samples.length * bytesPerSample, true);

  let offset = 44;
  for (const sample of samples) {
    const clamped = Math.max(-1, Math.min(1, sample));
    view.setInt16(offset, clamped < 0 ? clamped * 0x8000 : clamped * 0x7fff, true);
    offset += 2;
  }

  return buffer;
}

function writeString(view: DataView, offset: number, value: string): void {
  for (let index = 0; index < value.length; index += 1) {
    view.setUint8(offset + index, value.charCodeAt(index));
  }
}
