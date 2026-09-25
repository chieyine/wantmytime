import adapter from '@sveltejs/adapter-node';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

// Content Security Policy. SvelteKit adds a nonce (or hash, for prerendered
// pages) to every script it writes, so no inline script runs without one.
// src/hooks.server.ts adds the runtime origins that are only known from the
// environment (error reporting, the public photo bucket).
const csp = {
	mode: 'auto',
	directives: {
		'default-src': ['self'],
		'script-src': ['self'],
		// Svelte transitions and style attributes need inline styles; styles cannot run code.
		'style-src': ['self', 'unsafe-inline'],
		'img-src': ['self', 'data:', 'blob:'],
		'font-src': ['self'],
		'connect-src': ['self'],
		'media-src': ['none'],
		'object-src': ['none'],
		'frame-src': ['none'],
		'frame-ancestors': ['none'],
		'base-uri': ['self'],
		'form-action': ['self'],
		'manifest-src': ['self'],
		'worker-src': ['self']
	}
};

export default { preprocess: vitePreprocess(), kit: { adapter: adapter({ out: 'build' }), csp } };
