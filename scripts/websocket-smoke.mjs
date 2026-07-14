import { execFileSync } from 'node:child_process'

const showtimeId = '650000000000000000000064'
const password = process.env.REDIS_PASSWORD || 'cinema-local-redis'
const url = `ws://localhost:8080/api/v1/ws/showtimes/${showtimeId}`
const socket = new WebSocket(url)
let snapshotSeen = false
let incrementalSeen = false

const timeout = setTimeout(() => {
  console.error('WebSocket smoke test timed out.')
  process.exit(1)
}, 10_000)

socket.addEventListener('message', event => {
  const message = JSON.parse(String(event.data))
  if (message.type === 'snapshot') {
    snapshotSeen = Array.isArray(message.data?.seats) && message.data.seats.length === 80
    const fakeEvent = JSON.stringify({ event_id: crypto.randomUUID(), type: 'seat.locked', showtime_id: showtimeId, occurred_at: new Date().toISOString(), data: { seat_ids: ['6500000000000000000003e9'], expires_at: new Date(Date.now() + 300_000).toISOString() } })
    execFileSync('docker', ['compose', 'exec', '-T', 'redis', 'redis-cli', '-a', password, 'PUBLISH', 'cinema:seat-events', fakeEvent], { stdio: 'ignore' })
  } else if (message.type === 'seat.locked') {
    incrementalSeen = message.data?.seat_ids?.[0] === '6500000000000000000003e9'
    clearTimeout(timeout); socket.close()
    if (!snapshotSeen || !incrementalSeen) process.exit(1)
    console.log('WebSocket snapshot: true')
    console.log('WebSocket incremental fan-out: true')
  }
})
socket.addEventListener('error', () => { clearTimeout(timeout); process.exit(1) })

