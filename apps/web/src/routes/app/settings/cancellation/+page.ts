import { api } from '$lib/api';
import type { PageLoad } from './$types';

export type Policy = { key: string; name: string; summary: string };
type Owed = { outstanding_minor: number; max_share_bps: number; currency?: string };

export const load: PageLoad = async ({ fetch }) => {
	try {
		const [policy, owed] = await Promise.all([
			api<{ policy: string; options: Policy[] }>('/api/v1/me/cancellation-policy', undefined, fetch),
			api<Owed>('/api/v1/me/refund-recoveries', undefined, fetch)
		]);
		return { policy: policy.policy, options: policy.options, owed, loadError: '' };
	} catch (e) {
		return {
			policy: '',
			options: [] as Policy[],
			owed: null as Owed | null,
			loadError: e instanceof Error ? e.message : 'Your policy could not be loaded.'
		};
	}
};
