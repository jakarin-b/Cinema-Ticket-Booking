<script setup lang="ts">
import { onMounted, ref } from "vue";
import { api, localDate, money } from "@/api/client";
import type { AuditLog, Booking } from "@/types";
const bookings = ref<Booking[]>([]);
const audits = ref<AuditLog[]>([]);
const email = ref("");
const status = ref("");
const error = ref("");
async function load() {
  error.value = "";
  try {
    const query = new URLSearchParams();
    if (email.value) query.set("user_email", email.value);
    if (status.value) query.set("booking_status", status.value);
    const [b, a] = await Promise.all([
      api<Booking[]>(`/admin/bookings?${query}`),
      api<AuditLog[]>("/admin/audit-logs?limit=50"),
    ]);
    bookings.value = b.data;
    audits.value = a.data;
  } catch (cause) {
    error.value =
      cause instanceof Error ? cause.message : "Admin data could not be loaded.";
  }
}
onMounted(load);
</script>
<template>
  <section class="page">
    <div class="page-heading">
      <div>
        <span class="eyebrow">Operations</span>
        <h1>Admin dashboard</h1>
      </div>
      <form class="filters" @submit.prevent="load">
        <input v-model="email" type="search" placeholder="Filter by user email" /><select
          v-model="status"
        >
          <option value="">All statuses</option>
          <option value="CONFIRMED">Confirmed</option></select
        ><button class="button small">Apply</button>
      </form>
    </div>
    <p v-if="error" class="alert error">{{ error }}</p>
    <div class="admin-grid">
      <div class="panel table-panel">
        <h2>Bookings</h2>
        <div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Booking</th>
                <th>User</th>
                <th>Movie / showtime</th>
                <th>Seats</th>
                <th>Total</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="booking in bookings" :key="booking.id">
                <td>{{ booking.booking_number }}</td>
                <td>{{ booking.user_email }}</td>
                <td>
                  {{ booking.movie_title
                  }}<small>{{ localDate(booking.showtime_start) }}</small>
                </td>
                <td>{{ booking.seats.map((s) => s.label).join(", ") }}</td>
                <td>{{ money(booking.total_amount, booking.currency) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
      <div class="panel audit-panel">
        <h2>Audit log</h2>
        <div v-for="audit in audits" :key="audit.id" class="audit-item">
          <span class="badge" :class="audit.severity.toLowerCase()">{{
            audit.event_type
          }}</span
          ><small>{{ localDate(audit.created_at) }}</small>
          <p>{{ audit.entity_type }} · {{ audit.entity_id }}</p>
        </div>
      </div>
    </div>
  </section>
</template>
