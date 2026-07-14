<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api, localDate, money } from '@/api/client'
import type { Booking } from '@/types'
const route = useRoute(); const booking = ref<Booking>(); const error = ref(''); onMounted(async () => { try { booking.value = (await api<Booking>(`/bookings/${route.params.id}`)).data } catch (cause) { error.value = cause instanceof Error ? cause.message : 'Booking could not be loaded.' } })
</script>
<template><section class="center-page"><p v-if="error" class="alert error">{{ error }}</p><div v-else-if="booking" class="ticket panel"><div class="success-mark">✓</div><span class="eyebrow">Booking confirmed</span><h1>{{ booking.movie_title }}</h1><p class="booking-number">{{ booking.booking_number }}</p><div class="ticket-details"><div><span>Showtime</span><strong>{{ localDate(booking.showtime_start) }}</strong></div><div><span>Auditorium</span><strong>{{ booking.auditorium_name }}</strong></div><div><span>Seats</span><strong>{{ booking.seats.map(s => s.label).join(', ') }}</strong></div><div><span>Total paid</span><strong>{{ money(booking.total_amount, booking.currency) }}</strong></div></div><RouterLink to="/bookings" class="button">View all bookings</RouterLink></div></section></template>

