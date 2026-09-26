<script lang="ts">
	import PageMeta from '$lib/components/PageMeta.svelte';
	import { onMount } from 'svelte';
	import TimeDial from '$lib/components/TimeDial.svelte';
	import { formatMoney, parseMoneyToMinor } from '$lib/money';
	// Pricing lives here now: one number, and what you keep.
	let gross = $state('10000');
	let grossMinor = $derived(Number(parseMoneyToMinor(gross) ?? 0n));
	let fee = $derived(Math.floor((grossMinor * 500) / 10000));
	let handle = $state('');
	let handleError = $state('');
	let duration = $state(30);
	let methodProgress = $state(0);
	let methodSection = $state<HTMLElement>();
	const durationNotes: Record<number, string> = {
		15: 'A focused question. Fifteen minutes, fully yours.',
		30: 'Enough room for a real conversation.',
		60: 'An hour to give a big idea proper attention.'
	};
	let activeStep = $derived(Math.min(2, Math.floor(methodProgress * 3)));
	onMount(() => {
		let frame = 0;
		const update = () => {
			frame = 0;
			if (!methodSection) return;
			const bounds = methodSection.getBoundingClientRect();
			const distance = bounds.height + window.innerHeight * 0.35;
			methodProgress = Math.max(0, Math.min(1, (window.innerHeight * 0.65 - bounds.top) / distance));
		};
		const queue = () => {
			if (!frame) frame = requestAnimationFrame(update);
		};
		update();
		window.addEventListener('scroll', queue, { passive: true });
		window.addEventListener('resize', queue);
		return () => {
			window.removeEventListener('scroll', queue);
			window.removeEventListener('resize', queue);
			if (frame) cancelAnimationFrame(frame);
		};
	});
	function claim() {
		handleError = '';
		const normalized = handle.trim().toLowerCase();
		if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(normalized) || normalized.length < 3 || normalized.length > 24) {
			handleError = 'Use 3–24 letters, numbers or single hyphens.';
			return;
		}
		location.href = `/claim?handle=${encodeURIComponent(normalized)}`;
	}
</script>

<PageMeta
	title="WantMyTime — put a price on your time"
	description="One link for anyone who wants your time. They pick a slot and pay. You get paid after the call."
/>
<div class="brand-home">
	<section class="brand-hero">
		<div class="brand-hero-top"><span>YOUR TIME HAS A PRICE</span><span>NAME IT</span></div>
		<div class="brand-hero-grid">
			<div class="brand-hero-copy">
				<p class="brand-overline">FOR EVERYONE WHO KEEPS GETTING ASKED</p>
				<h1><span>WANT MY TIME?</span><br /><span><em>BOOK IT.</em></span></h1>
				<p class="brand-description">
					One link for anyone who wants your time. They pick a slot in their own timezone, pay, and show up. The money
					lands in your account about three hours after the call.
				</p>
				<form
					class="brand-claim"
					onsubmit={(event) => {
						event.preventDefault();
						claim();
					}}
				>
					<label for="brand-handle">START WITH YOUR NAME</label>
					<div>
						<span>wantmytime.com/</span><input
							id="brand-handle"
							bind:value={handle}
							placeholder="yourname"
							autocomplete="off"
						/><button type="submit">CLAIM YOUR LINK <span aria-hidden="true">↗</span></button>
					</div>
					{#if handleError}<p role="alert">{handleError}</p>{/if}
				</form>
				<p class="brand-note">Free to set up. You only pay when you get paid.</p>
			</div>
			<div class="brand-timepiece">
				<TimeDial {duration} />
				<div class="brand-duration">
					<span>CHOOSE A LENGTH</span>
					<div role="group" aria-label="Explore conversation lengths">
						{#each [15, 30, 60] as option (option)}<button
								type="button"
								class:active={duration === option}
								aria-pressed={duration === option}
								onclick={() => (duration = option)}>{option}<small>MIN</small></button
							>{/each}
					</div>
				</div>
				<p class="brand-timepiece-note" aria-live="polite">{durationNotes[duration]}</p>
			</div>
		</div>
		<div class="brand-hero-bottom">
			<span>ONE LINK</span><span>YOUR HOURS</span><span>YOUR PRICE</span><span>PAID AFTER THE CALL</span>
		</div>
	</section>
	<section class="brand-method" id="how" bind:this={methodSection} style={`--method-progress: ${methodProgress}`}>
		<div class="brand-method-title">
			<p>THE METHOD / 01—03</p>
			<h2>SAY YES<br />WITHOUT<br /><i>BEING USED.</i></h2>
		</div>
		<div class="brand-method-steps">
			<article class:active={activeStep === 0}>
				<span>01</span>
				<div>
					<h3>NAME YOUR PRICE</h3>
					<p>
						Decide what 15, 30 or 60 minutes of you costs, or let people make an offer. Open only the hours you want to
						give.
					</p>
				</div>
			</article>
			<article class:active={activeStep === 1}>
				<span>02</span>
				<div>
					<h3>DROP YOUR LINK</h3>
					<p>
						Bio, email signature, DMs. The next time someone asks “can I pick your brain?”, you have a one-line answer.
					</p>
				</div>
			</article>
			<article class:active={activeStep === 2}>
				<span>03</span>
				<div>
					<h3>GET PAID</h3>
					<p>
						They pay before the call. We hold the money, and it reaches your bank about three hours after you hang up.
						No chasing anyone.
					</p>
				</div>
			</article>
		</div>
	</section>
	<section class="brand-pricing" id="pricing" aria-labelledby="pricing-title">
		<p class="brand-pricing-kicker">PRICING</p>
		<div class="brand-pricing-grid">
			<div>
				<h2 id="pricing-title">5%. That’s it.</h2>
				<p>
					No subscription, no setup fee. When someone pays for your time, we keep 5% and send you the rest, about three
					hours after the call. If nobody books, you pay nothing.
				</p>
				<ul>
					<li>Buyers pay before the call, by bank transfer, pay with bank or mobile money.</li>
					<li>
						If they cancel at least 24 hours before, they get their money back. If you cancel, they get it all back.
					</li>
					<li>People can book from anywhere and see your times in their own timezone.</li>
				</ul>
			</div>
			<div class="calculator">
				<label
					>They pay you
					<div class="money-input">
						<span>₦</span><input bind:value={gross} inputmode="decimal" aria-label="Booking price in naira" />
					</div></label
				>
				<div><span>We keep (5%)</span><strong>{formatMoney(fee, 'NGN')}</strong></div>
				<div><span>You get</span><strong>{formatMoney(grossMinor - fee, 'NGN')}</strong></div>
				<small>Payment charges are paid by the person booking, not taken from your share.</small>
			</div>
		</div>
	</section>
	<section class="brand-fee">
		<p>YOUR LINK / 60 SECONDS</p>
		<div>
			<h2>STOP<br />GIVING IT<br />AWAY.</h2>
			<div>
				<p class="brand-fee-lead">Your time already has a price. Now people can pay it.</p>
				<a class="brand-fee-cta" href="/claim">CLAIM YOUR LINK <span aria-hidden="true">↗</span></a>
				<p>
					Free to set up. We keep 5% of each paid booking and nothing else. <a href="#pricing">How pricing works</a>
				</p>
			</div>
		</div>
	</section>
</div>
