import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { initializeApp, type FirebaseApp } from 'firebase/app'
import { getAuth, GoogleAuthProvider, signInWithPopup, signOut, type Auth } from 'firebase/auth'
import { api, configureAuth } from '@/api/client'
import type { User } from '@/types'

let firebaseApp: FirebaseApp | undefined
let firebaseAuth: Auth | undefined

function authClient(): Auth | undefined {
  if (firebaseAuth) return firebaseAuth
  const config = {
    apiKey: import.meta.env.VITE_FIREBASE_API_KEY,
    authDomain: import.meta.env.VITE_FIREBASE_AUTH_DOMAIN,
    projectId: import.meta.env.VITE_FIREBASE_PROJECT_ID,
    appId: import.meta.env.VITE_FIREBASE_APP_ID,
  }
  if (!config.apiKey || !config.projectId) return undefined
  firebaseApp = initializeApp(config)
  firebaseAuth = getAuth(firebaseApp)
  return firebaseAuth
}

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(null)
  const firebaseToken = ref<string | null>(null)
  const csrfToken = ref<string | null>(null)
  const method = ref<'firebase' | 'google_oauth' | null>(null)
  const loading = ref(false)
  const initialized = ref(false)
  const error = ref('')
  configureAuth(() => firebaseToken.value, () => csrfToken.value)

  async function signInFirebase() {
    const client = authClient()
    if (!client) { error.value = 'Firebase web configuration is missing.'; return }
    loading.value = true; error.value = ''
    try {
      const result = await signInWithPopup(client, new GoogleAuthProvider())
      firebaseToken.value = await result.user.getIdToken()
      const response = await api<{ user: User; auth_method: 'firebase' }>('/auth/session', { method: 'POST' })
      user.value = response.data.user; method.value = response.data.auth_method
    } catch (cause) { error.value = cause instanceof Error ? cause.message : 'Firebase sign-in failed.'; firebaseToken.value = null }
    finally { loading.value = false }
  }

  function signInGoogleOAuth(returnTo = '/') { window.location.assign(`/api/v1/auth/google/start?return_to=${encodeURIComponent(returnTo)}`) }

  async function restore() {
    if (initialized.value) return
    try {
      const client = authClient()
      if (client) {
        await client.authStateReady()
        firebaseToken.value = client.currentUser ? await client.currentUser.getIdToken() : null
      }
      const response = await api<{ user: User; auth_method: 'firebase' | 'google_oauth'; csrf_token?: string }>('/auth/me')
      user.value = response.data.user; method.value = response.data.auth_method; csrfToken.value = response.data.csrf_token ?? null
    } catch { user.value = null; method.value = null; csrfToken.value = null; firebaseToken.value = null }
    finally { initialized.value = true }
  }

  async function logout() {
    try { if (user.value) await api('/auth/logout', { method: 'POST' }) } catch { /* clear local state regardless */ }
    if (firebaseAuth) await signOut(firebaseAuth).catch(() => undefined)
    user.value = null; method.value = null; csrfToken.value = null; firebaseToken.value = null
  }

  return { user, method, loading, initialized, error, isAuthenticated: computed(() => Boolean(user.value)), isAdmin: computed(() => user.value?.role === 'ADMIN'), signInFirebase, signInGoogleOAuth, restore, logout }
})
