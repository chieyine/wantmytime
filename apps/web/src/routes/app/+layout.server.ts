import { error, redirect } from '@sveltejs/kit';
import { env } from '$env/dynamic/private';
import type { LayoutServerLoad } from './$types';

// Only "not signed in" (401) sends people to the login page. Anything else,
// such as the API being unreachable or a firewall answering instead of it,
// shows an error: redirecting would bounce a signed-in person back to login,
// where the browser still has a valid session, and round again.
export const load: LayoutServerLoad = async ({ cookies, fetch, url }) => {
	const toLogin = `/login?next=${encodeURIComponent(url.pathname + url.search)}`;
	const session = cookies.get('aside_session');
	if (!session) redirect(303, toLogin);
	const apiBase = env.API_INTERNAL_BASE || 'http://127.0.0.1:8081';
	let response: Response;
	try {
		response = await fetch(`${apiBase}/api/v1/me`, {
			headers: { cookie: `aside_session=${encodeURIComponent(session)}` }
		});
	} catch (cause) {
		console.error(
			JSON.stringify({ level: 'ERROR', msg: 'account check unreachable', api: apiBase, error: String(cause) })
		);
		error(503, 'We couldn’t reach your account just now. Please try again in a minute.');
	}
	if (response.ok) return { account: await response.json() };
	if (response.status === 401) redirect(303, toLogin);
	console.error(
		JSON.stringify({
			level: 'ERROR',
			msg: 'account check failed',
			api: apiBase,
			status: response.status,
			server: response.headers.get('server')
		})
	);
	error(503, 'We couldn’t reach your account just now. Please try again in a minute.');
};
