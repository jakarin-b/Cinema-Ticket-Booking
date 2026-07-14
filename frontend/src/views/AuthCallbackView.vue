<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
const auth = useAuthStore(); const route = useRoute(); const router = useRouter(); const message = ref('Completing secure Google sign-in…')
onMounted(async () => { auth.initialized = false; await auth.restore(); if (auth.isAuthenticated) { const target = typeof route.query.return_to === 'string' ? route.query.return_to : '/'; await router.replace(target) } else { message.value = 'Sign-in could not be completed.' } })
</script>
<template><section class="center-page"><div class="panel status-card"><div class="spinner"></div><h1>{{ message }}</h1></div></section></template>

