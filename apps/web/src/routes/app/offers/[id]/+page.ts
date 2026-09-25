import { api } from '$lib/api';
import type { PageLoad } from './$types';

export type Offer = {
	id: string;
	seller: string;
	seller_name?: string;
	buyer_name: string;
	duration_minutes: number;
	state: string;
	version: number;
	amount_minor: string;
	expires_at: string;
	checkout_expires_at: string | null;
	role: 'seller' | 'buyer';
	local_simulator?: boolean;
	provider_checkout_enabled?: boolean;
	timezone?: string;
	currency?: string;
};

export const load: PageLoad = async ({ fetch, params }) => {
	try {
		return {
			offer: await api<Offer>(`/api/v1/offers/${encodeURIComponent(params.id)}`, undefined, fetch),
			loadError: ''
		};
	} catch (error) {
		return {
			offer: null as Offer | null,
			loadError: error instanceof Error ? error.message : 'This offer could not be loaded.'
		};
	}
};
