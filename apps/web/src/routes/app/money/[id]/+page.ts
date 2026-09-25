import { api } from '$lib/api';
import type { PageLoad } from './$types';

export type Item = {
	id: string;
	booking_id: string;
	seller: string;
	starts_at: string;
	currency: string;
	gross_minor: number;
	deduction_minor: number;
	seller_entitlement_minor: number;
	route: string;
	state: string;
	provider_reference: string | null;
	settled_at: string | null;
};

export const load: PageLoad = async ({ fetch, params }) => {
	try {
		return {
			item: await api<Item>(`/api/v1/me/settlements/${encodeURIComponent(params.id)}`, undefined, fetch),
			loadError: ''
		};
	} catch (error) {
		return {
			item: null as Item | null,
			loadError: error instanceof Error ? error.message : 'This settlement record is unavailable.'
		};
	}
};
