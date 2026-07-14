<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { api, idempotencyKey, localDate, money } from "@/api/client";
import type { Hold, Seat, ShowtimeDetail } from "@/types";
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
async function load() {
  const [show, seatMap] = await Promise.all([
    api<ShowtimeDetail>(`/showtimes/${id}`),
    api<Seat[]>(`/showtimes/${id}/seats`),
  ]);
  detail.value = show.data;
  seats.value = seatMap.data;
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
function connect(attempt = 0) {
  const protocol = location.protocol === "https:" ? "wss" : "ws";
  socket = new WebSocket(`${protocol}://${location.host}/api/v1/ws/showtimes/${id}`);
  socket.onmessage = (event) => {
    const message = JSON.parse(event.data);
    if (message.type === "snapshot") seats.value = message.data.seats;
    else if (message.type === "seat.locked") apply(message.data.seat_ids, "LOCKED");
    else if (message.type === "seat.released") apply(message.data.seat_ids, "AVAILABLE");
    else if (message.type === "seat.booked") apply(message.data.seat_ids, "BOOKED");
  };
  socket.onclose = () => {
    if (!closed) {
      reconnectTimer = window.setTimeout(async () => {
        await load().catch(() => undefined);
        connect(Math.min(attempt + 1, 5));
      }, Math.min(1000 * 2 ** attempt, 30000));
    }
  };
}
async function hold() {
  if (!selected.value.size) return;
  holding.value = true;
  error.value = "";
  try {
    const response = await api<Hold>(`/showtimes/${id}/holds`, {
      method: "POST",
      headers: { "Idempotency-Key": idempotencyKey() },
      body: JSON.stringify({ seat_ids: [...selected.value] }),
    });
    sessionStorage.setItem(
      `hold:${response.data.hold_id}`,
      JSON.stringify(response.data)
    );
    await router.push(`/checkout/${response.data.hold_id}`);
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : "The seats could not be held.";
    await load();
  } finally {
    holding.value = false;
  }
}
onMounted(async () => {
  try {
    await load();
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
