<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { currencySymbol, formatMoney, parseMoneyToMinor } from '$lib/money';
	import { viewerTimeZone, todayIn, timeOnly } from '$lib/time';
	let { params } = $props();
	type Offer = {
		id: string;
		seller: string;
		seller_name?: string;
		buyer_name: string;
		duration_minutes: number;
		state: string;
		version: number;
		amount_minor: string;
		expires_at: string;
		checkout_expires_at: string | null;
		role: 'seller' | 'buyer';
		local_simulator?: boolean;
		provider_checkout_enabled?: boolean;
		timezone?: string;
		currency?: string;
	};
	let offer = $state<Offer | null>(null);
	let loading = $state(true);
	let busy = $state(false);
	let message = $state('');
	let counterAmount = $state('');
	let day = $state('');
	let selected = $state('');
	let checkoutKey = $state('');
	let slots = $state<{ starts_at: string; local_label: string }[]>([]);
	const zone = viewerTimeZone();
	const currency = $derived(offer?.currency || 'NGN');
	const money = (minor: number | string) => formatMoney(minor, currency);
	async function load() {
		try {
			offer = await api<Offer>(`/api/v1/offers/${encodeURIComponent(params.id)}`);
			counterAmount = String(Number(offer.amount_minor) / 100);
			if (
				offer.state === 'agreed' &&
				offer.role === 'buyer' &&
				(offer.local_simulator || offer.provider_checkout_enabled) &&
				!day
			) {
				day = todayIn(zone);
				await loadSlots();
			}
		} catch (error) {
			message = error instanceof Error ? error.message : 'This offer could not be loaded.';
		} finally {
			loading = false;
		}
	}
	async function loadSlots() {
		if (!day || !offer) return;
		selected = '';
		checkoutKey = '';
		try {
			slots = (
				await api<{ slots: { starts_at: string; local_label: string }[] }>(
					`/api/v1/people/${encodeURIComponent(offer.seller)}/slots?date=${encodeURIComponent(day)}&duration=${offer.duration_minutes}&tz=${encodeURIComponent(zone)}`
				)
			).slots;
		} catch (error) {
			slots = [];
			message = error instanceof Error ? error.message : 'Times could not be loaded.';
		}
	}
	onMount(load);
	async function act(action: 'accept' | 'counter' | 'decline' | 'withdraw') {
		if (!offer) return;
		message = '';
		const parsed = parseMoneyToMinor(counterAmount);
		if (action === 'counter' && (parsed === null || parsed < 100n || parsed > 100000000n)) {
			message = `Enter a counteroffer between ${money(100)} and ${money(100000000)}.`;
			return;
		}
		busy = true;
		try {
			await api(`/api/v1/offers/${encodeURIComponent(offer.id)}/${action}`, {
				method: 'POST',
				body: JSON.stringify({ version: offer.version, amount_minor: parsed === null ? 0 : Number(parsed) })
			});
			await load();
			message =
				action === 'accept' && offer?.role === 'seller'
					? `Accepted. ${offer.buyer_name} has been emailed a link to pick a time and pay; you’ll get a booking email when they do.`
					: 'Done. The other person has been emailed.';
		} catch (error) {
			message = error instanceof Error ? error.message : 'This offer could not be updated.';
		} finally {
			busy = false;
		}
	}
	async function checkout() {
		if (!offer || !selected) return;
		busy = true;
		message = '';
		try {
			if (!checkoutKey) checkoutKey = crypto.randomUUID() + crypto.randomUUID();
			const q = await api<{ id: string }>('/api/v1/offers/' + encodeURIComponent(offer.id) + '/checkout', {
				method: 'POST',
				headers: { 'Idempotency-Key': checkoutKey },
				body: JSON.stringify({ starts_at: selected })
			});
			location.href = '/checkout/' + encodeURIComponent(q.id);
		} catch (error) {
			message = error instanceof Error ? error.message : 'Checkout could not be started.';
			checkoutKey = '';
		} finally {
			busy = false;
		}
	}
	const dateLabel = (value: string) =>
		new Intl.DateTimeFormat(undefined, { dateStyle: 'full', timeStyle: 'short' }).format(new Date(value));
</script>

<svelte:head><title>Offer — WantMyTime</title></svelte:head>
<section class="form-page app-page workspace-offer-detail">
	<a class="back-link" href="/app/offers">← Offers</a>
	<p class="eyebrow">Offer detail</p>
	<h1 class="page-heading">A THOUGHTFUL<br />REQUEST.</h1>
	{#if loading}<p class="page-intro">Loading this private offer…</p>
	{:else if !offer}<div class="notice notice-warning">{message}</div>
	{:else}
		{#key offer.state}<div class="offer-detail-priority">
				<span>{offer.state.replaceAll('_', ' ').toUpperCase()} / VERSION {offer.version}</span><strong
					>{offer.role === 'seller' && offer.state === 'pending'
						? 'YOUR ANSWER IS NEXT.'
						: offer.role === 'buyer' && offer.state === 'countered'
							? 'A COUNTEROFFER IS WAITING.'
							: offer.state === 'agreed'
								? 'AGREED. WAITING FOR A TIME AND PAYMENT.'
								: offer.state === 'converted'
									? 'BOOKED.'
									: 'OFFER DETAILS'}</strong
				>
				<p>
					{offer.state === 'agreed'
						? offer.role === 'seller'
							? `${offer.buyer_name} picks a time and pays${offer.checkout_expires_at ? ` by ${dateLabel(offer.checkout_expires_at)}` : ''}. You’ll get a booking email.`
							: 'Pick a time and pay to confirm the booking.'
						: ['pending', 'countered'].includes(offer.state)
							? `Response deadline: ${dateLabel(offer.expires_at)}.`
							: ''}
				</p>
			</div>{/key}
		<div class="appointment-slip">
			<div>
				<small>{offer.role === 'seller' ? 'FROM' : 'TO'}</small><strong
					>{offer.role === 'seller' ? offer.buyer_name : offer.seller_name || offer.seller}</strong
				>
			</div>
			<div><small>HOW LONG</small><strong>{offer.duration_minutes} minutes</strong></div>
			<div><small>OFFER</small><strong>{money(offer.amount_minor)}</strong></div>
			<div><small>RESPOND BY</small><strong>{dateLabel(offer.expires_at)}</strong></div>
		</div>
		{#if offer.state === 'pending' && offer.role === 'seller'}
			<div class="setup-form">
				<button class="button" onclick={() => act('accept')} disabled={busy}>Accept {money(offer.amount_minor)}</button
				><label
					>Counter once
					<div class="money-input">
						<span>{currencySymbol(currency).trim()}</span><input bind:value={counterAmount} inputmode="decimal" />
					</div></label
				><button class="button button-secondary" onclick={() => act('counter')} disabled={busy}
					>Send counteroffer</button
				><button class="text-link" onclick={() => act('decline')} disabled={busy}>Decline offer</button>
			</div>
		{:else if offer.state === 'countered' && offer.role === 'buyer'}
			<div class="setup-form">
				<div class="notice notice-info">
					The seller sent one counteroffer. Accepting agrees to the amount; you then pick a time and pay.
				</div>
				<button class="button" onclick={() => act('accept')} disabled={busy}>Accept counteroffer</button><button
					class="button button-secondary"
					onclick={() => act('decline')}
					disabled={busy}>Decline counteroffer</button
				><button class="text-link" onclick={() => act('withdraw')} disabled={busy}>Withdraw</button>
			</div>
		{:else if (offer.state === 'pending' || offer.state === 'countered') && offer.role === 'buyer'}
			<button class="button button-secondary" onclick={() => act('withdraw')} disabled={busy}>Withdraw offer</button>
		{:else if offer.state === 'countered' && offer.role === 'seller'}
			<button class="button button-secondary" onclick={() => act('withdraw')} disabled={busy}
				>Withdraw counteroffer</button
			>
		{:else if offer.state === 'agreed' && offer.role === 'buyer'}
			{#if offer.local_simulator || offer.provider_checkout_enabled}
				<div class="setup-form">
					<label>Choose a date<input class="field" type="date" bind:value={day} onchange={loadSlots} /></label
					>{#if slots.length}<fieldset>
							<legend>Available times (your timezone)</legend>
							<div class="slot-list">
								{#each slots as slot}<button
										type="button"
										class="slot-option"
										class:selected={selected === slot.starts_at}
										onclick={() => {
											selected = slot.starts_at;
											checkoutKey = '';
										}}>{timeOnly(slot.starts_at, zone)}</button
									>{/each}
							</div>
						</fieldset>{:else}<p class="notice notice-info">Nothing free on this date. Try another.</p>{/if}<button
						class="button"
						onclick={checkout}
						disabled={!selected || busy}>Continue to payment</button
					>{#if offer.local_simulator}<p class="form-note">Test setup: no money moves.</p>{/if}
				</div>
			{:else}<div class="notice notice-warning">
					The agreement is saved. Payments for this seller aren’t switched on yet; no charge has been made.
				</div>{/if}
		{/if}
		{#if message}<p class="notice notice-info" aria-live="polite">{message}</p>{/if}
	{/if}
</section>
