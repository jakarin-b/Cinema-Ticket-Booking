package lock

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/redis/go-redis/v9"
)

var acquireScript = redis.NewScript(`
for i, key in ipairs(KEYS) do
  if redis.call('EXISTS', key) == 1 then
    return {0, key}
  end
end
for i, key in ipairs(KEYS) do
  redis.call('PSETEX', key, ARGV[2], ARGV[1])
end
return {1}
`)

var verifyScript = redis.NewScript(`
for i, key in ipairs(KEYS) do
  if redis.call('GET', key) ~= ARGV[1] then
    return {0, key}
  end
end
return {1}
`)

var releaseScript = redis.NewScript(`
local released = 0
for i, key in ipairs(KEYS) do
  if redis.call('GET', key) == ARGV[1] then
    redis.call('DEL', key)
    released = released + 1
  end
end
return released
`)

type Manager struct {
	client *redis.Client
	ttl    time.Duration
}

func New(client *redis.Client, ttl time.Duration) *Manager { return &Manager{client: client, ttl: ttl} }

func SeatKey(showtimeID, seatID string) string {
	return fmt.Sprintf("seatlock:%s:%s", showtimeID, seatID)
}
func Ownership(holdID, userID, token string) string {
	return fmt.Sprintf("%s:%s:%s", holdID, userID, token)
}

func Keys(showtimeID string, seatIDs []string) []string {
	ids := append([]string(nil), seatIDs...)
	sort.Strings(ids)
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = SeatKey(showtimeID, id)
	}
	return keys
}

func (m *Manager) Acquire(ctx context.Context, keys []string, ownership string) (string, error) {
	result, err := acquireScript.Run(ctx, m.client, keys, ownership, m.ttl.Milliseconds()).Slice()
	if err != nil {
		return "", err
	}
	if len(result) > 0 && asInt64(result[0]) == 1 {
		return "", nil
	}
	if len(result) > 1 {
		return fmt.Sprint(result[1]), nil
	}
	return "unknown", nil
}

func (m *Manager) Verify(ctx context.Context, keys []string, ownership string) (string, error) {
	result, err := verifyScript.Run(ctx, m.client, keys, ownership).Slice()
	if err != nil {
		return "", err
	}
	if len(result) > 0 && asInt64(result[0]) == 1 {
		return "", nil
	}
	if len(result) > 1 {
		return fmt.Sprint(result[1]), nil
	}
	return "unknown", nil
}

func (m *Manager) Release(ctx context.Context, keys []string, ownership string) (int64, error) {
	return releaseScript.Run(ctx, m.client, keys, ownership).Int64()
}

func asInt64(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	default:
		return 0
	}
}
