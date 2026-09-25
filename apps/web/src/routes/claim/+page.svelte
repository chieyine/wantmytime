<script lang="ts">
	import { api } from '$lib/api';
	import { timezoneOptions } from '$lib/timezones';
	import { parseNairaToMinor } from '$lib/money';
	let handle = $state('');
	let name = $state('');
	let email = $state('');
	let amount = $state('10000');
	let message = $state('');
	let busy = $state(false);
	const params = new URLSearchParams(typeof window !== 'undefined' ? window.location.search : '');
	if (params.get('handle')) handle = params.get('handle')!;
	// Everything else starts from a sensible default and can be changed later under Your link.
	function browserZone() {
		try {
			const zone = Intl.DateTimeFormat().resolvedOptions().timeZone;
			return timezoneOptions(zone).includes(zone) ? zone : 'Africa/Lagos';
		} catch {
			return 'Africa/Lagos';
		}
	}
	async function submit() {
		message = '';
		const minor = parseNairaToMinor(amount);
		if (minor === null || minor < 100n) { message = 'Enter a price of at least ₦1.'; return; }
		busy = true;
		try {
			const wanted = handle.trim().toLowerCase();
			const check = await api<{ available: boolean }>(`/api/v1/handles/${encodeURIComponent(wanted)}/availability`);
			if (!check.available) { message = 'That link is taken. Try another.'; return; }
			const result = await api<{ challenge_id: string }>('/api/v1/auth/challenges', { method: 'POST', body: JSON.stringify({ email, purpose: 'claim' }) });
			const draft = { handle: wanted, name: name.trim(), identity_url: '', mode: 'fixed', base_30_minor: Number(minor), durations: [15, 30, 60], timezone: browserZone() };
			sessionStorage.setItem('aside_claim_draft', JSON.stringify(draft));
			window.location.href = `/verify?purpose=claim&challenge=${encodeURIComponent(result.challenge_id)}`;
		} catch (error) {
			message = error instanceof Error ? error.message : 'We couldn’t send your code. Try again.';
		} finally {
			busy = false;
		}
	}
</script>
<svelte:head><title>Get your link — WantMyTime</title></svelte:head>
<section class="form-page">
	<a class="back-link" href="/">← Back</a>
	<p class="eyebrow">Your link · step 1 of 2</p>
	<h1 class="page-heading">Make it yours.</h1>
	<p class="page-intro">Four things and you’re live. You can change any of them later.</p>
	<form class="setup-form" onsubmit={(e) => { e.preventDefault(); submit(); }}>
		<label>Your link <span class="field-prefix">wantmytime.com/</span><input class="field" bind:value={handle} minlength="3" maxlength="24" pattern="[a-zA-Z0-9]+(-[a-zA-Z0-9]+)*" required placeholder="yourname" autocapitalize="off" spellcheck="false" /></label>
		<label>Your name<input class="field" bind:value={name} maxlength="80" required placeholder="How people know you" autocomplete="name" /></label>
		<label>Your price for 30 minutes<div class="money-input"><span>₦</span><input bind:value={amount} inputmode="decimal" required aria-label="Price in naira for 30 minutes" /></div><span class="form-note">15 minutes costs half, an hour costs double. Change it any time.</span></label>
		<label>Your email<input class="field" type="email" bind:value={email} autocomplete="email" required placeholder="you@example.com" /><span class="form-note">We’ll send a code to confirm it’s you. No password.</span></label>
		<button class="button" type="submit" disabled={busy}>{busy ? 'Sending your code…' : 'Send my code'} <span aria-hidden="true">↗</span></button>
		{#if message}<p class="notice notice-warning" role="alert">{message}</p>{/if}
	</form>
</section>
