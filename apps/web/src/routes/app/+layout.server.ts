import { redirect } from '@sveltejs/kit';
import { env } from '$env/dynamic/private';
import type { LayoutServerLoad } from './$types';

export const load: LayoutServerLoad = async ({ cookies, fetch, url }) => {
	const session = cookies.get('aside_session');
	if (!session) redirect(303, `/login?next=${encodeURIComponent(url.pathname + url.search)}`);
	const apiBase = env.API_INTERNAL_BASE || 'http://127.0.0.1:8081';
	try {
		const response = await fetch(`${apiBase}/api/v1/me`, {
			headers: { cookie: `aside_session=${encodeURIComponent(session)}` }
		});
		if (response.ok) return { account: await response.json() };
	} catch {
		// Fail closed if the identity service is unavailable.
	}
	redirect(303, `/login?next=${encodeURIComponent(url.pathname + url.search)}`);
};
