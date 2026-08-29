import { createApp } from 'vue'
// Element Plus 改为按需导入（见 vite.config.js 中的 unplugin 配置），无需全量 app.use
import gitSync from '../gitSync.vue'

const app = createApp(gitSync)
app.mount('#app')