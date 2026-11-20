import { defineConfig, loadEnv } from 'vite';
import vue from '@vitejs/plugin-vue';

export default defineConfig(({ mode }) => {
  const { CLASH_API_ORIGIN = 'http://127.0.0.1:8080' } = loadEnv(mode, process.cwd(), 'CLASH_');
  return {
    plugins: [vue()],
    server: {
      proxy: { '/api': CLASH_API_ORIGIN, '/s': CLASH_API_ORIGIN },
      fs: { allow: ['..'] },
    },
  };
});
