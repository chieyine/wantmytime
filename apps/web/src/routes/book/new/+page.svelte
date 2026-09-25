<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { recordProductEvent } from '$lib/analytics';
	import { formatMoney, priceForDuration, methodLabels } from '$lib/money';
	import { page } from '$app/state';
	import { viewerTimeZone, todayIn, sameClock, clockIn, zoneCity, timeOnly } from '$lib/time';
	let seller = $derived((page.url.searchParams.get('seller') ?? '').trim().toLowerCase());
	let duration = $derived.by(() => {
		const d = Number(page.url.searchParams.get('duration') ?? 30);
		return d === 15 || d === 30 || d === 60 ? d : 30;
	});
	type Person = {
		handle: string;
		name: string;
		mode: string;
		base_30_minor: number;
		durations: number[];
		timezone: string;
		paused: boolean;
		ready: boolean;
		local_simulator: boolean;
		provider_checkout_enabled: boolean;
		currency?: string;
		payment_methods?: string[];
	};
	type Slot = { starts_at: string; local_label: string; seller_label?: string };
	let person = $state<Person | null>(null);
	let loadMessage = $state('');
	let slots = $state<Slot[]>([]);
	let slotsLoading = $state(false);
	let selected = $state('');
	let day = $state('');
	let buyerName = $state('');
	let email = $state('');
	let message = $state('');
	let busy = $state(false);
	let slotRequest = 0;
	// Times default to the viewer's own zone; they can switch to the seller's.
	let viewerZone = $state('UTC');
	let showSellerZone = $state(false);
	let zone = $derived(showSellerZone && person ? person.timezone : viewerZone);
	let differentZone = $derived(person ? !sameClock(viewerZone, person.timezone) : false);
	let amount = $derived(person ? priceForDuration(person.base_30_minor, duration) : 0);
	let chosenSlot = $derived(slots.find((slot) => slot.starts_at === selected));
	let payWith = $derived((person?.payment_methods ?? []).map((m) => (methodLabels[m] ?? m).toLowerCase()).join(' or '));

	onMount(async () => {
		try {
			person = await api<Person>(`/api/v1/people/${encodeURIComponent(seller)}`);
			if (person.mode === 'offer') {
				// This link takes offers, not fixed-price bookings.
				window.location.replace(`/offer/new?seller=${encodeURIComponent(seller)}&duration=${duration}`);
				return;
			}
			viewerZone = viewerTimeZone();
			day = todayIn(zone);
			if (person.local_simulator || person.provider_checkout_enabled) {
				// Open on the first day that has a free time, looking up to two weeks ahead.
				await loadSlots();
				for (let i = 1; i < 14 && slots.length === 0 && !message; i++) {
					const next = new Date(`${day}T12:00:00Z`);
					next.setUTCDate(next.getUTCDate() + 1);
					day = next.toISOString().slice(0, 10);
					await loadSlots();
				}
			}
		} catch (error) {
			loadMessage = error instanceof Error ? error.message : 'This link could not be loaded.';
		}
	});

	async function loadSlots() {
		if (!day || !person) return;
		const request = ++slotRequest;
		selected = '';
		message = '';
		slotsLoading = true;
		try {
			const data = await api<{ slots: Slot[] }>(
				`/api/v1/people/${encodeURIComponent(seller)}/slots?date=${encodeURIComponent(day)}&duration=${duration}&tz=${encodeURIComponent(zone)}`
			);
			if (request === slotRequest) slots = data.slots;
		} catch (error) {
			if (request === slotRequest) {
				slots = [];
				message = error instanceof Error ? error.message : 'Times could not be loaded.';
			}
		} finally {
			if (request === slotRequest) slotsLoading = false;
		}
	}

	// Funnel events: once per visit to this page, never with names or emails.
	let pickedSent = false;
	$effect(() => {
		if (selected && !pickedSent && seller) {
			pickedSent = true;
			void recordProductEvent('slot_selected', seller);
		}
	});
	onMount(() => {
		if (seller) void recordProductEvent('booking_started', seller);
	});

	async function startVerification() {
		if (!selected || !buyerName.trim() || !email.trim()) return;
		busy = true;
		message = '';
		try {
			// No code first: open a booking-only session for this email, hold the time, go to payment.
			await api('/api/v1/bookings/start', { method: 'POST', body: JSON.stringify({ email: email.trim() }) });
			const quote = await api<{ id: string }>('/api/v1/quotes', {
				method: 'POST',
				headers: { 'Idempotency-Key': crypto.randomUUID() },
				body: JSON.stringify({ seller, duration_minutes: duration, name: buyerName.trim(), starts_at: selected })
			});
			window.location.href = `/checkout/${encodeURIComponent(quote.id)}`;
		} catch (error) {
			message = error instanceof Error ? error.message : 'We couldn’t hold that time. Try again.';
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>Choose a time — WantMyTime</title></svelte:head>
<section class="booking-page">
	<div class="booking-breadcrumb">
		<a href={`/${encodeURIComponent(seller)}`}>← {person?.name || seller}</a><span>Booking / 01</span>
	</div>
	{#if loadMessage}<div class="booking-error" role="alert">{loadMessage}</div>
	{:else if !person}<p class="booking-loading">Loading available times…</p>
	{:else}
		<header class="booking-heading">
			<p>Book a call</p>
			<h1>Pick a time with<br /><em>{person.name}.</em></h1>
		</header>
		{#if !person.local_simulator && !person.provider_checkout_enabled}<div class="booking-error">
				Bookings open soon. Check back in a little while.
			</div>
		{:else if person.paused || !person.ready}<div class="booking-error">
				{person.name} isn’t taking bookings right now.
			</div>
		{:else}
			<div class="booking-grid">
				<div class="booking-form">
					<section class="booking-step">
						<div class="booking-step-heading">
							<span>01</span>
							<div>
								<h2>Date and time</h2>
								<p>
									{#if !differentZone}Times are shown in {zoneCity(viewerZone)} time.{:else if showSellerZone}Times are
										shown in {person.name}’s time ({zoneCity(person.timezone)}).{:else}Times are shown in your time ({zoneCity(
											viewerZone
										)}). {person.name} is in {zoneCity(person.timezone)}.{/if}
								</p>
								{#if differentZone}<button
										type="button"
										class="link-button"
										onclick={() => {
											showSellerZone = !showSellerZone;
											loadSlots();
										}}>{showSellerZone ? 'Show my time' : `Show ${person.name}’s time`}</button
									>{/if}
							</div>
						</div>
						<label class="booking-date-label" for="booking-date">Date</label><input
							id="booking-date"
							class="booking-date"
							type="date"
							bind:value={day}
							onchange={loadSlots}
						/>
						{#if slotsLoading}<p class="booking-help">Finding available times…</p>
						{:else if slots.length === 0}<p class="booking-empty">
								{message || 'Nothing free on this day. Try another.'}
							</p>
						{:else}<div class="booking-times" role="group" aria-label="Available times">
								{#each slots as slot}<button
										type="button"
										class:selected={selected === slot.starts_at}
										aria-pressed={selected === slot.starts_at}
										onclick={() => (selected = slot.starts_at)}
										>{timeOnly(slot.starts_at, differentZone && showSellerZone ? person.timezone : viewerZone)}</button
									>{/each}
							</div>{/if}
					</section>
					<section class="booking-step">
						<div class="booking-step-heading">
							<span>02</span>
							<div>
								<h2>You</h2>
								<p>So {person.name} knows who’s coming. Your booking confirmation goes to this email.</p>
							</div>
						</div>
						<div class="booking-fields">
							<label for="booking-name">Your name</label><input
								id="booking-name"
								bind:value={buyerName}
								maxlength="80"
								autocomplete="name"
								placeholder="Your name"
							/><label for="booking-email">Email</label><input
								id="booking-email"
								type="email"
								bind:value={email}
								autocomplete="email"
								placeholder="you@example.com"
							/>
						</div>
					</section>
					{#if message && slots.length > 0}<p class="booking-error" role="alert">{message}</p>{/if}
					<button
						class="booking-submit"
						type="button"
						onclick={startVerification}
						disabled={!selected || !buyerName.trim() || !email.trim() || busy}
						>{busy ? 'Holding your time…' : 'Continue to payment'} <span aria-hidden="true">↗</span></button
					>
					<p class="booking-help">
						Next: pay{payWith ? ` by ${payWith}` : ''}. We hold your time while you do. {person.local_simulator
							? 'This is a test setup, so no money moves.'
							: ''}
					</p>
				</div>
				<aside class="booking-summary">
					<div class="booking-summary-top"><span>Your booking</span><span>WantMyTime</span></div>
					<h2>{person.name}</h2>
					<div class="booking-summary-row"><span>Length</span><strong>{duration} minutes</strong></div>
					<div class="booking-summary-row">
						<span>Time</span><strong
							>{chosenSlot ? chosenSlot.local_label : 'Pick a time'}{#if chosenSlot && differentZone}<small
									class="booking-other-zone"
									>{showSellerZone
										? `${clockIn(chosenSlot.starts_at, viewerZone)} your time`
										: `${clockIn(chosenSlot.starts_at, person.timezone)} for ${person.name}`}</small
								>{/if}</strong
						>
					</div>
					<div class="booking-summary-row">
						<span>Date</span><strong
							>{day
								? new Intl.DateTimeFormat(undefined, { weekday: 'short', day: 'numeric', month: 'short' }).format(
										new Date(`${day}T12:00:00`)
									)
								: 'Pick a date'}</strong
						>
					</div>
					<div class="booking-summary-total">
						<span>Total · {duration} min</span><strong>{formatMoney(amount, person.currency)}</strong>
					</div>
					<p>{payWith ? `Pay by ${payWith}.` : 'Pay after you pick a time.'} Confirmed the moment it lands.</p>
				</aside>
			</div>
		{/if}
	{/if}
</section>
