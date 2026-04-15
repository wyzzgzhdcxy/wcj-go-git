import { createApp } from 'vue'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import gitSync from '../gitSync.vue'

const app = createApp(gitSync)
app.use(ElementPlus)
app.mount('#app')