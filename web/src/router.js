import { createRouter, createWebHistory } from 'vue-router'
import LoginView from './views/LoginView.vue'
import FilesView from './views/FilesView.vue'
import AdminView from './views/AdminView.vue'
import LogsView from './views/LogsView.vue'
import ChangePasswordView from './views/ChangePasswordView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: LoginView, meta: { public: true, titleKey: 'page.login' } },
    { path: '/change-password', component: ChangePasswordView, meta: { titleKey: 'page.changePassword' } },
    { path: '/', component: FilesView, meta: { titleKey: 'page.files' } },
    { path: '/logs', component: LogsView, meta: { titleKey: 'page.logs' } },
    { path: '/admin', component: AdminView, meta: { admin: true, titleKey: 'page.admin' } },
    { path: '/:pathMatch(.*)*', redirect: '/' }
  ]
})

// beforeEach 保护受限页面，并为未登录访问保留原始跳转地址。
// beforeEach guards restricted pages and preserves the original destination for unauthenticated users.
router.beforeEach(async (to) => {
  const token = localStorage.getItem('filebox_token')
  if (to.meta.public) {
    return token ? '/' : true
  }
  if (!token) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  try {
    const result = await fetch('/api/auth/me', { headers: { Authorization: `Bearer ${token}` } })
    const body = await result.json()
    if (!result.ok) return '/login'
    if (body.data?.mustChangePassword && to.path !== '/change-password') return '/change-password'
    if (to.meta.admin && body.data?.role !== 'admin') return '/'
  } catch {
    localStorage.removeItem('filebox_token')
    localStorage.removeItem('filebox_user')
    return '/login'
  }
  return true
})

// router 导出带认证与管理员检查的单页应用路由器。
// router exports the SPA router with authentication and admin checks.
export default router
