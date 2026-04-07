/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_MEETING_API_BASE_URL?: string;
  readonly VITE_MEETING_SIGNALING_BASE_URL?: string;
  readonly VITE_MEETING_ICE_SERVERS?: string;
  readonly VITE_MEETING_STUN_URLS?: string;
  readonly VITE_MEETING_TURN_URLS?: string;
  readonly VITE_MEETING_TURN_USERNAME?: string;
  readonly VITE_MEETING_TURN_CREDENTIAL?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
