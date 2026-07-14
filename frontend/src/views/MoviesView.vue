<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '@/api/client'
import type { Movie } from '@/types'
const movies = ref<Movie[]>([]); const loading = ref(true); const error = ref('')
onMounted(async () => { try { movies.value = (await api<Movie[]>('/movies')).data } catch (cause) { error.value = cause instanceof Error ? cause.message : 'Could not load movies.' } finally { loading.value = false } })
</script>
<template>
  <section class="page">
    <div class="hero"><div><span class="eyebrow">Now showing</span><h1>Pick a film. Claim your seat.</h1><p>Live availability, five-minute holds, no double booking.</p></div></div>
    <p v-if="loading" class="muted">Loading the programme…</p><p v-else-if="error" class="alert error">{{ error }}</p>
    <div v-else class="movie-grid"><RouterLink v-for="movie in movies" :key="movie.id" :to="`/movies/${movie.id}`" class="movie-card"><img :src="movie.poster_url" :alt="movie.title" /><div><span class="duration">{{ movie.duration_minutes }} min</span><h2>{{ movie.title }}</h2><p>{{ movie.description }}</p></div></RouterLink></div>
  </section>
</template>

