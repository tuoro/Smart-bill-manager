import { createApp } from 'vue'
import App from './App.vue'
import router from './app/router'
import { initializeTheme } from './app/theme'
import './styles/tokens.css'
import './styles/app.css'

initializeTheme()
createApp(App).use(router).mount('#app')
