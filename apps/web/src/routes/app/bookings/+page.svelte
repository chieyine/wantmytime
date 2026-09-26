<script lang="ts">
	import { api } from '$lib/api';
	import { formatMoney } from '$lib/money';
	import CursorPager from '$lib/components/CursorPager.svelte';

	let { data } = $props();
	let ownHandle = $derived((data.account as { handle: string }).handle);
	import type { Booking, Offer } from './+page';
	// The first page arrives with the route; "load more" appends to it.
	let bookings = $derived(data.bookings);
	let loading = $state(false);
	let message = $derived(data.loadError);
	let nextCursor = $derived(data.nextCursor);
	let filter = $state<'upcoming' | 'past'>('upcoming');
	const upcoming = (booking: Booking) =>
		booking.state === 'confirmed' && new Date(booking.starts_at).getTime() > Date.now();
	const needsAttention = (booking: Booking) => upcoming(booking) && booking.seller === ownHandle;
	let filtered = $derived(
		bookings
			.filter((booking) => (filter === 'upcoming' ? upcoming(booking) : !upcoming(booking)))
			.sort((a, b) =>
				filter === 'past'
					? new Date(b.starts_at).getTime() - new Date(a.starts_at).getTime()
					: new Date(a.starts_at).getTime() - new Date(b.starts_at).getTime()
			)
	);
	// Requests: offers to answer, and agreed prices waiting for a time.
	let offers = $derived(data.offers);
	const live = (offer: Offer) => new Date(offer.expires_at).getTime() > Date.now();
	const canRespond = (offer: Offer) =>
		((offer.role === 'seller' && offer.state === 'pending') ||
			(offer.role === 'buyer' && offer.state === 'countered')) &&
		live(offer);
	const open = (offer: Offer) =>
		offer.state === 'agreed' || (['pending', 'countered'].includes(offer.state) && live(offer));
	let openRequests = $derived(
		offers
			.filter(open)
			.sort(
				(a, b) =>
					Number(canRespond(b)) - Number(canRespond(a)) ||
					new Date(a.expires_at).getTime() - new Date(b.expires_at).getTime()
			)
	);
	let pastRequests = $derived(offers.filter((offer) => !open(offer)));
	function requestStatus(offer: Offer) {
		if (canRespond(offer)) return offer.role === 'seller' ? 'ANSWER NEEDED' : 'COUNTEROFFER';
		if (offer.state === 'agreed') return 'AGREED';
		if (['pending', 'countered'].includes(offer.state) && !live(offer)) return 'EXPIRED';
		return offer.state.replaceAll('_', ' ').toUpperCase();
	}
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
	const dateLabel = (value: string) =>
		new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value));
	function status(booking: Booking) {
		if (booking.payment_state === 'simulated') return 'LOCAL SIMULATION';
		if (booking.state === 'confirmed') return upcoming(booking) ? 'UPCOMING' : 'CONFIRMED';
		return booking.state.replaceAll('_', ' ').toUpperCase();
	}
</script>

<svelte:head><title>Bookings — WantMyTime</title></svelte:head>
<section class="form-page app-page workspace-records">
	<p class="eyebrow">Bookings</p>
	<h1 class="page-heading">YOUR BOOKINGS.</h1>
	{#snippet requestRow(offer: Offer)}<a class="record-row" href={`/app/offers/${encodeURIComponent(offer.id)}`}
			><div class="record-row-date"><strong>{offer.duration_minutes}</strong><span>MIN</span></div>
			<div class="record-row-main">
				<span class="record-status" class:urgent={canRespond(offer)}>{requestStatus(offer)}</span>
				<h2>{offer.role === 'seller' ? offer.buyer_name : offer.seller}</h2>
				<p>
					{canRespond(offer)
						? `Answer by ${new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(new Date(offer.expires_at))}`
						: offer.state === 'agreed'
							? 'Price agreed · waiting for a time'
							: offer.state === 'countered'
								? offer.role === 'seller'
									? 'You countered'
									: 'They countered'
								: offer.role === 'seller'
									? 'Offer received'
									: 'Offer sent'}
				</p>
			</div>
			<div class="record-row-end">
				<strong>{formatMoney(Number(offer.amount_minor), offer.currency)}</strong><span
					>{canRespond(offer) ? 'ANSWER' : 'VIEW'} ↗</span
				>
			</div></a
		>{/snippet}
	{#if openRequests.length}<section class="records-requests" aria-labelledby="requests-title">
			<h2 id="requests-title" class="records-section-title">Requests <span>{openRequests.length}</span></h2>
			<p class="form-note">People who named a price. Accept one and they get a link to pick a time and pay.</p>
			<div class="record-list">
				{#each openRequests as offer (offer.id)}{@render requestRow(offer)}{/each}
			</div>
		</section>{/if}
	<div class="record-filter" role="group" aria-label="Show bookings">
		<button
			type="button"
			class:active={filter === 'upcoming'}
			aria-pressed={filter === 'upcoming'}
			onclick={() => (filter = 'upcoming')}>UPCOMING</button
		><button
			type="button"
			class:active={filter === 'past'}
			aria-pressed={filter === 'past'}
			onclick={() => (filter = 'past')}>PAST</button
		>
	</div>
	{#if loading && !bookings.length}<p class="page-intro">Loading your bookings…</p>
	{:else if message && !bookings.length}<div class="notice notice-warning" role="alert">{message}</div>
	{:else if bookings.length === 0}<div class="empty-state">
			<span class="empty-line"></span>
			<h2>YOUR FIRST BOOKING WILL APPEAR HERE.</h2>
			<p>When someone books your link, you will see the time and private details here.</p>
			<a href="/app" class="text-link">SHARE YOUR LINK ↗</a>
		</div>
	{:else}
		{#if filtered.length}<div class="record-list" aria-live="polite">
				{#each filtered as booking (booking.id)}<a
						class="record-row"
						href={`/app/bookings/${encodeURIComponent(booking.id)}`}
						><div class="record-row-date">
							<strong
								>{new Intl.DateTimeFormat(undefined, { day: '2-digit' }).format(new Date(booking.starts_at))}</strong
							><span
								>{new Intl.DateTimeFormat(undefined, { month: 'short' })
									.format(new Date(booking.starts_at))
									.toUpperCase()}</span
							>
						</div>
						<div class="record-row-main">
							<span class="record-status" class:urgent={needsAttention(booking)}>{status(booking)}</span>
							<h2>{booking.seller === ownHandle ? booking.buyer : booking.seller}</h2>
							<p>{dateLabel(booking.starts_at)} · {booking.duration_minutes} min</p>
						</div>
						<div class="record-row-end">
							<strong>{formatMoney(booking.amount_minor, booking.currency)}</strong><span
								>{needsAttention(booking) ? 'OPEN BOOKING' : 'VIEW DETAILS'} ↗</span
							>
						</div></a
					>{/each}
			</div>
		{:else}<div class="record-filter-empty">
				<h2>{filter === 'upcoming' ? 'NOTHING COMING UP.' : 'NO PAST BOOKINGS YET.'}</h2>
				<p>
					{filter === 'upcoming'
						? 'New bookings appear here as soon as they’re paid.'
						: 'Finished and cancelled bookings appear here.'}
				</p>
			</div>{/if}
		{#if message}<p class="notice notice-warning" role="alert">{message}</p>{/if}
		<CursorPager cursor={nextCursor} busy={loading} onNext={() => load(nextCursor)} />
	{/if}
	{#if filter === 'past' && pastRequests.length}<section class="records-requests" aria-labelledby="past-requests-title">
			<h2 id="past-requests-title" class="records-section-title">Past requests</h2>
			<div class="record-list">
				{#each pastRequests as offer (offer.id)}{@render requestRow(offer)}{/each}
			</div>
		</section>{/if}
</section>
