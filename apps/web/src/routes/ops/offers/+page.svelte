<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import CursorPager from '$lib/components/CursorPager.svelte';
	type Offer = {
		id: string;
		seller: string;
		buyer_name: string;
		duration_minutes: number;
		state: string;
		expires_at: string;
		created_at: string;
	};
	let offers = $state<Offer[]>([]);
	let message = $state('');
	let cursor = $state('');
	let loading = $state(false);
	async function load(after = '') {
		loading = true;
		try {
			const q = after ? `?cursor=${encodeURIComponent(after)}` : '';
			const page = await api<{ offers: Offer[]; next_cursor: string }>(`/api/v1/ops/offers${q}`);
			offers = after ? [...offers, ...page.offers] : page.offers;
			cursor = page.next_cursor;
		} catch (e) {
			message = e instanceof Error ? e.message : 'Offers could not be loaded.';
		} finally {
			loading = false;
		}
	}
	onMount(() => load());
</script>

<svelte:head><title>Offers — Ops · WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href="/ops">← Operations</a>
	<p class="eyebrow">Offers</p>
	<h1 class="page-heading">Offer activity.</h1>
	{#if message}<p class="notice notice-warning">{message}</p>{/if}{#if offers.length}<div class="list-stack">
			{#each offers as offer (offer.id)}<article class="list-card">
					<div>
						<strong>{offer.buyer_name} → {offer.seller}</strong>
						<p>{offer.duration_minutes} minutes · {offer.state.replaceAll('_', ' ')}</p>
						<small>Expires {new Date(offer.expires_at).toLocaleString()}</small>
					</div>
				</article>{/each}
		</div>
		<CursorPager {cursor} busy={loading} onNext={() => load(cursor)} />{:else if !loading && !message}<p
			class="page-intro"
		>
			No offers have been submitted.
		</p>{:else}<p class="page-intro">Loading offers…</p>{/if}
</section>
