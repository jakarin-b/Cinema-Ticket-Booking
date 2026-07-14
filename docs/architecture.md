# Architecture notes

The root README contains the primary Mermaid diagram and runtime flow. The important ownership boundaries are:

- The browser may propose seat IDs but never supplies trusted state, role, user ID, price, or expiration.
- Redis decides whether a contender may enter the MongoDB hold transaction.
- MongoDB conditions and transactions decide durable seat state.
- The worker may expire only an active hold and seats still carrying that hold ID.
- RabbitMQ is downstream of the transactional outbox and does not decide whether a booking succeeded.
- WebSocket events are hints. A MongoDB REST snapshot repairs any loss or reordering.

## Critical transition table

| Operation | Required prior state | Durable result | External compensation |
|---|---|---|---|
| Hold | `AVAILABLE` or expired `LOCKED` | seats `LOCKED`, hold `ACTIVE` | Release owned Redis keys on transaction failure |
| Manual release | hold `ACTIVE`, matching `hold_id` | seats `AVAILABLE`, hold `RELEASED` | Owned Redis release; TTL on failure |
| Confirm | owned, active, unexpired lock | seats `BOOKED`, hold `CONFIRMED`, booking + outbox | Owned Redis cleanup after commit |
| Expire | hold `ACTIVE` and expired | matching seats `AVAILABLE`, hold `EXPIRED` | Owned Redis cleanup after commit |

## Scaling boundary

API instances are stateless except for in-process WebSocket connections. Redis Pub/Sub broadcasts seat changes to every API instance. Sessions, OAuth state, rate limits, and locks already live in Redis. Multiple workers can safely race on expiration and outbox leases, although the provided Compose topology runs one of each.

