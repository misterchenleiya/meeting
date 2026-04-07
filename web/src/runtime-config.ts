type RuntimeIceServer = RTCIceServer & {
  urls: string | string[];
};

const defaultStunUrls = ["stun:stun.l.google.com:19302"];

function readEnv(key: keyof ImportMetaEnv): string | undefined {
  const value = import.meta.env[key];
  if (typeof value !== "string") {
    return undefined;
  }

  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : undefined;
}

function normalizeBaseUrl(value: string): string {
  const url = new URL(value);
  if (!url.pathname.endsWith("/")) {
    url.pathname = `${url.pathname}/`;
  }
  return url.toString();
}

function parseUrlList(value: string | undefined): string[] {
  if (!value) {
    return [];
  }

  return value
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter((item) => item.length > 0);
}

function normalizeIceServer(raw: Partial<RTCIceServer> & { urls?: string | string[] }): RTCIceServer {
  if (!raw.urls) {
    throw new Error("ICE server entry is missing urls");
  }

  const urls = Array.isArray(raw.urls)
    ? raw.urls.map((item) => item.trim()).filter((item) => item.length > 0)
    : raw.urls
        .split(/[\n,]/)
        .map((item) => item.trim())
        .filter((item) => item.length > 0);

  if (urls.length === 0) {
    throw new Error("ICE server entry has no usable urls");
  }

  const normalized: RTCIceServer = {
    urls
  };

  if (typeof raw.username === "string" && raw.username.trim().length > 0) {
    normalized.username = raw.username.trim();
  }

  if (typeof raw.credential === "string" && raw.credential.trim().length > 0) {
    normalized.credential = raw.credential.trim();
  }

  return normalized;
}

export function resolveApiBaseUrl(): string | null {
  const configuredBase = readEnv("VITE_MEETING_API_BASE_URL");
  if (!configuredBase) {
    return null;
  }

  return normalizeBaseUrl(configuredBase);
}

export function resolveApiUrl(path: string): string {
  const normalizedPath = path.startsWith("/") ? path.slice(1) : path;
  const baseUrl = resolveApiBaseUrl();
  if (!baseUrl) {
    return `/${normalizedPath}`;
  }

  return new URL(normalizedPath, baseUrl).toString();
}

export function resolveSignalBaseUrl(): string | null {
  const configuredBase = readEnv("VITE_MEETING_SIGNALING_BASE_URL");
  if (configuredBase) {
    return normalizeBaseUrl(configuredBase);
  }

  if (typeof window === "undefined") {
    return null;
  }

  const origin = new URL(window.location.origin);
  if (origin.port === "5188") {
    origin.port = "5180";
  }

  return normalizeBaseUrl(origin.toString());
}

export function resolveSignalUrl(path: string): URL {
  const normalizedPath = path.startsWith("/") ? path.slice(1) : path;
  const baseUrl = resolveSignalBaseUrl();
  const url = baseUrl ? new URL(normalizedPath, baseUrl) : new URL(normalizedPath, window.location.origin);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  return url;
}

export function resolveIceServers(): RTCIceServer[] {
  const configuredServers = readEnv("VITE_MEETING_ICE_SERVERS");
  if (configuredServers) {
    try {
      const parsed = JSON.parse(configuredServers) as Array<Partial<RTCIceServer> & { urls?: string | string[] }>;
      if (!Array.isArray(parsed)) {
        throw new Error("ICE servers config must be an array");
      }

      return parsed.map((item) => normalizeIceServer(item));
    } catch (error) {
      const reason = error instanceof Error ? error.message : "unknown error";
      throw new Error(`解析 VITE_MEETING_ICE_SERVERS 失败: ${reason}`);
    }
  }

  const stunUrls = parseUrlList(readEnv("VITE_MEETING_STUN_URLS"));
  const iceServers: RTCIceServer[] = [
    {
      urls: stunUrls.length > 0 ? stunUrls : defaultStunUrls
    }
  ];

  const turnUrls = parseUrlList(readEnv("VITE_MEETING_TURN_URLS"));
  if (turnUrls.length > 0) {
    const turnServer: RuntimeIceServer = {
      urls: turnUrls
    };
    const username = readEnv("VITE_MEETING_TURN_USERNAME");
    const credential = readEnv("VITE_MEETING_TURN_CREDENTIAL");
    if (username) {
      turnServer.username = username;
    }
    if (credential) {
      turnServer.credential = credential;
    }
    iceServers.push(turnServer);
  }

  return iceServers;
}
