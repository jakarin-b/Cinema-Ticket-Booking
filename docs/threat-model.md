# Focused threat model

| Threat | Control | Residual risk |
|---|---|---|
| Seat race or lock rollback | Atomic Lua acquisition, ownership-safe release, Mongo conditions and transactions | Redis/Mongo outage reduces availability |
| Stale worker releases a new hold | Release filter includes `status=LOCKED` and original `hold_id` | None within Mongo consistency model |
| Forged user/role | Verify provider token/session; role comes only from `ADMIN_EMAILS` | Admin email list compromise |
| OAuth login CSRF/replay | Random state, one-time Redis `GETDEL`, nonce, PKCE, short TTL | Compromised browser remains outside server control |
| Cookie CSRF | SameSite cookie plus session-bound header token | XSS could read the header token; CSP/TLS remain deployment concerns |
| Session theft | Random opaque token, HttpOnly/Secure production cookie, TTL, logout deletion | No provider-wide revocation in this assignment |
| Cross-account linking | Provider subject first; only normalized verified-email fallback | Provider account/email compromise |
| Booking retry duplication | Unique hold and idempotency indexes | Client must retain its idempotency key |
| Message loss/duplicate | Transactional outbox, confirms, manual ack, idempotent consumer | Single local broker is not highly available |
| Secret leakage | Environment variables, redacted logs, no tokens in audit metadata | Local `.env` file protection is the operator's responsibility |
| Resource exhaustion | Body limit, hold rate limit, bounded WebSocket queue, pagination limits | No global WAF or distributed DDoS protection |

