import { isRedirect, redirect } from '@sveltejs/kit';
import { env } from '$env/dynamic/private';
import type { PageServerLoad } from './$types';

// Already signed in? Go straight on instead of asking for a code again. Checked
// here, and only when there is a session cookie, so signed-out visitors don't
// trigger a failing request in the browser.
export const load: PageServerLoad = async ({ cookies, fetch, url }) => {
	const session = cookies.get('aside_session');
	if (!session) return {};
	const wanted = url.searchParams.get('next') ?? '';
	const next = wanted.startsWith('/') && !wanted.startsWith('//') ? wanted : '/app';
	try {
		const apiBase = env.API_INTERNAL_BASE || 'http://127.0.0.1:8081';
		const response = await fetch(`${apiBase}/api/v1/me`, {
			headers: { cookie: `aside_session=${encodeURIComponent(session)}` }
		});
		if (response.ok) redirect(303, next);
	} catch (cause) {
		if (isRedirect(cause)) throw cause;
		// The API is unreachable: show the sign-in form rather than an error.
	}
	return {};
};
