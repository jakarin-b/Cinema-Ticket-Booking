import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import MoviesView from '@/views/MoviesView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'movies', component: MoviesView },
    { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue') },
    { path: '/auth/callback', name: 'auth-callback', component: () => import('@/views/AuthCallbackView.vue') },
    { path: '/movies/:id', name: 'movie', component: () => import('@/views/MovieView.vue') },
    { path: '/showtimes/:id/seats', name: 'seats', component: () => import('@/views/SeatsView.vue'), meta: { auth: true } },
    { path: '/checkout/:holdId', name: 'checkout', component: () => import('@/views/CheckoutView.vue'), meta: { auth: true } },
    { path: '/bookings', name: 'bookings', component: () => import('@/views/BookingsView.vue'), meta: { auth: true } },
    { path: '/bookings/:id', name: 'booking', component: () => import('@/views/BookingView.vue'), meta: { auth: true } },
    { path: '/admin', name: 'admin', component: () => import('@/views/AdminView.vue'), meta: { auth: true, admin: true } },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

router.beforeEach(async (to) => {
  const auth = useAuthStore(); await auth.restore()
  if (to.meta.auth && !auth.isAuthenticated) return { name: 'login', query: { return_to: to.fullPath } }
  if (to.meta.admin && !auth.isAdmin) return { name: 'movies' }
  if (to.name === 'login' && auth.isAuthenticated) return { name: 'movies' }
})

export default router

