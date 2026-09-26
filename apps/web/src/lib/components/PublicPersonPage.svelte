<script lang="ts">
	import { formatMoney, priceForDuration, paymentMethodsSentence } from '$lib/money';
	import { onMount } from 'svelte';
	type Profile = {
		handle: string;
		name: string;
		identity_url?: string;
		identity_label?: string;
		avatar_url?: string;
		timezone: string;
		base_30_minor: number | string;
		durations: number[];
		paused: boolean;
		ready: boolean;
		mode: 'fixed' | 'both';
		currency?: string;
		payment_methods?: string[];
		local_simulator?: boolean;
		provider_checkout_enabled?: boolean;
		cancellation_policy?: { name: string; summary: string };
		rating?: { average: number; count: number };
	};
	type Review = {
		id: string;
		reviewer: string;
		rating: number;
		body: string;
		seller_reply: string | null;
		created_at: string;
	};
	let { person, preview = false } = $props<{ person: Profile; preview?: boolean }>();
	let duration = $state(30);
	$effect(() => {
		if (!person.durations.includes(duration))
			duration = person.durations.includes(30) ? 30 : (person.durations[0] ?? 30);
	});
	let amount = $derived(priceForDuration(person.base_30_minor, duration));
	let reviews = $state<Review[]>([]);
	onMount(async () => {
		if (preview || !person.handle || !person.rating?.count) return;
		try {
			const res = await fetch(`/api/v1/people/${encodeURIComponent(person.handle)}/reviews`);
			if (res.ok) reviews = (await res.json()).reviews;
		} catch {
			/* reviews are optional on this page */
		}
	});
	let firstName = $derived(person.name.trim().split(/\s+/)[0] || person.name);
	let initials = $derived(
		person.name
			.trim()
			.split(/\s+/)
			.slice(0, 2)
			.map((part: string) => part.slice(0, 1).toUpperCase())
			.join('')
	);
	// The seller's own clock, so a buyer elsewhere knows whether it is morning or night there.
	let now = $state(new Date());
	onMount(() => {
		const timer = setInterval(() => (now = new Date()), 30_000);
		return () => clearInterval(timer);
	});
	let theirTime = $derived.by(() => {
		try {
			return new Intl.DateTimeFormat(undefined, {
				hour: 'numeric',
				minute: '2-digit',
				timeZone: person.timezone || 'UTC'
			}).format(now);
		} catch {
			return '';
		}
	});
	let place = $derived((person.timezone || 'UTC').split('/').pop()?.replace(/_/g, ' ') ?? 'UTC');
	const reviewDate = (value: string) =>
		new Intl.DateTimeFormat(undefined, { month: 'short', year: 'numeric' }).format(new Date(value));
</script>

<section class="pp">
	<header class="pp-top">
		<a href="/" class="wordmark pp-brand">WantMyTime<span class="wordmark-period">.</span></a>
		{#if !preview}<a class="pp-own" href="/claim">Get your own link <span aria-hidden="true">↗</span></a>{/if}
	</header>
	{#if preview}<p class="pp-preview">Private preview. Save your changes to update your page.</p>{/if}
	<div class="pp-grid">
		<div class="pp-profile">
			<div class="pp-avatar">
				{#if person.avatar_url}<img src={person.avatar_url} alt="" />{:else}<span>{initials}</span>{/if}
			</div>
			<p class="pp-kicker">Book time with</p>
			<h1>{person.name}</h1>
			{#if person.rating?.count}<a
					class="pp-rating"
					href="#reviews"
					aria-label={`Rated ${person.rating.average} out of 5 from ${person.rating.count} reviews`}
					><span aria-hidden="true">★</span>
					{person.rating.average.toFixed(1)}
					<small>· {person.rating.count} review{person.rating.count === 1 ? '' : 's'}</small></a
				>{/if}
			<ul class="pp-facts">
				{#if theirTime}<li>
						<span>Local time</span><strong>{theirTime} in {place}</strong>
					</li>{/if}

				{#if person.identity_url}<li>
						<span>Find them at</span><a href={person.identity_url} target="_blank" rel="noopener noreferrer"
							>{person.identity_label || 'View profile'} <span aria-hidden="true">↗</span></a
						>
					</li>{/if}
			</ul>
			<ol class="pp-steps" aria-label="How booking works">
				<li><b>1</b>Choose how long</li>
				<li><b>2</b>Pick a free time</li>
				<li><b>3</b>Pay, and it’s booked</li>
			</ol>
		</div>
		<div class="pp-card">
			{#if person.local_simulator && !preview}<p class="pp-status">
					Development preview · no money will be collected.
				</p>{/if}
			<h2>Book a call with {firstName}</h2>
			{#if person.paused}<p class="pp-unavailable">{person.name} isn’t taking bookings right now.</p>
			{:else if !person.ready}<p class="pp-unavailable">Bookings open soon. Check back in a little while.</p>
			{:else}<fieldset class="pp-lengths">
					<legend>How long?</legend>
					<div class="pp-length-options" style={`--count:${Math.min(person.durations.length, 3)}`}>
						{#each person.durations as d (d)}<button
								type="button"
								class:active={duration === d}
								aria-pressed={duration === d}
								onclick={() => (duration = d)}
								><strong>{d} min</strong><span
									>{formatMoney(priceForDuration(person.base_30_minor, d), person.currency)}</span
								></button
							>{/each}
					</div>
				</fieldset>
				<div class="pp-total">
					<span>{duration} minutes</span><strong>{formatMoney(amount, person.currency)}</strong>
				</div>
				{#if preview}<button class="pp-cta" disabled>Pick a time</button>{:else}<a
						class="pp-cta"
						href={`/book/new?seller=${encodeURIComponent(person.handle)}&duration=${duration}`}
						>Pick a time <span aria-hidden="true">→</span></a
					>{/if}
				<ul class="pp-assurances">
					<li>{paymentMethodsSentence(person.payment_methods)}</li>
					<li>Confirmed the moment your payment arrives.</li>
					{#if person.cancellation_policy}<li>
							{person.cancellation_policy.summary}
						</li>{/if}
				</ul>
				{#if person.mode === 'both'}<div class="pp-offer-alt">
						<p>
							<strong>Have a different price in mind?</strong> Suggest one. {firstName} can accept, counter or say no, and
							you only pay if you both agree.
						</p>
						{#if preview}<button class="pp-cta-secondary" disabled>Make an offer</button>{:else}<a
								class="pp-cta-secondary"
								href={`/offer/new?seller=${encodeURIComponent(person.handle)}&duration=${duration}`}
								>Make an offer <span aria-hidden="true">→</span></a
							>{/if}
					</div>{/if}
			{/if}
		</div>
	</div>
	{#if reviews.length}
		<section class="pp-reviews" id="reviews" aria-label="Reviews">
			<h2>What people said</h2>
			<div>
				{#each reviews as review (review.id)}
					<article>
						<p class="review-stars" aria-label={`${review.rating} out of 5`}>
							{'★'.repeat(review.rating)}{'☆'.repeat(5 - review.rating)}
						</p>
						{#if review.body}<p class="pp-review-body">{review.body}</p>{/if}
						<small>{review.reviewer} · {reviewDate(review.created_at)}</small>
					</article>
				{/each}
			</div>
		</section>
	{/if}
	<footer class="pp-foot">
		<span>wantmytime.com/{person.handle || 'yourname'}</span>
		<span>Booked with WantMyTime</span>
	</footer>
</section>
