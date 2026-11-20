import { createApp } from 'vue';
import App from './App.vue';
import { initializeTheme } from './theme.ts';
import '../../prototype/src/style.css';
import './style.css';

initializeTheme();
createApp(App).mount('#app');
