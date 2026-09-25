<script lang="ts">
	import { formatMoney, priceForDuration, paymentMethodsSentence } from '$lib/money';
	import TimeDial from '$lib/components/TimeDial.svelte';
	import { onMount } from 'svelte';
	type Profile = { handle:string; name:string; identity_url?:string; identity_label?:string; avatar_url?:string; timezone:string; base_30_minor:number|string; durations:number[]; paused:boolean; ready:boolean; mode:'fixed'|'offer'|'both'; currency?:string; payment_methods?:string[]; local_simulator?:boolean; provider_checkout_enabled?:boolean; cancellation_policy?:{ key:string; name:string; summary:string }; rating?:{ average:number; count:number } };
	type Review = { id:string; reviewer:string; rating:number; body:string; seller_reply:string|null; created_at:string };
	let { person, preview = false } = $props<{ person:Profile; preview?:boolean }>();
	let duration = $state(30);
	$effect(() => { if (!person.durations.includes(duration)) duration = person.durations.includes(30) ? 30 : person.durations[0] ?? 30; });
	let amount = $derived(priceForDuration(person.base_30_minor, duration));
	let reviews = $state<Review[]>([]);
	onMount(async () => {
		if (preview || !person.handle || !person.rating?.count) return;
		try {
			const res = await fetch(`/api/v1/people/${encodeURIComponent(person.handle)}/reviews`);
			if (res.ok) reviews = (await res.json()).reviews;
		} catch { /* reviews are optional on this page */ }
	});
	const reviewDate = (value: string) => new Intl.DateTimeFormat(undefined, { month: 'short', year: 'numeric' }).format(new Date(value));
</script>

<section class="person-page">
	<div class="person-topline"><a href="/" class="person-brand">WANTMYTIME®</a><span>PERSONAL BOOKING / {person.handle || 'PREVIEW'}</span></div>
	{#if preview}<p class="person-preview">Private preview. Save your changes to update your page.</p>{/if}
	<div class="person-grid">
		<header class="person-identity"><p class="person-kicker">BOOK TIME WITH</p><div class="person-avatar">{#if person.avatar_url}<img src={person.avatar_url} alt="" />{:else}<span>{person.name.slice(0, 1).toUpperCase()}</span>{/if}</div><h1>{person.name}</h1>{#if person.rating?.count}<a class="person-rating" href="#reviews" aria-label={`Rated ${person.rating.average} out of 5 from ${person.rating.count} reviews`}>★ {person.rating.average.toFixed(1)} <span>· {person.rating.count} review{person.rating.count === 1 ? '' : 's'}</span></a>{/if}<p class="person-handle">wantmytime.com/{person.handle || 'yourname'}</p>{#if person.identity_url}<a class="person-identity-link" href={person.identity_url} target="_blank" rel="noopener noreferrer">{person.identity_label || 'View profile'} <span aria-hidden="true">↗</span></a>{/if}<div class="person-dial"><TimeDial {duration} compact /></div></header>
		<div class="person-action"><div class="person-action-head"><span>BOOKING / 01</span><span>{person.timezone || 'UTC'}</span></div><h2>{person.mode === 'offer' ? 'MAKE AN OFFER.' : 'FIND A TIME.'}</h2>
			{#if person.local_simulator && !preview}<p class="person-status">Development preview · no money will be collected.</p>{/if}
			{#if person.paused}<p class="person-unavailable">{person.name} isn’t taking bookings right now.</p>
			{:else if !person.ready}<p class="person-unavailable">Bookings open soon. Check back in a little while.</p>
			{:else}<fieldset class="person-duration"><legend>HOW LONG?</legend><div class="person-duration-options">{#each person.durations as d}<button type="button" class:active={duration === d} aria-pressed={duration === d} onclick={() => duration = d}><strong>{d}</strong><span>MINUTES</span></button>{/each}</div></fieldset>
				{#if person.mode !== 'offer'}<div class="person-price"><span>YOU PAY</span><strong>{formatMoney(amount, person.currency)}</strong></div>{#if preview}<button class="person-cta" disabled>PICK A TIME</button>{:else}<a class="person-cta" href={`/book/new?seller=${encodeURIComponent(person.handle)}&duration=${duration}`}>PICK A TIME <span aria-hidden="true">↗</span></a>{/if}<p class="person-fineprint">{paymentMethodsSentence(person.payment_methods)} The booking is yours the moment the payment arrives.{#if person.cancellation_policy}{' '}{person.cancellation_policy.name} cancellation: {person.cancellation_policy.summary}{/if}</p>{#if person.mode === 'both'}<div class="person-offer-alt"><p>Want to suggest a different price? {person.name} can accept, counter or say no. You only pay if you both agree.</p>{#if preview}<button class="person-cta-secondary" disabled>MAKE AN OFFER</button>{:else}<a class="person-cta-secondary" href={`/offer/new?seller=${encodeURIComponent(person.handle)}&duration=${duration}`}>MAKE AN OFFER <span aria-hidden="true">↗</span></a>{/if}</div>{/if}
				{:else}<p class="person-offer-copy">Name your price for {duration} minutes. {person.name} can accept, counter or say no. You only pay if you both agree.</p>{#if preview}<button class="person-cta" disabled>MAKE AN OFFER</button>{:else}<a class="person-cta" href={`/offer/new?seller=${encodeURIComponent(person.handle)}&duration=${duration}`}>MAKE AN OFFER <span aria-hidden="true">↗</span></a>{/if}{/if}
			{/if}
		</div>
	</div>
	{#if reviews.length}
		<section class="person-reviews" id="reviews" aria-label="Reviews">
			<h2>WHAT PEOPLE SAID</h2>
			{#each reviews as review (review.id)}
				<article>
					<p class="review-stars" aria-label={`${review.rating} out of 5`}>{'★'.repeat(review.rating)}{'☆'.repeat(5 - review.rating)}</p>
					{#if review.body}<p class="person-review-body">{review.body}</p>{/if}
					<small>{review.reviewer} · {reviewDate(review.created_at)}</small>
					{#if review.seller_reply}<p class="person-review-reply"><strong>{person.name}:</strong> {review.seller_reply}</p>{/if}
				</article>
			{/each}
		</section>
	{/if}
</section>
