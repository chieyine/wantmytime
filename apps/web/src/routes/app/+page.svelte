<script lang="ts">
	import { siteOrigin } from '$lib/site';
	import { linkLabel } from '$lib/handle';
	import { onMount } from 'svelte';
	import { formatMoney } from '$lib/money';
	import PushToggle from '$lib/components/PushToggle.svelte';
	import NewsPrompt from '$lib/components/NewsPrompt.svelte';
	import { refreshPush } from '$lib/push';
	let { data } = $props();
	let account = $derived(data.account as { name: string; email: string; handle: string });
	let copied = $state(false);
	let copyError = $state('');
	let o = $derived(data.overview);
	let bookings = $derived(o?.bookings ?? []);
	let offers = $derived(o?.offers ?? []);
	let windowCount = $derived(o?.windowCount ?? null);
	let profileReady = $derived(o?.profileReady ?? null);
	let profilePaused = $derived(o?.profilePaused ?? false);
	let hasBank = $derived(o?.hasBank ?? null);
	let onTheWay = $derived(o?.onTheWay ?? null);
	let currency = $derived(o?.currency ?? 'NGN');
	let overviewError = $derived(o?.failed ?? false);
	let link = $derived(account.handle ? `${siteOrigin()}/${account.handle}` : 'No link claimed yet');
	let nextBooking = $derived(
		bookings
			.filter(
				(booking) =>
					booking.seller === account.handle &&
					new Date(booking.starts_at).getTime() >= Date.now() &&
					booking.state === 'confirmed'
			)
			.sort((a, b) => new Date(a.starts_at).getTime() - new Date(b.starts_at).getTime())[0]
	);
	let pendingOffers = $derived(
		offers.filter(
			(offer) =>
				offer.role === 'seller' && offer.state === 'pending' && new Date(offer.expires_at).getTime() > Date.now()
		)
	);
	let nextOffer = $derived(
		[...pendingOffers].sort((a, b) => new Date(a.expires_at).getTime() - new Date(b.expires_at).getTime())[0]
	);
	let open = $derived(
		!!account.handle && (windowCount ?? 0) > 0 && hasBank === true && profileReady === true && !profilePaused
	);
	let nextSetupHref = $derived(
		!account.handle
			? '/claim'
			: windowCount === 0
				? '/app/availability'
				: hasBank === false
					? '/app/money/payouts'
					: profilePaused
						? '/app/link'
						: ''
	);
	let nextSetupLabel = $derived(
		!account.handle
			? 'CLAIM YOUR LINK'
			: windowCount === 0
				? 'SET YOUR HOURS'
				: hasBank === false
					? 'ADD YOUR PAYOUT ACCOUNT'
					: profilePaused
						? 'TURN BOOKINGS BACK ON'
						: 'SHARE YOUR LINK'
	);
	onMount(() => {
		void refreshPush().catch(() => undefined);
	});
	const dateLabel = (value: string) =>
		new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value));
	// Share where the phone offers it (WhatsApp, Instagram…), otherwise copy.
	async function share() {
		if (typeof navigator.share === 'function') {
			try {
				await navigator.share({ title: `Book time with ${account.name}`, url: link });
				return;
			} catch (error) {
				if (error instanceof DOMException && error.name === 'AbortError') return;
			}
		}
		await copy();
	}
	async function copy() {
		copyError = '';
		try {
			await navigator.clipboard.writeText(link);
			copied = true;
			window.setTimeout(() => (copied = false), 1500);
		} catch {
			copyError = 'Copy was unavailable. Select the link to copy it.';
		}
	}
</script>

<svelte:head><title>Your WantMyTime — Home</title></svelte:head>

<section class="workspace-overview">
	<header class="workspace-overview-head">
		<p class="workspace-kicker">HOME</p>
		<h1>YOUR TIME,<br /><em>{account.name?.trim().split(' ')[0] || 'YOUR SPACE'}.</em></h1>
		<p>Share your link. We’ll handle the booking and the payment.</p>
	</header>
	<section class="workspace-link-panel" aria-labelledby="workspace-link-title">
		<div>
			<p class="workspace-kicker">YOUR LINK <span>01</span></p>
			<h2 id="workspace-link-title">{account.handle ? linkLabel(link) : link}</h2>
		</div>
		<div class="workspace-link-actions">
			{#if account.handle}<button type="button" class="workspace-action" onclick={copy}
					>{copied ? 'COPIED' : 'COPY LINK'} <span aria-hidden="true">↗</span></button
				><button type="button" class="workspace-action workspace-action-outline" onclick={share}
					>SHARE <span aria-hidden="true">↗</span></button
				><a class="workspace-action workspace-action-outline" href={`/${account.handle}`}
					>VIEW PAGE <span aria-hidden="true">↗</span></a
				>{:else}<a class="workspace-action" href="/claim">CLAIM YOUR LINK <span aria-hidden="true">↗</span></a>{/if}
		</div>
		{#if copyError}<p class="workspace-inline-error" role="alert">{copyError}</p>{/if}
	</section>
	{#if overviewError}<p class="notice notice-warning workspace-overview-warning" role="status">
			Some details didn’t load. Refresh, or open a section directly.
		</p>{/if}
	<div class="workspace-overview-grid">
		<section class="workspace-readiness" aria-labelledby="workspace-readiness-title">
			<p class="workspace-kicker">{open ? 'STATUS' : 'SETUP'} <span>02</span></p>
			<h2 id="workspace-readiness-title">
				{open
					? 'YOU’RE OPEN FOR BOOKINGS.'
					: profilePaused
						? 'BOOKINGS ARE PAUSED.'
						: 'A FEW STEPS FROM YOUR FIRST BOOKING.'}
			</h2>
			<ol>
				<li><span>01</span><span>Your link <b>{account.handle ? 'Live' : 'To do'}</b></span></li>
				<li>
					<span>02</span><span
						>Your hours <b
							>{!account.handle ? 'After your link' : windowCount === null ? '—' : windowCount > 0 ? 'Set' : 'To do'}</b
						></span
					>
				</li>
				<li>
					<span>03</span><span
						>Payout account <b
							>{!account.handle ? 'After your link' : hasBank === null ? '—' : hasBank ? 'Added' : 'To do'}</b
						></span
					>
				</li>
				{#if account.handle && profileReady === false && hasBank}<li>
						<span>04</span><span>Bookings are on hold on our side <b>Contact us</b></span>
					</li>{/if}
			</ol>
			{#if nextSetupHref}<a href={nextSetupHref}>{nextSetupLabel} <span aria-hidden="true">↗</span></a>{:else}<button
					type="button"
					class="workspace-readiness-action"
					onclick={share}>{copied ? 'LINK COPIED' : nextSetupLabel} <span aria-hidden="true">↗</span></button
				>{/if}
		</section>
		<aside class="workspace-side-note">
			<p class="workspace-kicker">UP NEXT <span>03</span></p>
			<a href={nextBooking ? `/app/bookings/${encodeURIComponent(nextBooking.id)}` : '/app/bookings'}
				><span>NEXT BOOKING</span><strong
					>{nextBooking ? `${nextBooking.buyer} · ${dateLabel(nextBooking.starts_at)}` : 'Nothing booked yet'}</strong
				><b aria-hidden="true">↗</b></a
			><a href={nextOffer ? `/app/offers/${encodeURIComponent(nextOffer.id)}` : '/app/bookings'}
				><span>REQUESTS TO ANSWER</span><strong
					>{nextOffer
						? `${nextOffer.buyer_name} · ${formatMoney(Number(nextOffer.amount_minor), nextOffer.currency)}`
						: 'No offers waiting'}</strong
				><b aria-hidden="true">↗</b></a
			><a href="/app/money"
				><span>ON THE WAY TO YOU</span><strong>{onTheWay === null ? '—' : formatMoney(onTheWay, currency)}</strong><b
					aria-hidden="true">↗</b
				></a
			>
		</aside>
	</div>
	{#if account.handle}<div class="workspace-push"><PushToggle audience="seller" hideWhenOn /></div>{/if}
	{#if account.handle}<NewsPrompt />{/if}
</section>
