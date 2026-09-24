// Minimal Sentry reporting for the browser and the Node server, with no SDK.
// Events carry the route, status, request reference and stack; never form
// values, cookies, headers or query strings.

type Target = { endpoint: string };

const cache = new Map<string, Target | null>();
const recent = new Map<string, number>();

function target(dsn: string | undefined): Target | null {
	const value = (dsn ?? '').trim();
	if (!value) return null;
	if (cache.has(value)) return cache.get(value) ?? null;
	let parsed: Target | null = null;
	try {
		const url = new URL(value);
		const path = url.pathname.replace(/^\/+|\/+$/g, '');
		const slash = path.lastIndexOf('/');
		const prefix = slash >= 0 ? `/${path.slice(0, slash)}` : '';
		const project = slash >= 0 ? path.slice(slash + 1) : path;
		if (url.username && project) {
			parsed = { endpoint: `${url.protocol}//${url.host}${prefix}/api/${project}/envelope/?sentry_key=${encodeURIComponent(url.username)}&sentry_version=7` };
		}
	} catch {
		parsed = null;
	}
	cache.set(value, parsed);
	return parsed;
}

function eventId(): string {
	const bytes = new Uint8Array(16);
	crypto.getRandomValues(bytes);
	return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
}

function frames(stack: string | undefined) {
	if (!stack) return undefined;
	const lines = stack.split('\n').slice(1, 40);
	const parsed = lines
		.map((line) => {
			const match = line.match(/at (?:(.+?) \()?(.+?):(\d+):(\d+)\)?$/) ?? line.match(/^(.*)@(.+?):(\d+):(\d+)$/);
			if (!match) return null;
			return { function: match[1] || '?', filename: match[2].split('?')[0], lineno: Number(match[3]), colno: Number(match[4]), in_app: !match[2].includes('node_modules') };
		})
		.filter(Boolean);
	return parsed.length ? { frames: parsed.reverse() } : undefined;
}

export type ReportOptions = {
	dsn: string | undefined;
	platform: 'javascript' | 'node';
	environment?: string;
	release?: string;
	tags?: Record<string, string>;
};

/** Sends an error to Sentry. Never throws and never blocks the caller. */
export function reportError(error: unknown, options: ReportOptions): void {
	const t = target(options.dsn);
	if (!t) return;
	const err = error instanceof Error ? error : new Error(typeof error === 'string' ? error : 'Non-error value thrown');
	const fingerprint = `${err.name}|${err.message}|${options.tags?.route ?? ''}`;
	const now = Date.now();
	if ((recent.get(fingerprint) ?? 0) > now - 60_000) return;
	recent.set(fingerprint, now);
	if (recent.size > 500) recent.clear();
	const id = eventId();
	const event = {
		event_id: id,
		timestamp: new Date(now).toISOString(),
		level: 'error',
		platform: options.platform,
		logger: options.platform === 'node' ? 'aside-web-server' : 'aside-web-browser',
		environment: options.environment || 'production',
		release: options.release || undefined,
		tags: options.tags ?? {},
		exception: { values: [{ type: err.name || 'Error', value: err.message.slice(0, 1000), stacktrace: frames(err.stack) }] }
	};
	const payload = JSON.stringify(event);
	const body = `${JSON.stringify({ event_id: id, sent_at: new Date(now).toISOString() })}\n${JSON.stringify({ type: 'event' })}\n${payload}\n`;
	try {
		void fetch(t.endpoint, { method: 'POST', body, headers: { 'Content-Type': 'text/plain;charset=UTF-8' }, keepalive: true }).catch(() => {});
	} catch {
		// Reporting must never affect the page or the request.
	}
}
