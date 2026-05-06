export type ClientProfile = Record<string, string>;

export function collectClientProfile(): ClientProfile {
  if (typeof window === "undefined" || typeof navigator === "undefined") {
    return {};
  }

  const connection = getNetworkConnection();
  return compactProfile({
    browser: detectBrowser(navigator.userAgent),
    os: detectOS(navigator.userAgent),
    deviceCategory: detectDeviceCategory(navigator.userAgent, window.innerWidth),
    language: navigator.language || "",
    timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone || "",
    screenWidthBucket: widthBucket(window.screen.width),
    viewportWidthBucket: widthBucket(window.innerWidth),
    colorScheme: window.matchMedia?.("(prefers-color-scheme: dark)").matches ? "dark" : "light",
    networkEffectiveType: stringValue(connection?.effectiveType),
    webRTCSupported: booleanValue(typeof RTCPeerConnection !== "undefined"),
    mediaDevicesSupported: booleanValue(!!navigator.mediaDevices?.getUserMedia),
    audioInputSupported: booleanValue(!!navigator.mediaDevices?.getUserMedia),
    videoInputSupported: booleanValue(!!navigator.mediaDevices?.getUserMedia)
  });
}

function compactProfile(profile: ClientProfile): ClientProfile {
  const result: ClientProfile = {};
  for (const [key, value] of Object.entries(profile)) {
    const trimmed = value.trim();
    if (trimmed) {
      result[key] = trimmed.slice(0, 80);
    }
  }
  return result;
}

function getNetworkConnection(): { effectiveType?: unknown } | null {
  const candidate = navigator as Navigator & {
    connection?: { effectiveType?: unknown };
    mozConnection?: { effectiveType?: unknown };
    webkitConnection?: { effectiveType?: unknown };
  };
  return candidate.connection ?? candidate.mozConnection ?? candidate.webkitConnection ?? null;
}

function widthBucket(width: number): string {
  if (width < 480) {
    return "<480";
  }
  if (width < 768) {
    return "480-767";
  }
  if (width < 1024) {
    return "768-1023";
  }
  if (width < 1440) {
    return "1024-1439";
  }
  return "1440+";
}

function detectDeviceCategory(userAgent: string, viewportWidth: number): string {
  const normalized = userAgent.toLowerCase();
  if (/ipad|tablet/.test(normalized)) {
    return "tablet";
  }
  if (/mobi|android|iphone|ipod/.test(normalized) || viewportWidth < 768) {
    return "mobile";
  }
  return "desktop";
}

function detectBrowser(userAgent: string): string {
  if (/Edg\//.test(userAgent)) {
    return "Edge";
  }
  if (/Firefox\//.test(userAgent)) {
    return "Firefox";
  }
  if (/Chrome\//.test(userAgent) && !/Edg\//.test(userAgent)) {
    return "Chrome";
  }
  if (/Safari\//.test(userAgent) && !/Chrome\//.test(userAgent)) {
    return "Safari";
  }
  return "Other";
}

function detectOS(userAgent: string): string {
  if (/Windows NT/.test(userAgent)) {
    return "Windows";
  }
  if (/Mac OS X/.test(userAgent)) {
    return "macOS";
  }
  if (/Android/.test(userAgent)) {
    return "Android";
  }
  if (/iPhone|iPad|iPod/.test(userAgent)) {
    return "iOS";
  }
  if (/Linux/.test(userAgent)) {
    return "Linux";
  }
  return "Other";
}

function stringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function booleanValue(value: boolean): string {
  return value ? "true" : "false";
}
