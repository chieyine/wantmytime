<script lang="ts">
	import { api } from '$lib/api';
	import { viewerTimeZone } from '$lib/time';
	import { saveMarketingChoice } from '$lib/marketing';
	let code = $state('');
	let message = $state('');
	let busy = $state(false);
	const query = typeof window !== 'undefined' ? new URLSearchParams(window.location.search) : new URLSearchParams();
	const challenge = query.get('challenge') ?? '';
	const purpose = query.get('purpose') ?? 'login';
	const next = query.get('next');
	async function verify() {
		busy = true;
		message = '';
		try {
			await api<{ booking_ids?: string[] }>('/api/v1/auth/challenges/' + encodeURIComponent(challenge) + '/verify', {
				method: 'POST',
				body: JSON.stringify({ code, timezone: viewerTimeZone() })
			});
			if (purpose === 'guest_booking' || purpose === 'booking') {
				const raw = sessionStorage.getItem('aside_booking_draft');
				if (!raw) throw new Error('Your booking details expired. Choose a time again.');
				const draft = JSON.parse(raw) as {
					seller: string;
					duration: number;
					name: string;
					starts_at: string;
					idempotency_key: string;
				};
				const quote = await api<{ id: string }>('/api/v1/quotes', {
					method: 'POST',
					headers: { 'Idempotency-Key': draft.idempotency_key },
					body: JSON.stringify({
						seller: draft.seller,
						duration_minutes: draft.duration,
						name: draft.name,
						starts_at: draft.starts_at
					})
				});
				sessionStorage.removeItem('aside_booking_draft');
				window.location.href = `/checkout/${encodeURIComponent(quote.id)}`;
			} else if (purpose === 'guest_offer' || purpose === 'offer') {
				const raw = sessionStorage.getItem('aside_offer_draft');
				if (!raw) throw new Error('Your offer details expired. Start again to send it.');
				const draft = JSON.parse(raw) as {
					seller: string;
					duration: number;
					name: string;
					amount_minor: number;
					idempotency_key: string;
					marketing?: boolean;
				};
				const offer = await api<{ id: string }>('/api/v1/offers', {
					method: 'POST',
					headers: { 'Idempotency-Key': draft.idempotency_key },
					body: JSON.stringify({
						seller: draft.seller,
						duration_minutes: draft.duration,
						name: draft.name,
						amount_minor: draft.amount_minor
					})
				});
				// Never let the email preference stop the offer going through.
				if (typeof draft.marketing === 'boolean')
					await saveMarketingChoice(draft.marketing, 'offer').catch(() => undefined);
				sessionStorage.removeItem('aside_offer_draft');
				window.location.href = `/offer/${encodeURIComponent(offer.id)}`;
			} else if (purpose === 'access') {
				const target = next
					? new URL(next, window.location.origin)
					: new URL('/access/bookings', window.location.origin);
				window.location.href =
					target.origin === window.location.origin ? target.pathname + target.search + target.hash : '/access/bookings';
			} else if (purpose === 'claim') {
				const raw = sessionStorage.getItem('aside_claim_draft');
				if (!raw) throw new Error('Your setup details expired. Start again to claim your link.');
				const draft = JSON.parse(raw) as {
					handle: string;
					name: string;
					identity_url: string;
					mode: 'fixed' | 'both';
					base_30_minor: number;
					durations: number[];
					timezone: string;
					country?: string;
					marketing?: boolean;
				};
				const profile = {
					handle: draft.handle,
					name: draft.name,
					identity_url: draft.identity_url,
					mode: draft.mode,
					base_30_minor: draft.base_30_minor,
					durations: draft.durations,
					timezone: draft.timezone,
					country: draft.country ?? ''
				};
				await api('/api/v1/me/link', { method: 'POST', body: JSON.stringify(profile) });
				if (typeof draft.marketing === 'boolean')
					await saveMarketingChoice(draft.marketing, 'seller_signup').catch(() => undefined);
				sessionStorage.removeItem('aside_claim_draft');
				window.location.href = '/app';
			} else {
				const target = next ? new URL(next, window.location.origin) : new URL('/app', window.location.origin);
				window.location.href =
					target.origin === window.location.origin ? target.pathname + target.search + target.hash : '/app';
			}
		} catch (error) {
			message = error instanceof Error ? error.message : 'That code didn’t work. Check it and try again.';
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>Verify your email — WantMyTime</title></svelte:head>
<section class="form-page">
	{#if purpose === 'login' || purpose === 'access'}<a class="back-link" href="/login">← Back</a>{:else}<button
			type="button"
			class="back-link back-button"
			onclick={() => history.back()}>← Back</button
		>{/if}
	<p class="eyebrow">{purpose === 'claim' ? 'Your link · step 2 of 2' : 'One quick check'}</p>
	<h1 class="page-heading">Check your email.</h1>
	<p class="page-intro">
		We sent you an 8-digit code. It works for 10 minutes. Can’t find it? Look in spam or promotions.
	</p>
	<form
		class="setup-form"
		onsubmit={(e) => {
			e.preventDefault();
			verify();
		}}
	>
		<label
			>Your code<input
				class="field code-field"
				bind:value={code}
				oninput={() => {
					code = code.replace(/\D/g, '').slice(0, 8);
					if (code.length === 8 && !busy) verify();
				}}
				inputmode="numeric"
				autocomplete="one-time-code"
				minlength="8"
				maxlength="8"
				pattern="[0-9]{8}"
				required
				placeholder="12345678"
			/></label
		>
		{#if message}<p class="notice notice-warning" aria-live="polite">{message}</p>{/if}
		<button class="button" type="submit" disabled={busy || !challenge}
			>{busy ? 'Checking…' : 'Continue'} <span aria-hidden="true">↗</span></button
		>
	</form>
</section>
