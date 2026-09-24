<script lang="ts">
	import { onMount } from 'svelte';
	import TimeDial from '$lib/components/TimeDial.svelte';
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
		const queue = () => { if (!frame) frame = requestAnimationFrame(update); };
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
		if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(normalized) || normalized.length < 3 || normalized.length > 24) { handleError = 'Use 3–24 letters, numbers or single hyphens.'; return; }
		location.href = `/claim?handle=${encodeURIComponent(normalized)}`;
	}
</script>

<svelte:head><title>WantMyTime — friends free, everyone else books</title><meta name="description" content="Friends get your time free. Everyone else books it: one link where people you don’t know pick a time and pay by bank transfer, and you’re paid after the call." /></svelte:head>
<div class="brand-home">
	<section class="brand-hero">
		<div class="brand-hero-top"><span>FRIENDS GET IT FREE</span><span>EVERYONE ELSE BOOKS</span></div>
		<div class="brand-hero-grid">
			<div class="brand-hero-copy"><p class="brand-overline">FOR THE STRANGERS WHO ASK FOR YOUR TIME</p><h1><span>WANT MY TIME?</span><br /><span><em>BOOK IT.</em></span></h1><p class="brand-description">Friends get your time free. Everyone else books it. Share one link: people you don’t know pick a time and pay by transfer, and you’re paid after the call.</p><form class="brand-claim" onsubmit={(event) => { event.preventDefault(); claim(); }}><label for="brand-handle">START WITH YOUR NAME</label><div><span>wantmytime.com/</span><input id="brand-handle" bind:value={handle} placeholder="yourname" autocomplete="off" /><button type="submit">CLAIM YOUR LINK <span aria-hidden="true">↗</span></button></div>{#if handleError}<p role="alert">{handleError}</p>{/if}</form><p class="brand-note">Free to create. A small fee only when someone books.</p></div>
			<div class="brand-timepiece"><TimeDial {duration} /><div class="brand-duration"><span>CHOOSE A LENGTH</span><div role="group" aria-label="Explore conversation lengths">{#each [15, 30, 60] as option}<button type="button" class:active={duration === option} aria-pressed={duration === option} onclick={() => duration = option}>{option}<small>MIN</small></button>{/each}</div></div><p class="brand-timepiece-note" aria-live="polite">{durationNotes[duration]}</p></div>
		</div>
		<div class="brand-hero-bottom"><span>ONE LINK</span><span>YOUR HOURS</span><span>YOUR PRICE</span><span>PAID AFTER THE CALL</span></div>
	</section>
	<section class="brand-method" id="how" bind:this={methodSection} style={`--method-progress: ${methodProgress}`}><div class="brand-method-title"><p>THE METHOD / 01—03</p><h2>SAY YES<br />WITHOUT<br /><i>BEING USED.</i></h2></div><div class="brand-method-steps"><article class:active={activeStep === 0}><span>01</span><div><h3>SET YOUR TERMS</h3><p>Choose a price or accept offers. Open only the hours you’re happy to give people you don’t know.</p></div></article><article class:active={activeStep === 1}><span>02</span><div><h3>SHARE ONE LINK</h3><p>Put it where strangers find you: your bio, your email signature, your reply to “can I pick your brain?”</p></div></article><article class:active={activeStep === 2}><span>03</span><div><h3>GET PAID AFTER</h3><p>They pick a time and pay by transfer. WantMyTime holds the money and pays you about three hours after the call.</p></div></article></div></section>
	<section class="brand-fee"><p>PRICING / NO SUBSCRIPTION</p><div><h2>KEEP THE<br />VALUE OF<br />YOUR TIME.</h2><div><strong>≤5%</strong><p>The most we take, only when you’re booked. On a ₦10,000 booking, that is no more than ₦500.</p><a href="/pricing">SEE HOW PRICING WORKS <span aria-hidden="true">↗</span></a></div></div></section>
</div>
