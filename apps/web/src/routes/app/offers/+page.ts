import { api } from '$lib/api';
import type { PageLoad } from './$types';

export type Offer = {
	id: string;
	seller: string;
	buyer_name: string;
	duration_minutes: number;
	state: string;
	version: number;
	expires_at: string;
	amount_minor: string;
	role: 'seller' | 'buyer';
	currency?: string;
};

export const load: PageLoad = async ({ fetch }) => {
	try {
		const result = await api<{ offers: Offer[]; next_cursor: string }>('/api/v1/me/offers', undefined, fetch);
		return { offers: result.offers, nextCursor: result.next_cursor, loadError: '' };
	} catch (error) {
		return {
			offers: [] as Offer[],
			nextCursor: '',
			loadError: error instanceof Error ? error.message : 'Offers could not be loaded.'
		};
	}
};
