# API Documentation

This directory is the contract source of truth for backend interfaces.

## What lives here

- `openapi.yaml`: REST API contract for the HTTP backend
- `websocket-signaling.md`: WebSocket signaling protocol and event catalog

## Scope

- Use this directory when you need the exact request / response contract
- Keep `README.md` and `README.zh-CN.md` as high-level entry points
- Keep `docs/design/` for product and UI decisions, not API contracts

## Meeting identifier rule

- REST meeting-scoped endpoints accept either the internal runtime `id` or the public 9-digit meeting number
- The frontend usually keeps using the same identifier returned by the API so REST and WebSocket traffic stay aligned
- The WebSocket room path is documented separately in `websocket-signaling.md`

## Maintenance rule

- If an interface changes, update the contract here first
- Then sync the top-level README and `CHANGELOG.md`
