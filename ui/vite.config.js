import { defineConfig } from 'vite';

// The dev server proxies /api to api-service and strips the prefix, same as
// deploy/nginx/default.conf, so the page and the API share an origin: no ?ws=
// parameter and no cross-origin handshake in the dev loop. The build is a plain
// static folder (dist/) that any host can serve; which API it talks to is
// decided at deploy time by public/config.js, not here.
const API = process.env.VITE_API_ORIGIN || 'http://localhost:8088';

const proxy = {
  '/api': { target: API, ws: true, rewrite: (path) => path.replace(/^\/api/, '') },
};

export default defineConfig({
  server: { port: 5173, proxy },
  preview: { port: 4173, proxy },
  build: { outDir: 'dist', emptyOutDir: true },
});
