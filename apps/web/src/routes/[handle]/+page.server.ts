import { configuredSiteOrigin } from '$lib/site';
import { error, isRedirect, redirect } from '@sveltejs/kit';
import { env } from '$env/dynamic/private';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ params, fetch, url, setHeaders }) => {
	setHeaders({
		'cache-control': 'public, max-age=15, stale-while-revalidate=60'
	});
	const apiBase = env.API_INTERNAL_BASE || 'http://127.0.0.1:8081';
	const publicOrigin = env.PUBLIC_APP_ORIGIN ? configuredSiteOrigin() : url.origin;
	try {
		const response = await fetch(`${apiBase}/api/v1/people/${encodeURIComponent(params.handle)}`);
		if (response.status === 404) error(404, 'This link is not available.');
		if (!response.ok) {
			console.error(
				JSON.stringify({
					level: 'ERROR',
					msg: 'public page load failed',
					status: response.status,
					server: response.headers.get('server')
				})
			);
			error(503, 'We could not load this link. Please try again.');
		}
		const person = await response.json();
		// An old link (or odd capitals) lands on the one address search engines and people should keep.
		if (person.handle && person.handle !== params.handle) redirect(301, `/${person.handle}${url.search}`);
		const mediaOrigin = publicOrigin;
		person.avatar_url = person.avatar_version
			? `${mediaOrigin.replace(/\/$/, '')}/api/v1/people/${encodeURIComponent(person.handle)}/avatar?v=${person.avatar_version}`
			: '';
		person.preview_version = Math.max(1, Number(person.public_version || 1));
		return { person, publicOrigin };
	} catch (cause) {
		if (isRedirect(cause) || (cause && typeof cause === 'object' && 'status' in cause)) throw cause;
		throw error(503, 'We could not load this link. Please try again.');
	}
};
