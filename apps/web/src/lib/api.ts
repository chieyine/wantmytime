// Browser traffic stays same-origin so HttpOnly sessions work through the
// reverse proxy. Server-side loads call the API themselves with
// `$env/dynamic/private` API_INTERNAL_BASE; this helper is for browser code,
// including universal load functions in the workspace, which render in the browser.
const API = import.meta.env.VITE_API_BASE ?? '';

const cache = new Map<string, { data: unknown; expires: number }>();
const CACHE_TTL_MS = 20_000;

export function clearApiCache(prefix?: string) {
	if (prefix) {
		for (const key of cache.keys()) {
			if (key.startsWith(prefix)) cache.delete(key);
		}
	} else {
		cache.clear();
	}
}

// Load functions pass SvelteKit's fetch, so a page's data can be preloaded on hover/tap.
export async function api<T>(path: string, init?: RequestInit, fetcher: typeof fetch = fetch): Promise<T> {
	const method = (init?.method ?? 'GET').toUpperCase();
	const isGet = method === 'GET';
	const isBrowser = typeof window !== 'undefined';
	const noCache = init?.headers && new Headers(init.headers).get('Cache-Control') === 'no-cache';

	if (isBrowser && isGet && !noCache) {
		const cached = cache.get(path);
		if (cached && cached.expires > Date.now()) {
			return cached.data as T;
		}
	}

	const response = await fetcher(`${API}${path}`, {
		...init,
		headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) }
	});
	let body: unknown = null;
	if (response.status !== 204) {
		const text = await response.text();
		if (text) {
			try {
				body = JSON.parse(text);
			} catch {
				// A gateway error page (for example a 502 from Nginx) is not JSON.
				if (!response.ok) throw new Error('The service is temporarily unavailable. Please try again.');
				throw new Error('The service returned an unexpected response. Please try again.');
			}
		}
	}
	if (!response.ok) {
		const message =
			(body as { error?: { message?: string } } | null)?.error?.message ?? 'Something went wrong. Please try again.';
		// Server errors carry a reference that matches the API logs and error reports.
		const reference = response.status >= 500 ? response.headers.get('x-request-id') : null;
		throw new Error(reference && !message.includes(reference) ? `${message} (Reference: ${reference})` : message);
	}

	if (isBrowser) {
		if (isGet) {
			cache.set(path, { data: body, expires: Date.now() + CACHE_TTL_MS });
		} else {
			clearApiCache();
		}
	}

	return body as T;
}

export { API };
