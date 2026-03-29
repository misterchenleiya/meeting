# WebSocket Signaling Protocol

This document describes the realtime signaling channel used by the meeting backend.

## Connection

- URL: `GET /ws/meetings/{meetingID}?participantId={participantID}`
- `meetingID` may be the internal runtime id or the public 9-digit meeting number, but the frontend usually keeps one consistent identifier for the whole room.
- `participantId` is required.

If signaling is not enabled on the server, the HTTP upgrade route returns `501` with a JSON error body.

## Envelope format

Both directions use the same JSON envelope shape:

```json
{
  "type": "event.name",
  "payload": {}
}
```

- `type`: event or command name
- `payload`: event body, may be omitted for empty commands

## Client to server messages

| Type | Payload | Purpose |
| --- | --- | --- |
| `signal.offer` | `{ "targetParticipantId": "...", "data": { "type": "offer", "sdp": "..." } }` | Forward a WebRTC offer to another participant |
| `signal.answer` | `{ "targetParticipantId": "...", "data": { "type": "answer", "sdp": "..." } }` | Forward a WebRTC answer |
| `signal.ice_candidate` | `{ "targetParticipantId": "...", "data": { ...candidate... } }` | Forward an ICE candidate |
| `capability.request` | `{ "capability": "camera" }` | Ask the host for a capability |
| `capability.grant` | `{ "targetParticipantId": "...", "capability": "camera" }` | Host grants a capability |
| `role.assign_assistant` | `{ "targetParticipantId": "..." }` | Host promotes a participant to assistant |
| `chat.message` | `{ "message": "..." }` | Send a chat message |
| `whiteboard.draw` | `{ "action": { ...whiteboard action... } }` | Append a whiteboard action |
| `whiteboard.clear` | empty | Clear the whiteboard |
| `ready_check.start` | `{ "timeoutSeconds": 15 }` | Host starts a ready check round |
| `ready_check.respond` | `{ "status": "confirmed" }` | Participant responds to the ready check |

## Server to client events

| Type | Payload | Purpose |
| --- | --- | --- |
| `session.welcome` | `{ "meeting": { ... }, "participantId": "...", "onlineParticipantIds": [...], "serverTime": "..." }` | Initial room snapshot after connection |
| `participant.online` | `{ "participantId": "...", "status": "online" }` | A participant came online |
| `participant.offline` | `{ "participantId": "...", "status": "offline" }` | A participant went offline |
| `participant.joined` | `{ "participant": { ... } }` | A participant joined the meeting |
| `participant.left` | `{ "participantId": "..." }` | A participant left the meeting |
| `capability.requested` | `{ "fromParticipantId": "...", "capability": "camera" }` | A participant requested permission |
| `capability.granted` | `{ "targetParticipantId": "...", "grantedBy": "...", "capability": "camera" }` | Host granted a capability |
| `role.assistant_assigned` | `{ "participant": { ... }, "assignedBy": "..." }` | Host promoted a participant to assistant |
| `participant.nickname_updated` | `{ "participant": { ... }, "previousNickname": "...", "systemMessage": { ... } }` | A nickname changed |
| `chat.message` | `{ "message": { ... } }` | A chat message was appended |
| `whiteboard.action` | `{ "action": { ... } }` | A whiteboard action was appended |
| `whiteboard.cleared` | `{ "clearedBy": "..." }` | Whiteboard was cleared |
| `ready_check.started` | `{ "round": { ... } }` | Ready check started |
| `ready_check.updated` | `{ "round": { ... } }` | Ready check updated |
| `ready_check.finished` | `{ "round": { ... } }` | Ready check finished |
| `meeting.ended` | `{ "endedByParticipantId": "..." }` | Meeting ended |
| `error` | `{ "code": "...", "message": "..." }` | Invalid message or processing error |

## Event notes

- `signal.offer` / `signal.answer` / `signal.ice_candidate` are forwarded to the target participant unchanged except for `fromParticipantId`
- `capability.grant` and `role.assign_assistant` are enforced by the meeting service; the server rejects unauthorized actors
- `whiteboard.clear` and `ready_check.start` are host / capability guarded through the meeting service
- `meeting.ended` is broadcast after the host ends the room and the signaling hub closes the room

## Example session

```bash
websocat "ws://localhost:5180/ws/meetings/123456789?participantId=participant-abc"
```

## Message examples

Send a chat message:

```json
{
  "type": "chat.message",
  "payload": {
    "message": "hello"
  }
}
```

Start a ready check:

```json
{
  "type": "ready_check.start",
  "payload": {
    "timeoutSeconds": 15
  }
}
```

## Related REST docs

- `docs/api/openapi.yaml`
- `README.md` and `README.zh-CN.md` only keep the high-level endpoint summary
