import { error, isRedirect, redirect } from '@sveltejs/kit';
import { env } from '$env/dynamic/private';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ params, fetch, url }) => {
	const apiBase = env.API_INTERNAL_BASE || 'http://127.0.0.1:8081';
	const publicOrigin = env.PUBLIC_APP_ORIGIN || url.origin;
	try {
		const response = await fetch(`${apiBase}/api/v1/people/${encodeURIComponent(params.handle)}`);
		if (!response.ok) throw error(response.status as 404, 'This link is not available.');
		const person = await response.json();
		// An old link (or odd capitals) lands on the one address search engines and people should keep.
		if (person.handle && person.handle !== params.handle) redirect(301, `/${person.handle}${url.search}`);
		const mediaOrigin = env.PUBLIC_APP_ORIGIN || url.origin;
		person.avatar_url = person.avatar_version ? `${mediaOrigin.replace(/\/$/, '')}/api/v1/people/${encodeURIComponent(person.handle)}/avatar?v=${person.avatar_version}` : '';
		person.preview_version = Math.max(1, Number(person.public_version || 1));
		return { person, publicOrigin };
	} catch (cause) {
		if (isRedirect(cause) || (cause && typeof cause === 'object' && 'status' in cause)) throw cause;
		throw error(503, 'We could not load this link. Please try again.');
	}
};
