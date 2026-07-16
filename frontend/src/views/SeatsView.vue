<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ApiError, api, clearIdempotencyKey, localDate, money, savedIdempotencyFingerprint, stableIdempotencyKey } from "@/api/client";
import type { Hold, Seat, ShowtimeDetail } from "@/types";
type SeatEvent = { type: string; data?: { seat_ids?: string[] } };
const route = useRoute();
const router = useRouter();
const id = String(route.params.id);
const detail = ref<ShowtimeDetail>();
const seats = ref<Seat[]>([]);
const selected = ref(new Set<string>());
const error = ref("");
const notice = ref("");
const holding = ref(false);
let socket: WebSocket | undefined;
let reconnectTimer: number | undefined;
let closed = false;
let connectionGeneration = 0;
let synchronizationRequest = 0;
let syncing = false;
let bufferedEvents: SeatEvent[] = [];
const rows = computed(() =>
  seats.value.reduce<Record<string, Seat[]>>((grouped, seat) => {
    (grouped[seat.row] ??= []).push(seat);
    return grouped;
  }, {})
);
const selectedSeats = computed(() =>
  seats.value.filter((seat) => selected.value.has(seat.seat_id))
);
const total = computed(() =>
  selectedSeats.value.reduce((sum, seat) => sum + seat.price, 0)
);
function toggle(seat: Seat) {
  if (seat.status !== "AVAILABLE") return;
  const next = new Set(selected.value);
  next.has(seat.seat_id) ? next.delete(seat.seat_id) : next.add(seat.seat_id);
  selected.value = next;
}
async function loadDetail() {
  detail.value = (await api<ShowtimeDetail>(`/showtimes/${id}`)).data;
}
function apply(ids: string[], status: Seat["status"]) {
  seats.value = seats.value.map((seat) =>
    ids.includes(seat.seat_id) ? { ...seat, status } : seat
  );
  const taken = ids.filter((id) => selected.value.has(id));
  if (status !== "AVAILABLE" && taken.length) {
    const next = new Set(selected.value);
    taken.forEach((id) => next.delete(id));
    selected.value = next;
    notice.value = "Another customer just took one of your selected seats.";
  }
}
function applyEvent(message: SeatEvent) {
  const ids = message.data?.seat_ids ?? [];
  if (message.type === "seat.locked") apply(ids, "LOCKED");
  else if (message.type === "seat.released") apply(ids, "AVAILABLE");
  else if (message.type === "seat.booked") apply(ids, "BOOKED");
}
async function synchronizeSeats(generation: number) {
  const request = ++synchronizationRequest;
  syncing = true;
  try {
    const current = (await api<Seat[]>(`/showtimes/${id}/seats`)).data;
    if (generation !== connectionGeneration || request !== synchronizationRequest || closed) return;
    seats.value = current;
    const pending = bufferedEvents;
    bufferedEvents = [];
    pending.forEach(applyEvent);
    syncing = false;
  } catch (cause) {
    if (generation !== connectionGeneration || request !== synchronizationRequest || closed) return;
    syncing = false;
    error.value = cause instanceof Error ? cause.message : "Seat map could not be synchronized.";
    socket?.close();
  }
}
function connect(attempt = 0) {
  const generation = ++connectionGeneration;
  syncing = true;
  bufferedEvents = [];
  const protocol = location.protocol === "https:" ? "wss" : "ws";
  const connection = new WebSocket(`${protocol}://${location.host}/api/v1/ws/showtimes/${id}`);
  socket = connection;
  let snapshotSeen = false;
  connection.onmessage = (event) => {
    if (generation !== connectionGeneration || connection !== socket || closed) return;
    let message: SeatEvent;
    try {
      message = JSON.parse(String(event.data)) as SeatEvent;
    } catch {
      return;
    }
    if (message.type === "snapshot") {
      if (!snapshotSeen) {
        snapshotSeen = true;
        void synchronizeSeats(generation);
      }
      return;
    }
    if (syncing) bufferedEvents.push(message);
    else applyEvent(message);
  };
  connection.onclose = () => {
    if (!closed && connection === socket) {
      reconnectTimer = window.setTimeout(() => {
        connect(Math.min(attempt + 1, 5));
      }, Math.min(1000 * 2 ** attempt, 30000));
    }
  };
}
async function hold() {
  if (!selected.value.size) return;
  holding.value = true;
  error.value = "";
  const seatIDs = [...selected.value].sort();
  try {
    await submitHold(seatIDs);
  } catch (cause) {
    if (cause instanceof ApiError) clearIdempotencyKey(`hold:${id}`);
    error.value = cause instanceof Error ? cause.message : "The seats could not be held.";
    await synchronizeSeats(connectionGeneration);
  } finally {
    holding.value = false;
  }
}
async function submitHold(seatIDs: string[]) {
  const keyScope = `hold:${id}`;
  const response = await api<Hold>(`/showtimes/${id}/holds`, {
    method: "POST",
    headers: { "Idempotency-Key": stableIdempotencyKey(keyScope, seatIDs.join(",")) },
    body: JSON.stringify({ seat_ids: seatIDs }),
  });
  clearIdempotencyKey(keyScope);
  await router.push(`/checkout/${response.data.hold_id}`);
}
onMounted(async () => {
  try {
    await loadDetail();
    const pendingFingerprint = savedIdempotencyFingerprint(`hold:${id}`);
    const pendingSeatIDs = pendingFingerprint?.split(",").filter(Boolean) ?? [];
    const validPendingRequest = pendingSeatIDs.length > 0 && pendingSeatIDs.length <= 10 && pendingSeatIDs.every((seatID) => /^[a-f0-9]{24}$/.test(seatID));
    if (validPendingRequest) {
      holding.value = true;
      try {
        await submitHold(pendingSeatIDs);
        return;
      } catch (cause) {
        if (cause instanceof ApiError) clearIdempotencyKey(`hold:${id}`);
        error.value = cause instanceof Error ? cause.message : "The pending hold could not be recovered.";
      } finally {
        holding.value = false;
      }
    } else if (pendingFingerprint !== null) {
      clearIdempotencyKey(`hold:${id}`);
    }
    connect();
  } catch (cause) {
    error.value =
      cause instanceof Error ? cause.message : "Seat map could not be loaded.";
  }
});
onBeforeUnmount(() => {
  closed = true;
  socket?.close();
  if (reconnectTimer) clearTimeout(reconnectTimer);
});
</script>
<template>
  <section class="page seat-page">
    <div class="page-heading">
      <div>
        <span class="eyebrow">{{
          detail ? localDate(detail.showtime.start_time) : "Seat selection"
        }}</span>
        <h1>{{ detail?.movie.title ?? "Choose your seats" }}</h1>
        <p v-if="detail">{{ detail.auditorium.name }}</p>
      </div>
      <div class="legend">
        <span><i class="available"></i>Available</span
        ><span><i class="selected"></i>Selected</span
        ><span><i class="locked"></i>Held</span><span><i class="booked"></i>Booked</span>
      </div>
    </div>
    <p v-if="notice" class="alert warning">{{ notice }}</p>
    <p v-if="error" class="alert error">{{ error }}</p>
    <div class="screen"><span>SCREEN</span></div>
    <div class="seat-map">
      <div v-for="(rowSeats, row) in rows" :key="row" class="seat-row">
        <strong>{{ row }}</strong
        ><button
          v-for="seat in rowSeats"
          :key="seat.seat_id"
          class="seat"
          :class="[seat.status.toLowerCase(), { selected: selected.has(seat.seat_id) }]"
          :disabled="seat.status !== 'AVAILABLE'"
          :aria-label="`${seat.seat_label} ${seat.status}`"
          @click="toggle(seat)"
        >
          {{ seat.number }}
        </button>
      </div>
    </div>
    <aside class="selection-bar">
      <div>
        <span
          >{{ selectedSeats.length }} seat{{
            selectedSeats.length === 1 ? "" : "s"
          }}</span
        ><strong>{{
          selectedSeats.map((s) => s.seat_label).join(", ") || "None selected"
        }}</strong>
      </div>
      <div>
        <span>Total</span><strong>{{ money(total) }}</strong>
      </div>
      <button class="button" :disabled="!selected.size || holding" @click="hold">
        {{ holding ? "Holding…" : "Hold seats for 5 minutes" }}
      </button>
    </aside>
  </section>
</template>
