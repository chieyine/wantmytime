import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

// The API the dev server forwards /api/v1 to; end-to-end tests point it at their own.
const apiTarget = process.env.API_INTERNAL_BASE || 'http://127.0.0.1:8081';

export default defineConfig({
	plugins: [sveltekit()],
	server: {
		proxy: { '/api/v1': { target: apiTarget, changeOrigin: false } }
	},
	preview: {
		proxy: { '/api/v1': { target: apiTarget, changeOrigin: false } }
	}
});
