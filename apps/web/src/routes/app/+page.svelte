<script lang="ts">
	import { onMount } from 'svelte';
	import { env } from '$env/dynamic/public';
	import { api } from '$lib/api';
	import { formatMoney } from '$lib/money';
	import PushToggle from '$lib/components/PushToggle.svelte';
	import { refreshPush } from '$lib/push';
	let { data } = $props();
	let account = $derived(data.account as { name: string; email: string; handle: string });
	let copied = $state(false);
	let copyError = $state('');
	type Booking = { id: string; seller: string; buyer: string; duration_minutes: number; starts_at: string; state: string; payment_state: string };
	type Offer = { id: string; seller: string; buyer_name: string; amount_minor: string; state: string; role: 'seller' | 'buyer'; expires_at: string; currency?: string };
	let bookings = $state<Booking[]>([]);
	let offers = $state<Offer[]>([]);
	let windowCount = $state<number | null>(null);
	let profileReady = $state<boolean | null>(null);
	let profilePaused = $state(false);
	let hasBank = $state<boolean | null>(null);
	let onTheWay = $state<number | null>(null);
	let currency = $state('NGN');
	let overviewLoading = $state(true);
	let overviewError = $state(false);
	let link = $derived(account.handle ? `${(env.PUBLIC_APP_ORIGIN || (typeof window === 'undefined' ? '' : window.location.origin)).replace(/\/$/, '')}/${account.handle}` : 'No link claimed yet');
	let nextBooking = $derived(bookings.filter((booking) => booking.seller === account.handle && new Date(booking.starts_at).getTime() >= Date.now() && booking.state === 'confirmed').sort((a, b) => new Date(a.starts_at).getTime() - new Date(b.starts_at).getTime())[0]);
	let pendingOffers = $derived(offers.filter((offer) => offer.role === 'seller' && offer.state === 'pending' && new Date(offer.expires_at).getTime() > Date.now()));
	let nextOffer = $derived([...pendingOffers].sort((a, b) => new Date(a.expires_at).getTime() - new Date(b.expires_at).getTime())[0]);
	let open = $derived(!!account.handle && (windowCount ?? 0) > 0 && hasBank === true && profileReady === true && !profilePaused);
	let nextSetupHref = $derived(!account.handle ? '/claim' : windowCount === 0 ? '/app/availability' : hasBank === false ? '/app/settings/payouts' : profilePaused ? '/app/link' : '/app/share');
	let nextSetupLabel = $derived(!account.handle ? 'CLAIM YOUR LINK' : windowCount === 0 ? 'SET YOUR HOURS' : hasBank === false ? 'ADD YOUR PAYOUT ACCOUNT' : profilePaused ? 'TURN BOOKINGS BACK ON' : 'SHARE YOUR LINK');
	onMount(async () => {
		void refreshPush().catch(() => undefined);
		if (!account.handle) { overviewLoading = false; return; }
		const results = await Promise.allSettled([
			api<{ ready: boolean; paused: boolean; currency?: string }>('/api/v1/me/link'),
			api<{ windows: unknown[] }>('/api/v1/me/availability'),
			api<{ bookings: Booking[] }>('/api/v1/me/bookings'),
			api<{ offers: Offer[] }>('/api/v1/me/offers'),
			api<{ account: unknown | null }>('/api/v1/me/payout-account'),
			api<{ upcoming_minor: number }>('/api/v1/me/payouts')
		]);
		if (results[0].status === 'fulfilled') { profileReady = results[0].value.ready; profilePaused = results[0].value.paused; currency = results[0].value.currency || 'NGN'; }
		if (results[1].status === 'fulfilled') windowCount = results[1].value.windows.length;
		if (results[2].status === 'fulfilled') bookings = results[2].value.bookings;
		if (results[3].status === 'fulfilled') offers = results[3].value.offers;
		if (results[4].status === 'fulfilled') hasBank = results[4].value.account !== null;
		if (results[5].status === 'fulfilled') onTheWay = results[5].value.upcoming_minor;
		overviewError = results.some((result) => result.status === 'rejected');
		overviewLoading = false;
	});
	const dateLabel = (value: string) => new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value));
	async function copy() {
		copyError = '';
		try {
			await navigator.clipboard.writeText(link);
			copied = true;
			window.setTimeout(() => copied = false, 1500);
		} catch {
			copyError = 'Copy was unavailable. Select the link to copy it.';
		}
	}
</script>

<svelte:head><title>Your WantMyTime — Overview</title></svelte:head>

<section class="workspace-overview">
	<header class="workspace-overview-head">
		<p class="workspace-kicker">OVERVIEW <span>01 / 08</span></p>
		<h1>YOUR TIME,<br /><em>{account.name?.trim().split(' ')[0] || 'YOUR SPACE'}.</em></h1>
		<p>Your link, your hours, your bookings and your money, all in one place.</p>
	</header>
	<section class="workspace-link-panel" aria-labelledby="workspace-link-title">
		<div><p class="workspace-kicker">YOUR LINK <span>01</span></p><h2 id="workspace-link-title">{link}</h2></div>
		<div class="workspace-link-actions">{#if account.handle}<button type="button" class="workspace-action" onclick={copy}>{copied ? 'COPIED' : 'COPY LINK'} <span aria-hidden="true">↗</span></button><a class="workspace-action workspace-action-outline" href={`/${account.handle}`}>VIEW PAGE <span aria-hidden="true">↗</span></a>{:else}<a class="workspace-action" href="/claim">CLAIM YOUR LINK <span aria-hidden="true">↗</span></a>{/if}</div>
		{#if copyError}<p class="workspace-inline-error" role="alert">{copyError}</p>{/if}
	</section>
	{#if overviewError}<p class="notice notice-warning workspace-overview-warning" role="status">Some details didn’t load. Refresh, or open a section directly.</p>{/if}
	<div class="workspace-overview-grid">
		<section class="workspace-readiness" aria-labelledby="workspace-readiness-title">
			<p class="workspace-kicker">{open ? 'STATUS' : 'SETUP'} <span>02</span></p>
			<h2 id="workspace-readiness-title">{open ? 'YOU’RE OPEN FOR BOOKINGS.' : profilePaused ? 'BOOKINGS ARE PAUSED.' : 'A FEW STEPS FROM YOUR FIRST BOOKING.'}</h2>
			<ol><li><span>01</span><span>Your link <b>{account.handle ? 'Live' : 'To do'}</b></span></li><li><span>02</span><span>Your hours <b>{!account.handle ? 'After your link' : windowCount === null ? (overviewLoading ? 'Checking' : '—') : windowCount > 0 ? 'Set' : 'To do'}</b></span></li><li><span>03</span><span>Payout account <b>{!account.handle ? 'After your link' : hasBank === null ? (overviewLoading ? 'Checking' : '—') : hasBank ? 'Added' : 'To do'}</b></span></li>{#if account.handle && profileReady === false && hasBank}<li><span>04</span><span>Bookings are on hold on our side <b>Contact us</b></span></li>{/if}</ol>
			<a href={nextSetupHref}>{nextSetupLabel} <span aria-hidden="true">↗</span></a>
		</section>
		<aside class="workspace-side-note"><p class="workspace-kicker">UP NEXT <span>03</span></p><a href={nextBooking ? `/app/bookings/${encodeURIComponent(nextBooking.id)}` : '/app/bookings'}><span>NEXT BOOKING</span><strong>{overviewLoading ? 'Checking…' : nextBooking ? `${nextBooking.buyer} · ${dateLabel(nextBooking.starts_at)}` : 'Nothing booked yet'}</strong><b aria-hidden="true">↗</b></a><a href={nextOffer ? `/app/offers/${encodeURIComponent(nextOffer.id)}` : '/app/offers'}><span>OFFERS TO ANSWER</span><strong>{overviewLoading ? 'Checking…' : nextOffer ? `${nextOffer.buyer_name} · ${formatMoney(Number(nextOffer.amount_minor), nextOffer.currency)}` : 'No offers waiting'}</strong><b aria-hidden="true">↗</b></a><a href="/app/money"><span>ON THE WAY TO YOU</span><strong>{onTheWay === null ? (overviewLoading ? 'Checking…' : '—') : formatMoney(onTheWay, currency)}</strong><b aria-hidden="true">↗</b></a></aside>
	</div>
	{#if account.handle}<div class="workspace-push"><PushToggle audience="seller" hideWhenOn /></div>{/if}
</section>
