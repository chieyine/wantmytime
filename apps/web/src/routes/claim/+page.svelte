<script lang="ts">
	import { api } from '$lib/api';
	import { timezoneOptions } from '$lib/timezones';
	import { parseNairaToMinor } from '$lib/money';
	let handle = $state(''); let name = $state(''); let identity_url=$state(''); let mode = $state<'fixed'|'offer'>('fixed'); let amount = $state('10000'); let durations = $state([15,30,60]); let timezone = $state('Africa/Lagos'); let message = $state(''); let email=$state('');
	let busy = $state(false);
	const params = new URLSearchParams(typeof window !== 'undefined' ? window.location.search : '');
	if (params.get('handle')) handle = params.get('handle')!;
	async function submit(){
		message = '';
		const minor = mode === 'fixed' ? parseNairaToMinor(amount) : 0n;
		if (mode === 'fixed' && (minor === null || minor < 100n)) { message = 'Enter a price of at least ₦1.'; return; }
		busy = true;
		try {
			const wanted = handle.trim().toLowerCase();
			const check = await api<{available:boolean}>(`/api/v1/handles/${encodeURIComponent(wanted)}/availability`);
			if (!check.available) { message = 'That link is taken or not allowed. Try another.'; return; }
			const result = await api<{challenge_id:string}>('/api/v1/auth/challenges', {method:'POST', body:JSON.stringify({email, purpose:'claim'})});
			const draft = {handle: handle.trim().toLowerCase(), name, identity_url, mode, base_30_minor:mode==='fixed'?Number(minor):0, durations, timezone};
			sessionStorage.setItem('aside_claim_draft', JSON.stringify(draft));
			window.location.href = `/verify?purpose=claim&challenge=${encodeURIComponent(result.challenge_id)}`;
		} catch (error) { message = error instanceof Error ? error.message : 'Email verification could not be started.'; }
		finally { busy = false; }
	}
	function toggle(d:number){durations=durations.includes(d)?durations.filter(x=>x!==d):[...durations,d].sort((a,b)=>a-b)}
</script>
<svelte:head><title>Get your link — WantMyTime</title></svelte:head>
<section class="form-page"><a class="back-link" href="/">← Back</a><p class="eyebrow">Your link</p><h1 class="page-heading">Make it yours.</h1><p class="page-intro">Claim a personal link. Paid bookings stay off until the required setup and provider approvals are complete.</p>
	<form class="setup-form" onsubmit={(e)=>{e.preventDefault();submit()}}>
		<label>Choose your link <span class="field-prefix">wantmytime.com/</span><input class="field" bind:value={handle} minlength="3" maxlength="24" pattern="[a-zA-Z0-9]+(-[a-zA-Z0-9]+)*" required placeholder="yourname" /></label>
		<label>Your name<input class="field" bind:value={name} maxlength="80" required placeholder="How people know you" /></label>
		<label>Link to one social profile · optional<input class="field" type="url" bind:value={identity_url} placeholder="https://instagram.com/…" /></label>
		<label>Email<input class="field" type="email" bind:value={email} autocomplete="email" required placeholder="you@example.com" /></label>
		<fieldset><legend>How should requests work?</legend><div class="choice-row"><label class:chosen={mode==='fixed'}><input type="radio" bind:group={mode} value="fixed" /> I’ll set a price</label><label class:chosen={mode==='offer'}><input type="radio" bind:group={mode} value="offer" /> Let people make an offer</label></div></fieldset>
		{#if mode==='fixed'}<label>Your price for 30 minutes<div class="money-input"><span>₦</span><input bind:value={amount} inputmode="decimal" min="1" required aria-label="Naira price for 30 minutes" /></div></label>{/if}
		<fieldset><legend>Available conversation lengths</legend><div class="duration-row">{#each [15,30,60] as d}<label class:selected={durations.includes(d)}><input type="checkbox" checked={durations.includes(d)} onchange={()=>toggle(d)} /> {d} min</label>{/each}</div></fieldset>
		<label>Your timezone<select class="field" bind:value={timezone}>{#each timezoneOptions(timezone) as zone}<option value={zone}>{zone}</option>{/each}</select></label>
		<div class="notice notice-warning">We’ll verify your email before saving this setup. Paid bookings remain disabled until required approvals are complete.</div>
		<button class="button" type="submit" disabled={durations.length===0 || busy}>{busy ? 'Sending…' : 'Continue to verify email'} <span>↗</span></button>
		{#if message}<p class="notice notice-info" aria-live="polite">{message}</p>{/if}
	</form>
</section>
