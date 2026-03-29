# Meeting

English | [简体中文](README.zh-CN.md)

`Meeting` is a browser-first multi-party video meeting system for desktop browsers and mobile H5. The project follows a `P2P-first` WebRTC model with `WSS` signaling, local-only recording, and immediate deletion of meeting runtime data after the session ends.

Current capabilities include video meetings, whiteboard collaboration, screen sharing, local recording, chat, ready checks, temporary meeting minutes, host/assistant/participant roles, anonymous display names, and a browser-friendly `WSS + WebRTC` communication model.

![Meeting login preview](docs/meeting_login.png)

## Architecture

- Media plane: `WebRTC`
- Control plane and signaling: `WSS`
- Backend: `Golang`
- Database: `SQLite3`
- Frontend: `TypeScript + React + Vite`
- Runtime data policy: keep meeting state in memory only and remove it when the meeting ends
- Persistence policy: store only basic audit events and saved media preferences for registered users

## Core Constraints

- `participant` starts with chat-only permissions
- `participant` needs host approval to use microphone, camera, whiteboard, screen sharing, recording, and ready check
- The host can promote a participant to assistant, and assistants receive the granted host-side capabilities
- Recording stays local by default and is not uploaded to the server
- Temporary chat history, whiteboard state, ready check results, and temporary minutes live only until the meeting ends
- The host cannot leave while other participants are still in the room, and must explicitly end the meeting instead

## Module Map

| Module | Path | Responsibility | Current status |
| --- | --- | --- | --- |
| Service entry | `cmd/server` | Wires config, logging, storage, meeting service, HTTP API, and signaling hub | Implemented |
| Architecture decisions | `docs/adr` | Records major architectural decisions and tradeoffs | Implemented |
| Design assets | `docs/design` | Stores reusable HTML/CSS previews and rendering helpers | Implemented |
| Config | `internal/config` | Loads server address, SQLite path, log directory, and runtime config | Implemented |
| Logging | `internal/logging` | Initializes JSON logging, daily rotation, and retention behavior | Implemented |
| Storage | `internal/storage/sqlite` | Persists audit events and saved media preferences | Implemented |
| Meeting domain | `internal/meeting` | Handles rooms, participants, permissions, whiteboard, chat, ready check, and temporary minutes | Core flow implemented |
| HTTP API | `internal/httpapi` | Exposes create/join/leave/end, nickname update, minutes query, and audit endpoints | Implemented |
| Signaling | `internal/signaling` | Manages WebSocket sessions, broadcasts, capability flow, SDP/ICE forwarding, and collaboration events | Implemented |
| Frontend API | `web/src/api.ts` | Wraps REST calls | Implemented |
| Frontend signaling | `web/src/signaling.ts` | Wraps WebSocket connection and event handling | Implemented |
| Frontend RTC | `web/src/rtc.ts` | Manages `RTCPeerConnection`, track sync, and stats collection | 1v1 path implemented, multi-peer mesh still needs hardening |
| Frontend recording | `web/src/recording.ts` | Handles local recording cache, download, and discard | Implemented |
| Frontend whiteboard | `web/src/whiteboard.tsx` | Draws and renders the shared whiteboard | Implemented |
| Meeting console | `web/src/App.tsx` | Renders the productized auth shell, meeting entry flows, featured stage, drawers, and secondary collaboration panels | Implemented |

## Feature Status

### Implemented

- [x] Create, join, leave, and host-end meeting flows
- [x] Host / assistant / participant role model
- [x] Chat-only default permissions for participants
- [x] 1v1 `WebRTC` P2P connection flow
- [x] Local media capture and local/remote video preview
- [x] Screen sharing
- [x] Local recording cache, download, and discard
- [x] Text chat
- [x] Whiteboard collaboration
- [x] Ready checks
- [x] Temporary in-meeting minutes
- [x] Local export of temporary minutes
- [x] Basic audit reporting
- [x] Anonymous and registered display names
- [x] Nickname updates with chat trail
- [x] Public 9-digit meeting numbers, meeting-number copy, and in-room share QR codes
- [x] Runtime state cleanup after meeting end

### Partially Implemented

- [~] Multi-party video meetings
  The repo already has a 1v1 primary path and a mesh foundation, but still needs multi-peer mesh hardening, weak-network handling, and graceful degradation.
- [~] Productized login and scheduling flow
  The new dark auth shell, quick meeting, scheduled meeting form, and password-gated join flow are implemented on the frontend, but real authentication, verification codes, and true scheduled-meeting persistence are still missing.
- [~] Meeting minutes
  Temporary minutes, chat history, whiteboard counts, and ready check summaries can be exported, but there is no host-side save reminder at meeting end yet.
- [~] Audit logging
  The frontend already reports latency, packet loss, frame rate, bitrate, and connection summary, but device fingerprinting and richer network context are still missing.

### Not Yet Implemented

- [ ] Real `TURN` integration and automatic fallback for peers that fail NAT traversal
- [ ] Dynamic multi-peer mesh management and performance optimization
- [ ] WeChat registration and QR-code login
- [ ] Email verification code registration/login
- [ ] Auto-fill the join form when opening an invite link directly
- [ ] Host reminder to save meeting minutes at meeting end

## Current UI Flow

- Before joining a meeting, the login view now uses a full-screen single-card layout with a large `meeting` wordmark, a focused spotlight below it, and a centered auth card in the same macOS-dark visual system as the room UI.
- Host flow: after sign-in, the app returns to the dark entry shell for quick meeting or scheduled meeting entry. In the current test mode, any non-empty email and password can sign in, and the page pre-fills `meeting@07c2.com.cn / helloworld`. The scheduled form currently reuses the existing create-meeting API and enters the room immediately.
- Join flow: enter the public 9-digit meeting number, run a preflight lookup, and then enter the password in a modal only if the meeting requires one. Grouped `3-3-3` meeting numbers with spaces are normalized automatically.
- In-room flow: the room now uses a single-screen full-stage layout with a top title bar, a bottom dock toolbar, attached host / meeting / settings / apps / end panels, and right-side drawers for members and chat. Idle meetings show an avatar wall; active media switches to a featured stage with a thumbnail rail.
- The share window now shows the public 9-digit meeting number, a QR code, and copy actions; the internal room id is no longer shown directly in user-facing UI.
- Whiteboard, ready check, temporary minutes, audit summary, and capability management remain available through menus, drawers, and floating panels around the main stage.

## API Surface

Key endpoints that are already available:

- `POST /api/meetings`
- `GET /api/meetings/{meetingID}`
- `GET /api/meetings/{meetingID}/minutes`
- `POST /api/meetings/{meetingID}/join`
- `POST /api/meetings/{meetingID}/participants/{participantID}/leave`
- `POST /api/meetings/{meetingID}/participants/{participantID}/nickname`
- `POST /api/meetings/{meetingID}/participants/{participantID}/capabilities/{capability}/grant`
- `POST /api/meetings/{meetingID}/participants/{participantID}/audit`
- `POST /api/meetings/{meetingID}/end`
- `PUT /api/users/{userID}/preferences`
- `GET /ws/meetings/{meetingID}`

Detailed contract docs live in [docs/api/README.md](docs/api/README.md).

Notes:

- `POST /api/meetings` now returns both the internal `id` and the public `meetingNumber`.
- Meeting-scoped REST endpoints such as `GET /api/meetings/{meetingID}` and `POST /api/meetings/{meetingID}/join` accept either the internal runtime id or the public 9-digit meeting number.
- `GET /ws/meetings/{meetingID}` still uses the internal runtime id to keep the signaling path stable.

## Local Development

### Backend

```bash
go run ./cmd/server
```

Optional environment variables:

- `MEETING_HTTP_ADDR`, default `:5180`
- `MEETING_SQLITE_PATH`, default `./data/meeting.db`
- `MEETING_LOG_DIR`, default `./logs`

### Frontend

```bash
cd web
npm install
npm run dev
```

The frontend dev server listens on `0.0.0.0:5188` by default.

### Makefile

```bash
make build
make run-backend
make run-frontend
make clean
```

- `make build`: builds the backend binary and frontend assets into `build/`
- Backend build output: `build/backend/meeting`
- Frontend build output: `build/frontend/`
- `make run-backend`: starts the backend and writes runtime logs and SQLite data to `build/run/`
- `make run-frontend`: starts the frontend dev server
- Frontend runtime logs are written to the browser console; `warn`/`error` and selected `info` events are batched to `POST /api/client-logs` and end up in the backend JSON logs, while the browser no longer persists them locally
- `make clean`: removes `build/`

## Validation

```bash
go test ./...
go build ./cmd/server
cd web && npm run build
make build
```

## Data Lifecycle

- Rooms, participants, permission states, temporary chat, whiteboard state, ready check state, and temporary minutes stay in memory only
- Runtime state is deleted immediately after the meeting ends
- The server persists only audit events and saved media preferences

## License

This project is released under the MIT License. See [LICENSE](LICENSE).

## Documentation

- Architecture ADR: `docs/adr/ADR-0001-20260325-meeting-architecture.md`
- Open issues: `docs/issues/README.md`
- Design assets: `docs/design/`
- UI rollout record: `docs/design/20260325-product-ui-rollout.md`
