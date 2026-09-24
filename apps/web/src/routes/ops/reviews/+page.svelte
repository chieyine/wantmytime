<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';

	type Review = { id: string; booking_id: string; seller: string; reviewer: string; rating: number; body: string; seller_reply: string; created_at: string; hidden_at: string | null; hidden_reason: string };
	let reviews = $state<Review[]>([]);
	let message = $state('');
	let busy = $state('');
	let reasons = $state<Record<string, string>>({});

	async function load() {
		try { reviews = (await api<{ reviews: Review[] }>('/api/v1/ops/reviews')).reviews; }
		catch (e) { message = e instanceof Error ? e.message : 'Reviews could not be loaded.'; }
	}
	onMount(load);

	async function setHidden(id: string, hide: boolean) {
		busy = id;
		message = '';
		try {
			await api(`/api/v1/ops/reviews/${encodeURIComponent(id)}/${hide ? 'hide' : 'restore'}`, { method: 'POST', body: JSON.stringify({ reason: reasons[id] ?? '' }) });
			message = hide ? 'Review hidden from the public page.' : 'Review restored.';
			await load();
		} catch (e) { message = e instanceof Error ? e.message : 'The review could not be updated.'; }
		finally { busy = ''; }
	}
</script>

<svelte:head><title>Reviews — Ops · WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href="/ops">← Operations</a>
	<p class="eyebrow">Reviews</p>
	<h1 class="page-heading">Reviews.</h1>
	<p class="page-intro">Only buyers who had a session can review, once per booking. Hide a review only for abuse, personal data or clear falsehood, and record why. Both actions are audited.</p>
	{#if message}<p class="notice notice-info" aria-live="polite">{message}</p>{/if}
	{#if reviews.length === 0}<p class="page-intro">No reviews yet.</p>{/if}
	<div class="list-stack">
		{#each reviews as r (r.id)}
			<article class="list-card">
				<div>
					<strong>{'★'.repeat(r.rating)}{'☆'.repeat(5 - r.rating)} · @{r.seller}</strong>
					{#if r.body}<p>“{r.body}” — {r.reviewer}</p>{:else}<p>{r.reviewer} (no comment)</p>{/if}
					{#if r.seller_reply}<small>Reply: {r.seller_reply}</small>{/if}
					{#if r.hidden_at}<small>Hidden: {r.hidden_reason}</small>{/if}
					<div class="setup-form">
						<label>Reason<input class="field" bind:value={reasons[r.id]} /></label>
						<button class="button button-secondary" onclick={() => setHidden(r.id, !r.hidden_at)} disabled={busy === r.id || (reasons[r.id] ?? '').trim().length < 8}>{r.hidden_at ? 'Restore' : 'Hide'}</button>
					</div>
				</div>
				<span>{r.hidden_at ? 'HIDDEN' : 'PUBLIC'}</span>
			</article>
		{/each}
	</div>
</section>
