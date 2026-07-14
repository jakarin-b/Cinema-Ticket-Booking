<script setup lang="ts">
import { useAuthStore } from '@/stores/auth'
const auth = useAuthStore()
</script>

<template>
  <div class="app-shell">
    <header class="topbar">
      <RouterLink to="/" class="brand"><span class="brand-mark">S</span><span>Silver Screen</span></RouterLink>
      <nav>
        <RouterLink to="/">Movies</RouterLink>
        <RouterLink v-if="auth.isAuthenticated" to="/bookings">My bookings</RouterLink>
        <RouterLink v-if="auth.isAdmin" to="/admin">Admin</RouterLink>
      </nav>
      <div class="account">
        <template v-if="auth.user">
          <img v-if="auth.user.avatar_url" :src="auth.user.avatar_url" alt="" class="avatar" referrerpolicy="no-referrer" />
          <span class="account-name">{{ auth.user.display_name || auth.user.email }}</span>
          <button class="button ghost small" @click="auth.logout">Log out</button>
        </template>
        <RouterLink v-else to="/login" class="button small">Sign in</RouterLink>
      </div>
    </header>
    <main><RouterView /></main>
    <footer>Concurrency-safe booking demo · MongoDB + Redis + RabbitMQ</footer>
  </div>
</template>

