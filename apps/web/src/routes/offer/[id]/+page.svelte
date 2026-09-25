<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { viewerTimeZone, todayIn, zoneCity, timeOnly } from '$lib/time';
	import { currencySymbol, formatMoney, parseMoneyToMinor } from '$lib/money';
	let { params } = $props();
	type Offer = { id: string; seller: string; seller_name?: string; buyer_name: string; duration_minutes: number; state: string; version: number; amount_minor: string; expires_at: string; checkout_expires_at: string | null; role: 'seller' | 'buyer'; local_simulator: boolean; provider_checkout_enabled: boolean; timezone: string; currency?: string };
	type Slot = { starts_at: string; local_label: string };
	let offer = $state<Offer | null>(null);
	let loading = $state(true);
	let busy = $state(false);
	let message = $state('');
	let counter = $state('');
	let date = $state('');
	let selected = $state('');
	let slots = $state<Slot[]>([]);
	let slotsLoading = $state(false);
	let key = $state('');
	const zone = viewerTimeZone();
	const currency = $derived(offer?.currency || 'NGN');
	const money = (minor: number | string) => formatMoney(minor, currency);
	const dateLabel = (value: string) => new Intl.DateTimeFormat(undefined, { dateStyle: 'full', timeStyle: 'short' }).format(new Date(value));

	async function load() {
		try {
			offer = await api<Offer>(`/api/v1/offers/${encodeURIComponent(params.id)}`);
			counter = String(Number(offer.amount_minor) / 100);
			if (offer.state === 'agreed' && offer.role === 'buyer' && !date) await firstFreeDay();
		} catch (e) {
			message = e instanceof Error ? e.message : 'This offer is unavailable. Open it with the email you used to make it.';
		} finally {
			loading = false;
		}
	}
	// Open on the first day with a free time, up to two weeks ahead.
	async function firstFreeDay() {
		date = todayIn(zone);
		await loadSlots();
		for (let i = 1; i < 14 && slots.length === 0; i++) {
			const next = new Date(`${date}T12:00:00Z`);
			next.setUTCDate(next.getUTCDate() + 1);
			date = next.toISOString().slice(0, 10);
			await loadSlots();
		}
	}
	async function loadSlots() {
		if (!offer || !date) return;
		selected = '';
		slotsLoading = true;
		try {
			slots = (await api<{ slots: Slot[] }>(`/api/v1/people/${encodeURIComponent(offer.seller)}/slots?date=${encodeURIComponent(date)}&duration=${offer.duration_minutes}&tz=${encodeURIComponent(zone)}`)).slots;
		} catch (e) {
			slots = [];
			message = e instanceof Error ? e.message : 'Available times could not be loaded.';
		} finally {
			slotsLoading = false;
		}
	}
	async function respond(action: 'accept' | 'counter' | 'decline' | 'withdraw') {
		if (!offer) return;
		message = '';
		const parsed = parseMoneyToMinor(counter);
		if (action === 'counter' && (parsed === null || parsed < 100n || parsed > 100000000n)) {
			message = `Enter a counteroffer between ${money(100)} and ${money(100000000)}.`;
			return;
		}
		busy = true;
		try {
			await api(`/api/v1/offers/${encodeURIComponent(offer.id)}/${action}`, { method: 'POST', body: JSON.stringify({ version: offer.version, amount_minor: parsed === null ? 0 : Number(parsed) }) });
			await load();
			message = action === 'accept' && offer?.role === 'buyer' ? 'Agreed. Now pick a time and pay to confirm the booking.' : 'Done. The other person has been emailed.';
		} catch (e) {
			message = e instanceof Error ? e.message : 'The offer could not be updated.';
		} finally {
			busy = false;
		}
	}
	async function checkout() {
		if (!offer || !selected) return;
		busy = true;
		message = '';
		try {
			if (!key) key = crypto.randomUUID() + crypto.randomUUID();
			const quote = await api<{ id: string }>(`/api/v1/offers/${encodeURIComponent(offer.id)}/checkout`, { method: 'POST', headers: { 'Idempotency-Key': key }, body: JSON.stringify({ starts_at: selected }) });
			location.href = `/checkout/${encodeURIComponent(quote.id)}`;
		} catch (e) {
			message = e instanceof Error ? e.message : 'Checkout could not be started.';
			key = '';
		} finally {
			busy = false;
		}
	}
	onMount(load);
</script>
<svelte:head><title>Offer — WantMyTime</title><meta name="robots" content="noindex,nofollow" /></svelte:head>
<section class="form-page">
	<a class="back-link" href="/">← WantMyTime</a>
	<p class="eyebrow">Your offer</p>
	<h1 class="page-heading">{offer?.state === 'agreed' ? 'Agreed. Pick a time.' : 'Your offer.'}</h1>
	{#if loading}<p class="page-intro">Loading your offer…</p>
	{:else if !offer}<div class="notice notice-warning">{message}<p><a href={`/access?next=${encodeURIComponent(`/offer/${params.id}`)}`}>Open it with your email ↗</a></p></div>
	{:else}
		<div class="appointment-slip"><div><small>{offer.role === 'seller' ? 'FROM' : 'TO'}</small><strong>{offer.role === 'seller' ? offer.buyer_name : offer.seller_name || offer.seller}</strong></div><div><small>LENGTH</small><strong>{offer.duration_minutes} minutes</strong></div><div><small>AMOUNT</small><strong>{money(offer.amount_minor)}</strong></div><div><small>STATUS</small><strong>{offer.state.replaceAll('_', ' ')}</strong></div></div>
		{#if offer.state === 'pending' && offer.role === 'seller'}
			<div class="setup-form"><button class="button" onclick={() => respond('accept')} disabled={busy}>Accept offer</button><label>Counter once<div class="money-input"><span>{currencySymbol(currency).trim()}</span><input bind:value={counter} inputmode="decimal" /></div></label><button class="button button-secondary" onclick={() => respond('counter')} disabled={busy}>Send counteroffer</button><button class="text-link" onclick={() => respond('decline')} disabled={busy}>Decline</button></div>
		{:else if offer.role === 'buyer' && (offer.state === 'pending' || offer.state === 'countered')}
			{#if offer.state === 'countered'}
				<p class="page-intro">{offer.seller_name || offer.seller} came back with {money(offer.amount_minor)}. Accept it to pick a time and pay, or decline. Respond by {dateLabel(offer.expires_at)}.</p>
				<div class="setup-form"><button class="button" onclick={() => respond('accept')} disabled={busy}>Accept {money(offer.amount_minor)}</button><button class="button button-secondary" onclick={() => respond('decline')} disabled={busy}>Decline</button></div>
			{:else}
				<p class="page-intro">Waiting for {offer.seller_name || offer.seller} to answer. We’ll email you. Nothing has been charged.</p>
			{/if}
			<button class="text-link" onclick={() => respond('withdraw')} disabled={busy}>Withdraw offer</button>
		{:else if offer.state === 'agreed' && offer.role === 'buyer'}
			<p class="page-intro">Pick a time{offer.checkout_expires_at ? ` by ${dateLabel(offer.checkout_expires_at)}` : ''} and pay {money(offer.amount_minor)} to confirm it. Times are in your timezone ({zoneCity(zone)}).</p>
			<div class="setup-form">
				<label>Date<input class="field" type="date" bind:value={date} onchange={loadSlots} /></label>
				{#if slotsLoading}<p class="form-note">Finding available times…</p>
				{:else if slots.length}<fieldset><legend>Available times</legend><div class="slot-list">{#each slots as slot}<button type="button" class="slot-option" class:selected={selected === slot.starts_at} onclick={() => { selected = slot.starts_at; key = ''; }}>{timeOnly(slot.starts_at, zone)}</button>{/each}</div></fieldset>
				{:else}<p class="form-note">Nothing free on this day. Try another.</p>{/if}
				<button class="button" onclick={checkout} disabled={!selected || busy || (!offer.local_simulator && !offer.provider_checkout_enabled)}>{busy ? 'Holding your time…' : 'Continue to payment'}</button>
				{#if !offer.local_simulator && !offer.provider_checkout_enabled}<p class="form-note">Payments for this seller aren’t switched on yet. Your agreed price is saved.</p>{/if}
				{#if offer.local_simulator}<p class="form-note">Test setup: no money moves.</p>{/if}
			</div>
		{:else if offer.state === 'converted'}
			<p class="page-intro">This offer is booked. The details are in your booking confirmation email.</p>
		{:else if offer.state === 'expired' || offer.state === 'declined' || offer.state === 'withdrawn'}
			<p class="page-intro">This offer is closed. <a class="text-link" href={`/${offer.seller}`}>Make a new one</a>.</p>
		{/if}
		{#if message}<p class="notice notice-info" aria-live="polite">{message}</p>{/if}
	{/if}
</section>
