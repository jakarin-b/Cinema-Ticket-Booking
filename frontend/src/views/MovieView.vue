<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRoute } from "vue-router";
import { api, localDate } from "@/api/client";
import type { Movie, Showtime } from "@/types";
const route = useRoute();
const movie = ref<Movie>();
const showtimes = ref<Showtime[]>([]);
const error = ref("");
onMounted(async () => {
  try {
    const id = String(route.params.id);
    [movie.value, showtimes.value] = [
      (await api<Movie>(`/movies/${id}`)).data,
      (await api<Showtime[]>(`/movies/${id}/showtimes`)).data,
    ];
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : "Movie could not be loaded.";
  }
});
</script>
<template>
  <section class="page">
    <p v-if="error" class="alert error">{{ error }}</p>
    <div v-else-if="movie" class="movie-detail">
      <img :src="movie.poster_url" :alt="movie.title" />
      <div>
        <span class="eyebrow">{{ movie.duration_minutes }} minutes</span>
        <h1>{{ movie.title }}</h1>
        <p class="lead">{{ movie.description }}</p>
        <h2>Upcoming showtimes</h2>
        <div class="showtime-list">
          <RouterLink
            v-for="show in showtimes"
            :key="show.id"
            :to="`/showtimes/${show.id}/seats`"
            class="showtime"
            ><strong>{{ localDate(show.start_time) }}</strong
            ><span>Choose seats →</span></RouterLink
          >
        </div>
        <p v-if="!showtimes.length" class="muted">No future showtimes.</p>
      </div>
    </div>
  </section>
</template>
