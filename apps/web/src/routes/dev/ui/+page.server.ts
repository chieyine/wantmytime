import { error } from '@sveltejs/kit';
import { dev } from '$app/environment';
import type { PageServerLoad } from './$types';

// The component gallery exists only in development builds.
export const load: PageServerLoad = () => {
	if (!dev) error(404, 'Not found');
};
