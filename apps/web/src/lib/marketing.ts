import { api } from '$lib/api';

export { marketingWording } from './marketing-wording';

export type MarketingSource = 'seller_signup' | 'booking' | 'offer' | 'settings';

/** Saves the choice for whoever is signed in (a seller, or a buyer's booking session). */
export async function saveMarketingChoice(subscribed: boolean, source: MarketingSource) {
	await api('/api/v1/me/marketing', { method: 'PUT', body: JSON.stringify({ subscribed, source }) });
}
