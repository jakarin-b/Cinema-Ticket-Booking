<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
const auth = useAuthStore(); const route = useRoute(); const returnTo = computed(() => typeof route.query.return_to === 'string' ? route.query.return_to : '/')
</script>

<template>
  <section class="auth-page">
    <div class="auth-card panel">
      <span class="eyebrow">Welcome to Silver Screen</span>
      <h1>Your next story starts here.</h1>
      <p class="muted">Choose either Google identity path. Both create the same secure cinema account.</p>
      <p v-if="auth.error" class="alert error">{{ auth.error }}</p>
      <button class="button google" :disabled="auth.loading" @click="auth.signInFirebase"><span class="google-g">G</span> Continue with Google via Firebase</button>
      <button class="button secondary google" :disabled="auth.loading" @click="auth.signInGoogleOAuth(returnTo)"><span class="google-g">G</span> Continue with direct Google OAuth</button>
      <p class="fine-print">Roles and account linking are enforced by the backend from verified provider claims.</p>
    </div>
  </section>
</template>

