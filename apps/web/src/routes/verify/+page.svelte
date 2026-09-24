<script lang="ts">
	import { api } from '$lib/api';
	import { viewerTimeZone } from '$lib/time';
	let code = $state(''); let message = $state(''); let busy = $state(false);
	const query = typeof window !== 'undefined' ? new URLSearchParams(window.location.search) : new URLSearchParams();
	const challenge = query.get('challenge') ?? '';
	const purpose = query.get('purpose') ?? 'login';
	const next = query.get('next');
	async function verify() {
		busy = true; message = '';
		try {
			await api<{booking_ids?:string[]}>('/api/v1/auth/challenges/' + encodeURIComponent(challenge) + '/verify', {method:'POST', body:JSON.stringify({code, timezone: viewerTimeZone()})});
			if (purpose === 'guest_booking' || purpose === 'booking') {
				const raw = sessionStorage.getItem('aside_booking_draft');
				if (!raw) throw new Error('Your booking details expired. Choose a time again.');
				const draft = JSON.parse(raw) as {seller:string; duration:number; name:string; starts_at:string; idempotency_key:string};
				const quote = await api<{id:string}>('/api/v1/quotes', {method:'POST', headers:{'Idempotency-Key':draft.idempotency_key}, body:JSON.stringify({seller:draft.seller,duration_minutes:draft.duration,name:draft.name,starts_at:draft.starts_at})});
				sessionStorage.removeItem('aside_booking_draft');
				window.location.href = `/checkout/${encodeURIComponent(quote.id)}`;
			} else if (purpose === 'guest_offer' || purpose === 'offer') {
				const raw = sessionStorage.getItem('aside_offer_draft');
				if (!raw) throw new Error('Your offer details expired. Start again to send it.');
				const draft = JSON.parse(raw) as {seller:string;duration:number;name:string;amount_minor:number;idempotency_key:string};
				const offer = await api<{id:string}>('/api/v1/offers', {method:'POST',headers:{'Idempotency-Key':draft.idempotency_key},body:JSON.stringify({seller:draft.seller,duration_minutes:draft.duration,name:draft.name,amount_minor:draft.amount_minor})});
				sessionStorage.removeItem('aside_offer_draft');
				window.location.href = `/offer/${encodeURIComponent(offer.id)}`;
			} else if (purpose === 'access') {
				const target=next?new URL(next,window.location.origin):new URL('/access/bookings',window.location.origin);
				window.location.href=target.origin===window.location.origin?target.pathname+target.search+target.hash:'/access/bookings';
			} else
			if (purpose === 'claim') {
				const raw = sessionStorage.getItem('aside_claim_draft');
				if (!raw) throw new Error('Your setup details expired. Start again to claim your link.');
				const draft = JSON.parse(raw) as {handle:string; name:string; identity_url:string; mode:'fixed'|'offer'; base_30_minor:number; durations:number[]; timezone:string};
				const profile = {handle:draft.handle, name:draft.name, identity_url:draft.identity_url, mode:draft.mode, base_30_minor:draft.base_30_minor, durations:draft.durations, timezone:draft.timezone};
				await api('/api/v1/me/link', {method:'POST', body:JSON.stringify(profile)});
				sessionStorage.removeItem('aside_claim_draft');
				window.location.href = '/' + encodeURIComponent(draft.handle);
			} else {const target=next?new URL(next,window.location.origin):new URL('/app',window.location.origin);window.location.href=target.origin===window.location.origin?target.pathname+target.search+target.hash:'/app'}
		} catch (error) { message = error instanceof Error ? error.message : 'We could not verify that code.'; }
		finally { busy = false; }
	}
</script>
<svelte:head><title>Verify your email — WantMyTime</title></svelte:head>
<section class="form-page"><a class="back-link" href="/login">← Back</a><p class="eyebrow">Email verification</p><h1 class="page-heading">Enter your code.</h1><p class="page-intro">Use the eight digit code sent to your email. It expires in ten minutes.</p>
	<form class="setup-form" onsubmit={(e)=>{e.preventDefault();verify()}}>
		<label>Verification code<input class="field" bind:value={code} inputmode="numeric" autocomplete="one-time-code" minlength="8" maxlength="8" pattern="[0-9]{8}" required /></label>
		{#if message}<p class="notice notice-warning" aria-live="polite">{message}</p>{/if}
		<button class="button" type="submit" disabled={busy || !challenge}>{busy ? 'Verifying…' : 'Verify email'} <span>↗</span></button>
	</form>
</section>
