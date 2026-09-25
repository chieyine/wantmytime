import type { Handle, HandleFetch, HandleServerError } from '@sveltejs/kit';
import { env } from '$env/dynamic/private';
import { env as publicEnv } from '$env/dynamic/public';
import { reportError } from '$lib/observe/sentry';

const requestIdPattern = /^[A-Za-z0-9._-]{8,64}$/;
const privatePath =
	/^\/(app|ops|login|claim|verify|auth|booking|book|offer|checkout|payment|access|dev|unsubscribe)(\/|$)/;

function log(level: 'info' | 'warn' | 'error', message: string, fields: Record<string, unknown>) {
	// One JSON object per line, matching the API and gateway logs.
	console.log(
		JSON.stringify({
			time: new Date().toISOString(),
			level: level.toUpperCase(),
			msg: message,
			service: 'web',
			...fields
		})
	);
}

// Origins only known at runtime, added to the Content Security Policy that
// SvelteKit builds from svelte.config.js.
function origin(value: string | undefined): string | null {
	if (!value) return null;
	try {
		const url = new URL(value.trim());
		return url.protocol === 'https:' || url.protocol === 'http:' ? url.origin : null;
	} catch {
		return null;
	}
}

function extendPolicy(policy: string, additions: Record<string, (string | null)[]>): string {
	return policy
		.split(';')
		.map((part) => {
			const directive = part.trim();
			const name = directive.split(/\s+/)[0];
			const extra = (additions[name] ?? []).filter((v): v is string => !!v && !directive.includes(v));
			return extra.length ? `${directive} ${extra.join(' ')}` : directive;
		})
		.filter(Boolean)
		.join('; ');
}

function securityHeaders(headers: Headers) {
	headers.set('x-content-type-options', 'nosniff');
	headers.set('referrer-policy', 'strict-origin-when-cross-origin');
	headers.set('x-frame-options', 'DENY');
	headers.set('cross-origin-opener-policy', 'same-origin');
	headers.set('permissions-policy', 'camera=(), microphone=(), geolocation=(), payment=(), usb=()');
	if (env.APP_ENV === 'production') {
		headers.set('strict-transport-security', 'max-age=63072000; includeSubDomains; preload');
	}
	const policy = headers.get('content-security-policy');
	if (policy) {
		let extended = extendPolicy(policy, {
			'connect-src': [origin(publicEnv.PUBLIC_SENTRY_DSN)],
			'img-src': [origin(publicEnv.PUBLIC_MEDIA_BASE_URL)]
		});
		if (env.APP_ENV === 'production' && !extended.includes('upgrade-insecure-requests'))
			extended += '; upgrade-insecure-requests';
		headers.set('content-security-policy', extended);
	}
}

export const handle: Handle = async ({ event, resolve }) => {
	const started = performance.now();
	const incoming = event.request.headers.get('x-request-id') ?? '';
	event.locals.requestId = requestIdPattern.test(incoming) ? incoming : crypto.randomUUID().replaceAll('-', '');
	const response = await resolve(event);
	response.headers.set('x-request-id', event.locals.requestId);
	// robots.txt only asks crawlers to stay out; this keeps private pages out of results even when linked.
	if (privatePath.test(event.url.pathname)) {
		try {
			response.headers.set('x-robots-tag', 'noindex, nofollow');
		} catch {
			// Proxied API responses have immutable headers.
		}
	}
	try {
		securityHeaders(response.headers);
	} catch {
		// Responses from fetch() have immutable headers; those are proxied API bytes, not pages.
	}
	const route = event.route.id ?? 'unmatched';
	if (route !== '/health') {
		log(response.status >= 500 ? 'warn' : 'info', 'request', {
			request_id: event.locals.requestId,
			method: event.request.method,
			route,
			status: response.status,
			duration_ms: Math.round(performance.now() - started)
		});
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
		log('error', 'unhandled server error', {
			request_id: reference,
			route: event.route.id ?? 'unmatched',
			status,
			error: error instanceof Error ? `${error.name}: ${error.message}` : String(error),
			stack: error instanceof Error ? error.stack : undefined
		});
		reportError(error, {
			dsn: env.SENTRY_DSN,
			platform: 'node',
			environment: env.APP_ENV,
			release: env.APP_RELEASE,
			tags: { route: event.route.id ?? 'unmatched', status: String(status), request_id: reference }
		});
	}
	return { message: status >= 500 ? 'Something went wrong on our side.' : message, reference };
};
