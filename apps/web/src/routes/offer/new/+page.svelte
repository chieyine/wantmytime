<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { page } from '$app/state';
	import { currencySymbol, formatMoney, parseMoneyToMinor } from '$lib/money';
	let seller = $derived((page.url.searchParams.get('seller') ?? '').trim().toLowerCase());
	type Person = { handle: string; name: string; mode: string; durations: number[]; paused: boolean; ready: boolean; currency?: string };
	let person = $state<Person | null>(null);
	let duration = $state(30);
	let amount = $state('10000');
	let buyerName = $state('');
	let email = $state('');
	let message = $state('');
	let loading = $state(true);
	let busy = $state(false);
	const currency = $derived(person?.currency || 'NGN');
	onMount(async () => {
		try {
			person = await api<Person>(`/api/v1/people/${encodeURIComponent(seller)}`);
			if (person.mode === 'fixed') {
				window.location.replace(`/book/new?seller=${encodeURIComponent(seller)}&duration=${page.url.searchParams.get('duration') ?? 30}`);
				return;
			}
			const requested = Number(page.url.searchParams.get('duration') ?? 30);
			duration = person.durations.includes(requested) ? requested : (person.durations[0] ?? 30);
		} catch (error) {
			message = error instanceof Error ? error.message : 'This link could not be loaded.';
		} finally {
			loading = false;
		}
	});
	async function submit() {
		if (!person || person.mode === 'fixed' || person.paused || !person.ready) return;
		const parsed = parseMoneyToMinor(amount);
		if (parsed === null || parsed < 100n || parsed > 100000000n) {
			message = `Enter an offer between ${formatMoney(100, currency)} and ${formatMoney(100000000, currency)}.`;
			return;
		}
		busy = true;
		message = '';
		try {
			const challenge = await api<{ challenge_id: string }>('/api/v1/auth/challenges', { method: 'POST', body: JSON.stringify({ email, purpose: 'guest_offer' }) });
			sessionStorage.setItem('aside_offer_draft', JSON.stringify({ seller, duration, name: buyerName.trim(), amount_minor: Number(parsed), idempotency_key: crypto.randomUUID() }));
			window.location.href = `/verify?purpose=offer&challenge=${encodeURIComponent(challenge.challenge_id)}`;
		} catch (error) {
			message = error instanceof Error ? error.message : 'Email verification could not be started.';
		} finally {
			busy = false;
		}
	}
</script>
<svelte:head><title>Make an offer — WantMyTime</title></svelte:head>
<section class="form-page">
	<a class="back-link" href={`/${seller}`}>← Back to {person?.name || seller}</a>
	<p class="eyebrow">Offer</p>
	<h1 class="page-heading">Make an offer.</h1>
	{#if loading}<p class="page-intro">Loading this link…</p>
	{:else if !person}<div class="notice notice-warning">{message}</div>
	{:else if person.mode === 'fixed' || person.paused || !person.ready}<div class="notice notice-info">{person.name} isn’t taking offers right now. Check back soon.</div>
	{:else}
		<p class="page-intro">Name your price for time with {person.name}. They can accept, counter once or decline, and you’ll get an email either way. You only pay if you both agree, after you pick a time.</p>
		<form class="setup-form" onsubmit={(e) => { e.preventDefault(); submit(); }}>
			<label>How long?<select class="field" bind:value={duration}>{#each person.durations as d}<option value={d}>{d} minutes</option>{/each}</select></label>
			<label>Your offer<div class="money-input"><span>{currencySymbol(currency).trim()}</span><input bind:value={amount} inputmode="decimal" required aria-label={`Offer in ${currency}`} /></div><span class="form-note">In {currency}.</span></label>
			<label>Your name<input class="field" bind:value={buyerName} maxlength="80" autocomplete="name" required /></label>
			<label>Your email<input class="field" type="email" bind:value={email} autocomplete="email" required /><span class="form-note">We send a code to confirm it, then send your offer. Replies come to this address.</span></label>
			{#if message}<p class="notice notice-warning" aria-live="polite">{message}</p>{/if}
			<button class="button" type="submit" disabled={busy}>{busy ? 'Sending code…' : 'Confirm email and send offer'} <span>↗</span></button>
		</form>
	{/if}
</section>
