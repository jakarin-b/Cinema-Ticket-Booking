<script setup lang="ts">
import { onMounted, ref } from "vue";
import { api, localDate, money } from "@/api/client";
import type { Booking } from "@/types";
const bookings = ref<Booking[]>([]);
const error = ref("");
onMounted(async () => {
  try {
    bookings.value = (await api<Booking[]>("/bookings/me")).data;
  } catch (cause) {
    error.value =
      cause instanceof Error ? cause.message : "Bookings could not be loaded.";
  }
});
</script>
<template>
  <section class="page">
    <span class="eyebrow">Your cinema history</span>
    <h1>My bookings</h1>
    <p v-if="error" class="alert error">{{ error }}</p>
    <div class="booking-list">
      <RouterLink
        v-for="booking in bookings"
        :key="booking.id"
        :to="`/bookings/${booking.id}`"
        class="panel booking-row"
        ><div>
          <strong>{{ booking.movie_title }}</strong
          ><span>{{ localDate(booking.showtime_start) }}</span>
        </div>
        <div>
          <strong>{{ booking.seats.map((s) => s.label).join(", ") }}</strong
          ><span>{{ booking.booking_number }}</span>
        </div>
        <strong>{{ money(booking.total_amount, booking.currency) }}</strong></RouterLink
      >
      <p v-if="!bookings.length && !error" class="muted">
        You have no confirmed bookings yet.
      </p>
    </div>
  </section>
</template>
