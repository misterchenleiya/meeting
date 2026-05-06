# ADR-0002-20260506-single-account-session

## Status

accepted

## Context

The current auth model allows one account to create multiple valid `auth_sessions`. This means the same registered account can stay logged in on multiple browsers or devices until each session expires or is manually logged out. A registered user can also join the same meeting more than once if different devices keep different participant sessions.

The product requirement is to prevent one account from being used concurrently across devices, and to prevent one registered account from participating in the same meeting at the same time through multiple participant identities.

## Decision

Use a single-active-session policy for registered accounts:

- Creating a new auth session first inserts the new session, then revokes all other still-active sessions for the same `user_id`. If revoking older sessions fails, the newly-created session is revoked before returning an error.
- Auth session creation is serialized inside the auth service to avoid two simultaneous login requests creating two valid sessions.
- Browser clients periodically validate `/api/auth/me`; if the session is revoked or expired, the client exits the meeting shell and returns to login.
- When a registered account joins a meeting, any existing non-host participant in that same meeting with the same `user_id` is marked as left before the new participant is added.
- The signaling hub broadcasts the old participant leaving and closes the old participant WebSocket so the old device exits promptly.
- When the host account joins its own active meeting again, the backend returns the existing host participant and the signaling hub replaces the previous connection for that participant.

Anonymous participants are not affected because they do not have a stable account identity.

## Alternatives Considered

- Allow multiple sessions and only block duplicate meeting joins: rejected because it does not prevent one account staying logged in on multiple devices.
- Reject the second login while an old session is active: rejected because users who closed a browser without logging out could get locked out until expiry.
- Store device fingerprints and allow one session per device class: rejected as unnecessary for the current product rule and more privacy-sensitive.

## Impact

- `auth_sessions` keeps historical rows for audit, but older active rows are marked with `revoked_at`.
- Existing devices discover revoked sessions on the next `/api/auth/me` check or when a protected API call returns `401`.
- Existing registered participant identities in the same meeting are removed when the same account joins again from a newer login.
- When the host account joins its own active meeting again from a newer device, the backend reuses the existing host participant identity and the signaling hub replaces the old WebSocket connection for that participant.

## Compatibility and Migration

No database schema migration is required. Existing active sessions remain valid until the first subsequent login for the same account, at which point older active sessions are revoked.

## Validation

- Auth service tests cover revoking an older session after a newer login.
- Meeting service tests cover replacing an older same-account participant in the same meeting.
- Signaling and frontend build tests verify the changed runtime flow still compiles and existing room events continue to work.

## Rollback

Rollback is limited to removing the session revocation call during session creation and the same-account participant replacement in `JoinMeeting`. Historical `revoked_at` values can remain in the database; they only affect sessions that were already intentionally invalidated.
