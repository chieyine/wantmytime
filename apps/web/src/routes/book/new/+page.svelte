<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { formatNaira, priceForDuration } from '$lib/money';
	import { page } from '$app/state';
	import { viewerTimeZone, todayIn, sameClock, clockIn, zoneCity } from '$lib/time';
	let seller = $derived((page.url.searchParams.get('seller') ?? '').trim().toLowerCase());
	let duration = $derived.by(() => { const d = Number(page.url.searchParams.get('duration') ?? 30); return d === 15 || d === 30 || d === 60 ? d : 30; });
	type Person = { handle:string; name:string; mode:string; base_30_minor:number; durations:number[]; timezone:string; paused:boolean; ready:boolean; local_simulator:boolean; provider_checkout_enabled:boolean };
	type Slot = { starts_at:string; local_label:string; seller_label?:string };
	let person = $state<Person|null>(null);
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

	onMount(async () => {
		try {
			person = await api<Person>(`/api/v1/people/${encodeURIComponent(seller)}`);
			viewerZone = viewerTimeZone();
			day = todayIn(zone);
			if (person.local_simulator || person.provider_checkout_enabled) await loadSlots();
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
			const data = await api<{ slots:Slot[] }>(`/api/v1/people/${encodeURIComponent(seller)}/slots?date=${encodeURIComponent(day)}&duration=${duration}&tz=${encodeURIComponent(zone)}`);
			if (request === slotRequest) slots = data.slots;
		} catch (error) {
			if (request === slotRequest) { slots = []; message = error instanceof Error ? error.message : 'Times could not be loaded.'; }
		} finally {
			if (request === slotRequest) slotsLoading = false;
		}
	}

	async function startVerification() {
		if (!selected || !buyerName.trim() || !email.trim()) return;
		busy = true;
		message = '';
		try {
			const challenge = await api<{ challenge_id:string }>('/api/v1/auth/challenges', { method:'POST', body:JSON.stringify({ email, purpose:'guest_booking' }) });
			const draft = { seller, duration, name:buyerName.trim(), starts_at:selected, idempotency_key:crypto.randomUUID() };
			sessionStorage.setItem('aside_booking_draft', JSON.stringify(draft));
			window.location.href = `/verify?purpose=booking&challenge=${encodeURIComponent(challenge.challenge_id)}`;
		} catch (error) {
			message = error instanceof Error ? error.message : 'Email verification could not be started.';
		} finally { busy = false; }
	}
</script>

<svelte:head><title>Choose a time — WantMyTime</title></svelte:head>
<section class="booking-page">
	<div class="booking-breadcrumb"><a href={`/${encodeURIComponent(seller)}`}>← {person?.name || seller}</a><span>Booking / 01</span></div>
	{#if loadMessage}<div class="booking-error" role="alert">{loadMessage}</div>
	{:else if !person}<p class="booking-loading">Loading available times…</p>
	{:else}
		<header class="booking-heading"><p>Book a conversation</p><h1>Choose a time with<br /><em>{person.name}.</em></h1></header>
		{#if !person.local_simulator && !person.provider_checkout_enabled}<div class="booking-error">Bookings are not open yet. Payment setup is still in progress.</div>
		{:else if person.paused || !person.ready}<div class="booking-error">{person.name} is not taking bookings right now.</div>
		{:else}
			<div class="booking-grid">
				<div class="booking-form">
					<section class="booking-step"><div class="booking-step-heading"><span>01</span><div><h2>Date and time</h2><p>{#if !differentZone}Times are shown in {zoneCity(viewerZone)} time.{:else if showSellerZone}Times are shown in {person.name}’s time ({zoneCity(person.timezone)}).{:else}Times are shown in your time ({zoneCity(viewerZone)}). {person.name} is in {zoneCity(person.timezone)}.{/if}</p>{#if differentZone}<button type="button" class="link-button" onclick={() => { showSellerZone = !showSellerZone; loadSlots(); }}>{showSellerZone ? 'Show my time' : `Show ${person.name}’s time`}</button>{/if}</div></div>
						<label class="booking-date-label" for="booking-date">Choose a date</label><input id="booking-date" class="booking-date" type="date" bind:value={day} onchange={loadSlots} />
						{#if slotsLoading}<p class="booking-help">Finding available times…</p>
						{:else if slots.length === 0}<p class="booking-empty">{message || 'No times on this date. Try another day.'}</p>
						{:else}<div class="booking-times" role="group" aria-label="Available times">{#each slots as slot}<button type="button" class:selected={selected === slot.starts_at} aria-pressed={selected === slot.starts_at} onclick={() => selected = slot.starts_at}>{slot.local_label}</button>{/each}</div>{/if}
					</section>
					<section class="booking-step"><div class="booking-step-heading"><span>02</span><div><h2>Your details</h2><p>We’ll email a code to verify your address.</p></div></div>
						<div class="booking-fields"><label for="booking-name">Your name</label><input id="booking-name" bind:value={buyerName} maxlength="80" autocomplete="name" placeholder="The name you go by" /><label for="booking-email">Email address</label><input id="booking-email" type="email" bind:value={email} autocomplete="email" placeholder="you@example.com" /></div>
					</section>
					{#if message && slots.length > 0}<p class="booking-error" role="alert">{message}</p>{/if}
					<button class="booking-submit" type="button" onclick={startVerification} disabled={!selected || !buyerName.trim() || !email.trim() || busy}>{busy ? 'Sending verification code…' : 'Verify email and continue'} <span aria-hidden="true">↗</span></button>
					<p class="booking-help">Your time is held for 10 minutes after you verify your email. {person.local_simulator ? 'This is a development preview; no money is collected.' : 'Payment happens on the next step.'}</p>
				</div>
				<aside class="booking-summary"><div class="booking-summary-top"><span>Booking details</span><span>WantMyTime</span></div><h2>{person.name}</h2><div class="booking-summary-row"><span>Conversation</span><strong>{duration} minutes</strong></div><div class="booking-summary-row"><span>Time</span><strong>{chosenSlot ? chosenSlot.local_label : 'Choose a time'}{#if chosenSlot && differentZone}<small class="booking-other-zone">{showSellerZone ? `${clockIn(chosenSlot.starts_at, viewerZone)} your time` : `${clockIn(chosenSlot.starts_at, person.timezone)} for ${person.name}`}</small>{/if}</strong></div><div class="booking-summary-row"><span>Date</span><strong>{day || 'Choose a date'}</strong></div><div class="booking-summary-total"><span>Total</span><strong>{formatNaira(amount)}</strong></div><p>Your booking is confirmed after payment is verified.</p></aside>
			</div>
		{/if}
	{/if}
</section>
