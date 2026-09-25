import { api } from '$lib/api';
import type { PageLoad } from './$types';

export type Profile = {
	handle: string;
	name: string;
	identity_url: string;
	mode: 'fixed' | 'offer' | 'both';
	base_30_minor: number;
	durations: number[];
	timezone: string;
	paused: boolean;
	ready: boolean;
	avatar_version?: number;
	currency?: string;
};

export const load: PageLoad = async ({ fetch }) => {
	try {
		return { profile: await api<Profile>('/api/v1/me/link', undefined, fetch), loadError: '' };
	} catch (error) {
		const text = error instanceof Error ? error.message : '';
		// No link yet is a normal state: the page offers to create one.
		return {
			profile: null as Profile | null,
			loadError: text.includes('not claimed') ? '' : text || 'Your link could not be loaded.'
		};
	}
};
