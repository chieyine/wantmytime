<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { formatMoney } from '$lib/money';
	import CursorPager from '$lib/components/CursorPager.svelte';

	type Offer = { id: string; seller: string; buyer_name: string; duration_minutes: number; state: string; version: number; expires_at: string; amount_minor: string; role: 'seller' | 'buyer'; currency?: string };
	let offers = $state<Offer[]>([]);
	let loading = $state(true);
	let message = $state('');
	let nextCursor = $state('');
	let filter = $state<'respond' | 'active' | 'all'>('respond');
	const canRespond = (offer: Offer) => (offer.role === 'seller' && offer.state === 'pending' || offer.role === 'buyer' && offer.state === 'countered') && new Date(offer.expires_at).getTime() > Date.now();
	const active = (offer: Offer) => offer.state === 'agreed' || ['pending', 'countered'].includes(offer.state) && new Date(offer.expires_at).getTime() > Date.now();
	let filtered = $derived(offers.filter((offer) => filter === 'respond' ? canRespond(offer) : filter === 'active' ? active(offer) : true).sort((a, b) => {
		const priority = Number(canRespond(b)) - Number(canRespond(a));
		return priority || new Date(a.expires_at).getTime() - new Date(b.expires_at).getTime();
	}));
	async function load(cursor = '') {
		loading = true;
		message = '';
		try {
			const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : '';
			const result = await api<{ offers: Offer[]; next_cursor: string }>(`/api/v1/me/offers${query}`);
			offers = cursor ? [...offers, ...result.offers] : result.offers;
			nextCursor = result.next_cursor;
		} catch (error) {
			message = error instanceof Error ? error.message : 'Offers could not be loaded.';
		} finally {
			loading = false;
		}
	}
	onMount(() => { void load(); });
	const dateLabel = (value: string) => new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(new Date(value));
	function nextAction(offer: Offer) {
		if (canRespond(offer)) return offer.role === 'seller' ? 'RESPOND TO OFFER' : 'REVIEW COUNTEROFFER';
		if (offer.state === 'agreed') return 'VIEW AGREEMENT';
		return 'VIEW OFFER';
	}
	function status(offer: Offer) {
		if (canRespond(offer)) return 'ANSWER NEEDED';
		if (['pending', 'countered'].includes(offer.state) && new Date(offer.expires_at).getTime() <= Date.now()) return 'RESPONSE WINDOW PASSED';
		return offer.state.replaceAll('_', ' ').toUpperCase();
	}
</script>

<svelte:head><title>Offers — WantMyTime</title></svelte:head>
<section class="form-page app-page workspace-records">
	<a class="back-link" href="/app">← Overview</a>
	<p class="eyebrow">Offers / requests</p>
	<h1 class="page-heading">REQUESTS<br />FOR YOUR TIME.</h1>
	<p class="page-intro">When you let people name a price, their offers land here. Accept one and they get a link to pick a time and pay.</p>
	<div class="record-filter" role="group" aria-label="Filter loaded offers"><button type="button" class:active={filter === 'respond'} aria-pressed={filter === 'respond'} onclick={() => filter = 'respond'}>TO RESPOND</button><button type="button" class:active={filter === 'active'} aria-pressed={filter === 'active'} onclick={() => filter = 'active'}>ACTIVE</button><button type="button" class:active={filter === 'all'} aria-pressed={filter === 'all'} onclick={() => filter = 'all'}>ALL LOADED</button></div>
	{#if loading && !offers.length}<p class="page-intro">Loading your offers…</p>
	{:else if message && !offers.length}<div class="notice notice-warning" role="alert">{message}</div>
	{:else if offers.length === 0}<div class="empty-state"><span class="empty-line"></span><h2>NO OFFERS YET.</h2><p>Offers people send you will show up here.</p></div>
	{:else}
		{#if filtered.length}<div class="record-list" aria-live="polite">{#each filtered as offer (offer.id)}<a class="record-row" href={`/app/offers/${encodeURIComponent(offer.id)}`}><div class="record-row-date"><strong>{offer.duration_minutes}</strong><span>MIN</span></div><div class="record-row-main"><span class="record-status" class:urgent={canRespond(offer)}>{status(offer)}</span><h2>{offer.role === 'seller' ? offer.buyer_name : offer.seller}</h2><p>{offer.role === 'seller' ? 'Received' : 'Sent'} · {offer.state === 'agreed' ? 'agreement saved' : `response deadline ${dateLabel(offer.expires_at)}`}</p></div><div class="record-row-end"><strong>{formatMoney(Number(offer.amount_minor), offer.currency)}</strong><span>{nextAction(offer)} ↗</span></div></a>{/each}</div>
		{:else}<div class="record-filter-empty"><h2>{filter === 'respond' ? 'YOU ARE ALL CAUGHT UP.' : 'NO OFFERS IN THIS VIEW.'}</h2><p>Nothing here. Older offers may be further down: try All or load more.</p></div>{/if}
		{#if message}<p class="notice notice-warning" role="alert">{message}</p>{/if}
		<CursorPager cursor={nextCursor} busy={loading} onNext={() => load(nextCursor)} />
	{/if}
</section>
