<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { browserZone } from '$lib/timezones';
	import { currencySymbol, parseMoneyToMinor } from '$lib/money';
	import { handlePattern, validHandle } from '$lib/handle';
	import { fetchHandleSuggestions } from '$lib/handle-suggestions';
	import HandleSuggestions from '$lib/components/HandleSuggestions.svelte';
	import MarketingConsent from '$lib/components/MarketingConsent.svelte';
	type Market = { country: string; name: string; currency: string; timezone: string };
	let handle = $state('');
	let name = $state('');
	let email = $state('');
	let amount = $state('10000');
	let message = $state('');
	let busy = $state(false);
	let marketing = $state(true);
	let markets = $state<Market[]>([{ country: 'NG', name: 'Nigeria', currency: 'NGN', timezone: 'Africa/Lagos' }]);
	let country = $state('NG');
	let market = $derived(markets.find((m) => m.country === country) ?? markets[0]);
	const params = new URLSearchParams(typeof window !== 'undefined' ? window.location.search : '');
	if (params.get('handle')) handle = params.get('handle')!;
	// The link follows the name until the person edits it themselves.
	let handleTouched = $state(!!params.get('handle'));
	let handleState = $state<'idle' | 'checking' | 'available' | 'taken' | 'invalid'>('idle');
	let suggestions = $state<string[]>([]);
	// One request per pause in typing: free links for the name, and whether the
	// typed link is free. Until the person types a link, the best one fills in.
	$effect(() => {
		const typedName = name.trim();
		const wanted = handleTouched ? handle.trim().toLowerCase() : '';
		if (!typedName && !wanted) {
			suggestions = [];
			handleState = 'idle';
			return;
		}
		if (wanted) handleState = validHandle(wanted) ? 'checking' : 'invalid';
		const timer = setTimeout(async () => {
			try {
				const result = await fetchHandleSuggestions(typedName, wanted);
				suggestions = result.suggestions;
				if (!handleTouched) {
					handle = result.suggestions[0] ?? '';
					handleState = handle ? 'available' : 'idle';
				} else if (result.wanted?.valid && handle.trim().toLowerCase() === wanted) {
					handleState = result.wanted.available ? 'available' : 'taken';
				}
			} catch {
				// Suggestions are a convenience; the form still works without them.
			}
		}, 300);
		return () => clearTimeout(timer);
	});
	function pickHandle(next: string) {
		handle = next;
		handleTouched = true;
	}

	onMount(async () => {
		try {
			const list = (await api<{ markets: Market[] }>('/api/v1/markets')).markets;
			if (list.length) {
				markets = list;
				// Start from the country that matches the browser's timezone.
				const zone = browserZone();
				country = (list.find((m) => m.timezone === zone) ?? list[0]).country;
			}
		} catch {
			// Nigeria stays the default if the list can't load.
		}
	});

	async function submit() {
		message = '';
		const minor = parseMoneyToMinor(amount);
		if (minor === null || minor < 100n) {
			message = `Enter a price of at least ${currencySymbol(market.currency)}1.`;
			return;
		}
		busy = true;
		try {
			const wanted = handle.trim().toLowerCase();
			const check = await api<{ available: boolean }>(`/api/v1/handles/${encodeURIComponent(wanted)}/availability`);
			if (!check.available) {
				message = 'That link is taken. Try another.';
				return;
			}
			const result = await api<{ challenge_id: string }>('/api/v1/auth/challenges', {
				method: 'POST',
				body: JSON.stringify({ email, purpose: 'claim' })
			});
			// Everything else starts from a sensible default and can be changed later under Your link.
			const draft = {
				handle: wanted,
				name: name.trim(),
				identity_url: '',
				mode: 'fixed',
				base_30_minor: Number(minor),
				durations: [15, 30, 60],
				timezone: browserZone(),
				country: market.country,
				marketing
			};
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
	<p class="page-intro">A few things and you’re live. You can change most of them later.</p>
	<form
		class="setup-form"
		onsubmit={(e) => {
			e.preventDefault();
			submit();
		}}
	>
		<label
			>Your name<input
				class="field"
				bind:value={name}
				maxlength="80"
				required
				placeholder="How people know you"
				autocomplete="name"
			/></label
		>
		<div class="link-handle">
			<label for="claim-handle">Your link</label>
			<div class="handle-input">
				<span>wantmytime.com/</span><input
					id="claim-handle"
					class="field"
					bind:value={handle}
					oninput={() => (handleTouched = true)}
					minlength="3"
					maxlength="24"
					pattern={handlePattern}
					required
					placeholder="yourname"
					autocapitalize="off"
					autocomplete="off"
					spellcheck="false"
					aria-describedby="claim-handle-note"
				/>
			</div>
			<p id="claim-handle-note" class="form-note" aria-live="polite">
				{#if handleState === 'available'}wantmytime.com/{handle.trim().toLowerCase()} is yours if you want it.{:else if handleState === 'taken'}That
					link is taken. Pick a free one below or try another.{:else if handleState === 'invalid'}Use 3 to 24 letters,
					numbers or single hyphens.{:else if handleState === 'checking'}Checking…{:else}We’ll suggest one from your
					name. You can change it later.{/if}
			</p>
			<HandleSuggestions
				{suggestions}
				current={handle}
				label={handleState === 'taken' ? 'Free' : 'Also free'}
				onpick={pickHandle}
			/>
		</div>
		{#if markets.length > 1}
			<label
				>Where you get paid<select class="field" bind:value={country}
					>{#each markets as m (m.country)}<option value={m.country}>{m.name} ({m.currency})</option>{/each}</select
				><span class="form-note"
					>Your prices and payouts are in this country’s currency. People can book you from anywhere.</span
				></label
			>
		{/if}
		<label
			>Your price for 30 minutes
			<div class="money-input">
				<span>{currencySymbol(market.currency).trim()}</span><input
					bind:value={amount}
					inputmode="decimal"
					required
					aria-label={`Price in ${market.currency} for 30 minutes`}
				/>
			</div>
			<span class="form-note">15 minutes costs half, an hour costs double. Change it any time.</span></label
		>
		<label
			>Your email<input
				class="field"
				type="email"
				bind:value={email}
				autocomplete="email"
				required
				placeholder="you@example.com"
			/><span class="form-note">We’ll send a code to confirm it’s you. No password.</span></label
		>
		<MarketingConsent bind:checked={marketing} />
		<button class="button" type="submit" disabled={busy}
			>{busy ? 'Sending your code…' : 'Send my code'} <span aria-hidden="true">↗</span></button
		>
		{#if message}<p class="notice notice-warning" role="alert">{message}</p>{/if}
	</form>
</section>
