import { api } from '$lib/api';

export type SuggestionResult = {
	suggestions: string[];
	wanted?: { handle: string; valid: boolean; available: boolean };
};

/** Free links for a name, and whether the typed link (if any) is free. One request. */
export function fetchHandleSuggestions(name: string, wanted = ''): Promise<SuggestionResult> {
	const query = new URLSearchParams({ name: name.trim().slice(0, 80) });
	if (wanted) query.set('want', wanted.trim().toLowerCase());
	return api<SuggestionResult>(`/api/v1/handles/suggestions?${query}`);
}
