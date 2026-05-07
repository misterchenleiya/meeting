# API Documentation

This directory is the contract source of truth for backend interfaces.

## What lives here

- `openapi.yaml`: REST API contract for the HTTP backend
- `websocket-signaling.md`: WebSocket signaling protocol and event catalog

## Scope

- Use this directory when you need the exact request / response contract
- Keep `README.md` and `README.zh-CN.md` as high-level entry points
- Keep `docs/design/` for product and UI decisions, not API contracts
- Meeting snapshots and realtime `chat.message` events may carry structured system-message metadata through `ChatMessage.kind` and `ChatMessage.action`; update the REST and WebSocket contracts together when this shape changes

## Meeting identifier rule

- REST meeting-scoped endpoints are documented with the public 9-digit `meetingNumber` path parameter
- During the compatibility window, legacy internal runtime ids are still accepted and normalized server-side
- WebSocket connections also use the public `meetingNumber`; the signaling hub resolves it to the internal room id before registering clients
- The WebSocket room path is documented separately in `websocket-signaling.md`

## Maintenance rule

- If an interface changes, update the contract here first
- Then sync the top-level README and `CHANGELOG.md`
