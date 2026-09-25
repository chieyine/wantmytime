<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { formatNaira } from '$lib/money';
	import CursorPager from '$lib/components/CursorPager.svelte';

	let { data } = $props();
	let ownHandle = $derived((data.account as { handle: string }).handle);
	type Booking = { id: string; seller: string; buyer: string; duration_minutes: number; starts_at: string; amount_minor: number; state: string; payment_state: string };
	let bookings = $state<Booking[]>([]);
	let loading = $state(true);
	let message = $state('');
	let nextCursor = $state('');
	let filter = $state<'attention' | 'upcoming' | 'all'>('attention');
	const upcoming = (booking: Booking) => booking.state === 'confirmed' && new Date(booking.starts_at).getTime() > Date.now();
	const needsAttention = (booking: Booking) => upcoming(booking) && booking.seller === ownHandle;
	let filtered = $derived(bookings.filter((booking) => filter === 'attention' ? needsAttention(booking) : filter === 'upcoming' ? upcoming(booking) : true).sort((a, b) => filter === 'all' ? new Date(b.starts_at).getTime() - new Date(a.starts_at).getTime() : new Date(a.starts_at).getTime() - new Date(b.starts_at).getTime()));
	async function load(cursor = '') {
		loading = true;
		message = '';
		try {
			const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : '';
			const result = await api<{ bookings: Booking[]; next_cursor: string }>(`/api/v1/me/bookings${query}`);
			bookings = cursor ? [...bookings, ...result.bookings] : result.bookings;
			nextCursor = result.next_cursor;
		} catch (error) {
			message = error instanceof Error ? error.message : 'Bookings could not be loaded.';
		} finally {
			loading = false;
		}
	}
	onMount(() => { void load(); });
	const dateLabel = (value: string) => new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value));
	function status(booking: Booking) {
		if (booking.payment_state === 'simulated') return 'LOCAL SIMULATION';
		if (booking.state === 'confirmed') return upcoming(booking) ? 'UPCOMING' : 'CONFIRMED';
		return booking.state.replaceAll('_', ' ').toUpperCase();
	}
</script>

<svelte:head><title>Bookings — WantMyTime</title></svelte:head>
<section class="form-page app-page workspace-records">
	<a class="back-link" href="/app">← Overview</a>
	<p class="eyebrow">Bookings / your time</p>
	<h1 class="page-heading">YOUR<br />CONVERSATIONS.</h1>
	<p class="page-intro">Find the meeting that needs your attention and open its private details.</p>
	<div class="record-filter" role="group" aria-label="Filter loaded bookings"><button type="button" class:active={filter === 'attention'} aria-pressed={filter === 'attention'} onclick={() => filter = 'attention'}>YOUR UPCOMING</button><button type="button" class:active={filter === 'upcoming'} aria-pressed={filter === 'upcoming'} onclick={() => filter = 'upcoming'}>ALL UPCOMING</button><button type="button" class:active={filter === 'all'} aria-pressed={filter === 'all'} onclick={() => filter = 'all'}>ALL LOADED</button></div>
	{#if loading && !bookings.length}<p class="page-intro">Loading your bookings…</p>
	{:else if message && !bookings.length}<div class="notice notice-warning" role="alert">{message}</div>
	{:else if bookings.length === 0}<div class="empty-state"><span class="empty-line"></span><h2>YOUR FIRST BOOKING WILL APPEAR HERE.</h2><p>When someone books your link, you will see the time and private details here.</p><a href="/app/share" class="text-link">SHARE YOUR LINK ↗</a></div>
	{:else}
		{#if filtered.length}<div class="record-list" aria-live="polite">{#each filtered as booking (booking.id)}<a class="record-row" href={`/app/bookings/${encodeURIComponent(booking.id)}`}><div class="record-row-date"><strong>{new Intl.DateTimeFormat(undefined, { day: '2-digit' }).format(new Date(booking.starts_at))}</strong><span>{new Intl.DateTimeFormat(undefined, { month: 'short' }).format(new Date(booking.starts_at)).toUpperCase()}</span></div><div class="record-row-main"><span class="record-status" class:urgent={needsAttention(booking)}>{status(booking)}</span><h2>{booking.seller === ownHandle ? booking.buyer : booking.seller}</h2><p>{dateLabel(booking.starts_at)} · {booking.duration_minutes} min</p></div><div class="record-row-end"><strong>{formatNaira(booking.amount_minor)}</strong><span>{needsAttention(booking) ? 'OPEN BOOKING' : 'VIEW DETAILS'} ↗</span></div></a>{/each}</div>
		{:else}<div class="record-filter-empty"><h2>{filter === 'attention' ? 'NO UPCOMING BOOKINGS ON THIS PAGE.' : 'NO BOOKINGS IN THIS VIEW.'}</h2><p>Nothing here. Older bookings may be further down: try All or load more.</p></div>{/if}
		{#if message}<p class="notice notice-warning" role="alert">{message}</p>{/if}
		<CursorPager cursor={nextCursor} busy={loading} onNext={() => load(nextCursor)} />
	{/if}
</section>
