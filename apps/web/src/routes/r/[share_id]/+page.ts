import { error, redirect } from '@sveltejs/kit';
import type { PageLoad } from './$types';
export const load: PageLoad = ({ params }) => {
	const handle = params.share_id.replace(/^s-/, '');
	if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(handle)) throw error(404, 'Link unavailable.');
	throw redirect(302, `/${encodeURIComponent(handle)}`);
};
