<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { formatNaira } from '$lib/money';
	import CursorPager from '$lib/components/CursorPager.svelte';

	type Item = { id: string; booking_id: string; seller: string; starts_at: string; currency: string; gross_minor: number; deduction_minor: number; seller_entitlement_minor: number; route: string; state: string; provider_reference: string | null; settled_at: string | null };
	let items = $state<Item[]>([]);
	let loading = $state(true);
	let message = $state('');
	let nextCursor = $state('');
	let collectionEnabled = $state<boolean | null>(null);
	let filter = $state<'awaiting' | 'settled' | 'all'>('awaiting');
	let awaiting = $derived(items.filter((item) => !item.settled_at));
	let settled = $derived(items.filter((item) => !!item.settled_at));
	let filtered = $derived(items.filter((item) => filter === 'awaiting' ? !item.settled_at : filter === 'settled' ? !!item.settled_at : true));
	async function load(cursor = '') {
		loading = true;
		message = '';
		try {
			const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : '';
			const result = await api<{ settlements: Item[]; next_cursor: string; payment_collection_enabled: boolean }>(`/api/v1/me/settlements${query}`);
			items = cursor ? [...items, ...result.settlements] : result.settlements;
			nextCursor = result.next_cursor;
			collectionEnabled = result.payment_collection_enabled;
		} catch (error) {
			message = error instanceof Error ? error.message : 'Settlement records could not be loaded.';
		} finally {
			loading = false;
		}
	}
	onMount(() => { void load(); });
	const dateLabel = (value: string) => new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(new Date(value));
</script>

<svelte:head><title>Money — WantMyTime</title></svelte:head>
<section class="form-page app-page workspace-money">
	<a class="back-link" href="/app">← Overview</a>
	<p class="eyebrow">Money / verified records</p>
	<h1 class="page-heading">KNOW WHERE<br />IT STANDS.</h1>
	<p class="page-intro">A payment allocation records your share of a verified booking. It becomes a confirmed settlement only when matching provider evidence is recorded.</p>
	{#if collectionEnabled === false}<div class="notice notice-warning">Live payment collection is currently disabled. Local simulations are not earnings.</div>{/if}
	{#if loading && !items.length}<p class="page-intro">Loading payment records…</p>
	{:else if message && !items.length}<div class="notice notice-warning" role="alert">{message}</div>
	{:else if items.length === 0}<div class="money-empty"><p class="workspace-kicker">NO VERIFIED ALLOCATIONS YET</p><h2>NOTHING TO SETTLE.</h2><p>When a provider-verified paid booking creates an allocation, its status will appear here.</p><div class="money-stages"><div><span>01</span><strong>Payment verified</strong><p>A provider confirms the booking payment.</p></div><div><span>02</span><strong>Allocation recorded</strong><p>Your share is recorded. This is not a bank transfer.</p></div><div><span>03</span><strong>Settlement matched</strong><p>Provider evidence confirms the settlement.</p></div></div></div>
	{:else}
		<div class="money-status-grid" aria-label="Status of loaded settlement records"><div><span>ALLOCATIONS LOADED</span><strong>{items.length}</strong><small>Verified payment allocations</small></div><div><span>AWAITING PROVIDER EVIDENCE</span><strong>{awaiting.length}</strong><small>Not confirmed settled</small></div><div><span>PROVIDER-CONFIRMED</span><strong>{settled.length}</strong><small>Matched settlement evidence</small></div></div>
		<div class="record-filter" role="group" aria-label="Filter loaded settlement records"><button type="button" class:active={filter === 'awaiting'} aria-pressed={filter === 'awaiting'} onclick={() => filter = 'awaiting'}>AWAITING</button><button type="button" class:active={filter === 'settled'} aria-pressed={filter === 'settled'} onclick={() => filter = 'settled'}>SETTLED</button><button type="button" class:active={filter === 'all'} aria-pressed={filter === 'all'} onclick={() => filter = 'all'}>ALL LOADED</button></div>
		{#if filtered.length}<div class="record-list" aria-live="polite">{#each filtered as item (item.id)}<a class="record-row" href={`/app/money/${encodeURIComponent(item.id)}`}><div class="record-row-date"><strong>{new Intl.DateTimeFormat(undefined, { day: '2-digit' }).format(new Date(item.starts_at))}</strong><span>{new Intl.DateTimeFormat(undefined, { month: 'short' }).format(new Date(item.starts_at)).toUpperCase()}</span></div><div class="record-row-main"><span class="record-status" class:urgent={!item.settled_at}>{item.settled_at ? 'PROVIDER-CONFIRMED SETTLED' : 'AWAITING SETTLEMENT EVIDENCE'}</span><h2>{formatNaira(item.seller_entitlement_minor)}</h2><p>Booking {dateLabel(item.starts_at)} · {item.route.replaceAll('_', ' ')}</p></div><div class="record-row-end"><strong>{item.settled_at ? dateLabel(item.settled_at) : 'NOT SETTLED'}</strong><span>VIEW EVIDENCE ↗</span></div></a>{/each}</div>
		{:else}<div class="record-filter-empty"><h2>NO RECORDS IN THIS VIEW.</h2><p>Other records may be on older pages. Use All loaded or load more below.</p></div>{/if}
		{#if message}<p class="notice notice-warning" role="alert">{message}</p>{/if}
		<CursorPager cursor={nextCursor} busy={loading} onNext={() => load(nextCursor)} />
	{/if}
</section>
