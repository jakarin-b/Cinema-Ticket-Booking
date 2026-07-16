<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { api, clearIdempotencyKey, localDate, money, stableIdempotencyKey } from "@/api/client";
import type { Booking, Hold, Seat, ShowtimeDetail } from "@/types";
const route = useRoute();
const router = useRouter();
const holdId = String(route.params.holdId);
const hold = ref<Hold>();
const detail = ref<ShowtimeDetail>();
const seats = ref<Seat[]>([]);
const now = ref(Date.now());
const busy = ref(false);
const error = ref("");
let timer: number;
const remaining = computed(() =>
  hold.value
    ? Math.max(
        0,
        Math.floor((new Date(hold.value.expires_at).getTime() - now.value) / 1000)
      )
    : 0
);
const labels = computed(() =>
  seats.value
    .filter((s) => hold.value?.seat_ids.includes(s.seat_id))
    .map((s) => s.seat_label)
);
const total = computed(() =>
  seats.value
    .filter((s) => hold.value?.seat_ids.includes(s.seat_id))
    .reduce((sum, s) => sum + s.price, 0)
);
const clock = computed(
  () =>
    `${String(Math.floor(remaining.value / 60)).padStart(2, "0")}:${String(
      remaining.value % 60
    ).padStart(2, "0")}`
);
async function load() {
  hold.value = (await api<Hold>(`/holds/${holdId}`)).data;
  const [show, map] = await Promise.all([
    api<ShowtimeDetail>(`/showtimes/${hold.value.showtime_id}`),
    api<Seat[]>(`/showtimes/${hold.value.showtime_id}/seats`),
  ]);
  detail.value = show.data;
  seats.value = map.data;
}
async function confirm() {
  busy.value = true;
  error.value = "";
  try {
    const booking = (
      await api<Booking>(`/holds/${holdId}/confirm`, {
        method: "POST",
        headers: { "Idempotency-Key": stableIdempotencyKey(`confirm:${holdId}`, "MOCK") },
        body: JSON.stringify({ payment_method: "MOCK" }),
      })
    ).data;
    clearIdempotencyKey(`confirm:${holdId}`);
    sessionStorage.removeItem(`hold:${holdId}`);
    await router.replace(`/bookings/${booking.id}`);
  } catch (cause) {
    error.value =
      cause instanceof Error ? cause.message : "Booking could not be confirmed.";
  } finally {
    busy.value = false;
  }
}
async function release() {
  busy.value = true;
  try {
    await api(`/holds/${holdId}`, { method: "DELETE" });
    clearIdempotencyKey(`confirm:${holdId}`);
    sessionStorage.removeItem(`hold:${holdId}`);
    await router.replace(`/showtimes/${hold.value?.showtime_id}/seats`);
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : "Hold could not be released.";
  } finally {
    busy.value = false;
  }
}
onMounted(async () => {
  try {
    await load();
    timer = window.setInterval(() => (now.value = Date.now()), 1000);
  } catch (cause) {
    error.value =
      cause instanceof Error ? cause.message : "Checkout could not be loaded.";
  }
});
onBeforeUnmount(() => clearInterval(timer));
</script>
<template>
  <section class="page narrow">
    <div v-if="hold && detail" class="checkout-grid">
      <div>
        <span class="eyebrow">Checkout</span>
        <h1>{{ detail.movie.title }}</h1>
        <div class="panel summary">
          <div>
            <span>Showtime</span
            ><strong>{{ localDate(detail.showtime.start_time) }}</strong>
          </div>
          <div>
            <span>Auditorium</span><strong>{{ detail.auditorium.name }}</strong>
          </div>
          <div>
            <span>Seats</span><strong>{{ labels.join(", ") }}</strong>
          </div>
          <div>
            <span>Total</span><strong>{{ money(total) }}</strong>
          </div>
        </div>
      </div>
      <div class="panel payment">
        <span class="eyebrow">Your hold expires in</span>
        <div class="countdown" :class="{ danger: remaining < 30 }">{{ clock }}</div>
        <p class="muted">Payment is mocked for this engineering demonstration.</p>
        <p v-if="error" class="alert error">{{ error }}</p>
        <button
          class="button full"
          :disabled="busy || remaining <= 0 || hold.status !== 'ACTIVE'"
          @click="confirm"
        >
          Confirm mock payment</button
        ><button class="button ghost full" :disabled="busy" @click="release">
          Release seats
        </button>
      </div>
    </div>
    <p v-else-if="error" class="alert error">{{ error }}</p>
    <p v-else>Loading checkout…</p>
  </section>
</template>
