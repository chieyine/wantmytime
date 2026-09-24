<script lang="ts">
	import {onMount} from 'svelte';import {api} from '$lib/api';import {page} from '$app/state';import {parseNairaToMinor} from '$lib/money';
	let seller=$derived((page.url.searchParams.get('seller')??'').trim().toLowerCase());
	type Person={handle:string;name:string;mode:string;durations:number[];paused:boolean};
	let person=$state<Person|null>(null);let duration=$state(30);let amount=$state('10000');let buyerName=$state('');let email=$state('');let message=$state('');let loading=$state(true);let busy=$state(false);
	onMount(async()=>{try{person=await api<Person>(`/api/v1/people/${encodeURIComponent(seller)}`);const requested=Number(page.url.searchParams.get('duration')??30);duration=person.durations.includes(requested)?requested:(person.durations[0]??30)}catch(error){message=error instanceof Error?error.message:'This link could not be loaded.'}finally{loading=false}});
	async function submit(){
		if(!person||person.mode!=='offer'||person.paused)return;const parsed=parseNairaToMinor(amount);if(parsed===null||parsed<100n||parsed>100000000n){message='Enter an offer between ₦1 and ₦1,000,000.';return}const minor=Number(parsed);
		busy=true;message='';try{const challenge=await api<{challenge_id:string}>('/api/v1/auth/challenges',{method:'POST',body:JSON.stringify({email,purpose:'guest_offer'})});sessionStorage.setItem('aside_offer_draft',JSON.stringify({seller,duration,name:buyerName.trim(),amount_minor:minor,idempotency_key:crypto.randomUUID()}));window.location.href=`/verify?purpose=offer&challenge=${encodeURIComponent(challenge.challenge_id)}`}catch(error){message=error instanceof Error?error.message:'Email verification could not be started.'}finally{busy=false}
	}
</script>
<svelte:head><title>Make an offer — WantMyTime</title></svelte:head>
<section class="form-page"><a class="back-link" href={`/${seller}`}>← Back to {seller}</a><p class="eyebrow">Offer request</p><h1 class="page-heading">Make an offer.</h1>
	{#if loading}<p class="page-intro">Loading this link…</p>{:else if !person}<div class="notice notice-warning">{message}</div>{:else if person.mode!=='offer'||person.paused}<div class="notice notice-info">This link is not receiving offers right now.</div>{:else}
	<p class="page-intro">Send a verified offer to {person.name}. They can accept, counter once or decline. No payment is taken when you make an offer.</p><form class="setup-form" onsubmit={(e)=>{e.preventDefault();submit()}}>
		<label>How long?<select class="field" bind:value={duration}>{#each person.durations as d}<option value={d}>{d} minutes</option>{/each}</select></label><label>Your offer<div class="money-input"><span>₦</span><input bind:value={amount} inputmode="decimal" required /></div></label><label>Your name<input class="field" bind:value={buyerName} maxlength="80" autocomplete="name" required /></label><label>Email for verification<input class="field" type="email" bind:value={email} autocomplete="email" required /></label>
		<div class="notice notice-warning">The offer is saved to both accounts after email verification. Email notifications are not configured, so the seller will see it in their WantMyTime account.</div>{#if message}<p class="notice notice-warning" aria-live="polite">{message}</p>{/if}<button class="button" type="submit" disabled={busy}>{busy?'Sending code…':'Verify email and send offer'} <span>↗</span></button>
	</form>{/if}
</section>
