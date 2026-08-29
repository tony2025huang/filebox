import { createApp } from 'vue'
import App from './App.vue'
import router from './router'
import './styles.css'
import { brand, loadBrand } from './brand'
import { loadLocale } from './i18n'

// 应用启动时先挂载 Vue 路由，再异步加载公开品牌配置。
// Mount Vue and the router first, then load public branding asynchronously.
createApp(App).use(router).mount('#app')
loadBrand().then(() => loadLocale(brand.defaultLang))
