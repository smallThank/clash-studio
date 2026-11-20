import { defineConfig } from 'vite';

export default defineConfig({
  build: { modulePreload: false },
  plugins: [{
    name: 'standalone-preview',
    apply: 'build',
    enforce: 'post',
    generateBundle(_, bundle) {
      const page = bundle['index.html'];
      if (!page || page.type !== 'asset') return;
      page.source = String(page.source)
        .replace(/<script type="module"[^>]+src="\/([^"]+)"><\/script>/, (_, filename) => {
          const script = bundle[filename];
          if (script?.type !== 'chunk') throw new Error('缺少原型脚本 ' + filename);
          delete bundle[filename];
          return '<script type="module">' + script.code.replace(/<\/script/gi, '<\\/script') + '</script>';
        })
        .replace(/<link rel="stylesheet"[^>]+href="\/([^"]+)">/, (_, filename) => {
          const stylesheet = bundle[filename];
          if (stylesheet?.type !== 'asset') throw new Error('缺少原型样式 ' + filename);
          delete bundle[filename];
          return '<style>' + stylesheet.source + '</style>';
        });
    },
  }],
});
