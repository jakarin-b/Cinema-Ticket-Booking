import { mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it } from 'vitest'
import LoginView from './LoginView.vue'

describe('LoginView', () => {
  it('offers both explicit Google authentication paths', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/login', component: LoginView }] })
    await router.push('/login?return_to=/bookings'); await router.isReady()
    const wrapper = mount(LoginView, { global: { plugins: [createPinia(), router] } })
    expect(wrapper.text()).toContain('Continue with Google via Firebase')
    expect(wrapper.text()).toContain('Continue with direct Google OAuth')
    expect(wrapper.findAll('button')).toHaveLength(2)
  })
})

