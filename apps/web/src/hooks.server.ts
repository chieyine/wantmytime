import type { Handle, HandleFetch, HandleServerError } from '@sveltejs/kit';
import { env } from '$env/dynamic/private';
import { reportError } from '$lib/observe/sentry';

const requestIdPattern = /^[A-Za-z0-9._-]{8,64}$/;

function log(level: 'info' | 'warn' | 'error', message: string, fields: Record<string, unknown>) {
	// One JSON object per line, matching the API and gateway logs.
	console.log(JSON.stringify({ time: new Date().toISOString(), level: level.toUpperCase(), msg: message, service: 'web', ...fields }));
}

export const handle: Handle = async ({ event, resolve }) => {
	const started = performance.now();
	const incoming = event.request.headers.get('x-request-id') ?? '';
	event.locals.requestId = requestIdPattern.test(incoming) ? incoming : crypto.randomUUID().replaceAll('-', '');
	const response = await resolve(event);
	response.headers.set('x-request-id', event.locals.requestId);
	const route = event.route.id ?? 'unmatched';
	if (route !== '/health') {
		log(response.status >= 500 ? 'warn' : 'info', 'request', { request_id: event.locals.requestId, method: event.request.method, route, status: response.status, duration_ms: Math.round(performance.now() - started) });
	}
	return response;
};

// Server-side loads that call the API pass the same request ID along.
export const handleFetch: HandleFetch = async ({ event, request, fetch }) => {
	const apiBase = env.API_INTERNAL_BASE || 'http://127.0.0.1:8081';
	if (request.url.startsWith(apiBase) && event.locals.requestId) {
		request.headers.set('x-request-id', event.locals.requestId);
	}
	return fetch(request);
};

export const handleError: HandleServerError = ({ error, event, status, message }) => {
	const reference = event.locals.requestId || crypto.randomUUID().replaceAll('-', '');
	if (status >= 500) {
		log('error', 'unhandled server error', { request_id: reference, route: event.route.id ?? 'unmatched', status, error: error instanceof Error ? `${error.name}: ${error.message}` : String(error), stack: error instanceof Error ? error.stack : undefined });
		reportError(error, { dsn: env.SENTRY_DSN, platform: 'node', environment: env.APP_ENV, release: env.APP_RELEASE, tags: { route: event.route.id ?? 'unmatched', status: String(status), request_id: reference } });
	}
	return { message: status >= 500 ? 'Something went wrong on our side.' : message, reference };
};
