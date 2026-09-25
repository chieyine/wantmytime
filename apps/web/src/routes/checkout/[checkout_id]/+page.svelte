<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api';
	import { formatMoney, methodLabels } from '$lib/money';
	let { params } = $props();

	type Quote = {
		id: string;
		state: string;
		gross_minor: string;
		currency?: string;
		seller_country?: string;
		duration_minutes: number;
		starts_at: string;
		expires_at: string;
		seller: string;
		seller_name?: string;
		offer_id?: string | null;
		transfer_fee_minor?: number;
		method_fees?: Record<string, number>;
		booking_id: string | null;
		booking_payment_state: string | null;
		local_simulator: boolean;
		provider_checkout_enabled: boolean;
		cancellation_policy?: { name: string; summary: string };
		payment_methods?: string[];
		problem_window_minutes?: number;
	};
	type Transfer = {
		account_number: string;
		account_name: string;
		bank_name: string;
		amount_minor: number;
		fee_minor?: number;
		expires_at: string;
	};
	type Checkout = { reference: string; method: string; authorization_url?: string; transfer?: Transfer };

	let quote = $state<Quote | null>(null);
	let loading = $state(true);
	let busy = $state(false);
	let message = $state('');
	let transfer = $state<Transfer | null>(null);
	let reference = $state('');
	let status = $state<'waiting' | 'underpaid' | 'checking' | 'expired'>('waiting');
	let copied = $state('');
	let now = $state(Date.now());
	let poll: ReturnType<typeof setInterval> | undefined;
	let tick: ReturnType<typeof setInterval> | undefined;

	const currency = $derived(quote?.currency || 'NGN');
	const money = (minor: number | string) => formatMoney(minor, currency);
	const methods = $derived(quote?.payment_methods ?? []);
	const primary = $derived(methods[0] ?? '');
	const others = $derived(methods.slice(1));
	const windowHours = $derived(Math.round((quote?.problem_window_minutes ?? 120) / 60));
	const remaining = $derived(transfer ? Math.max(0, new Date(transfer.expires_at).getTime() - now) : 0);
	const remainingLabel = $derived(
		`${Math.floor(remaining / 60000)}:${String(Math.floor((remaining % 60000) / 1000)).padStart(2, '0')}`
	);
	const feeFor = (method: string) =>
		quote?.method_fees?.[method] ?? (method === primary ? (quote?.transfer_fee_minor ?? 0) : 0);
	const againHref = $derived(
		quote
			? quote.offer_id
				? `/offer/${encodeURIComponent(quote.offer_id)}`
				: `/book/new?seller=${encodeURIComponent(quote.seller)}&duration=${quote.duration_minutes}`
			: '/'
	);

	onMount(async () => {
		try {
			quote = await api<Quote>(`/api/v1/quotes/${encodeURIComponent(params.checkout_id)}`);
		} catch (error) {
			message = error instanceof Error ? error.message : 'Checkout could not be loaded.';
		} finally {
			loading = false;
		}
	});
	onDestroy(() => {
		clearInterval(poll);
		clearInterval(tick);
	});

	async function simulate() {
		if (!quote) return;
		busy = true;
		message = '';
		try {
			const result = await api<{ booking_id: string }>(
				`/api/v1/dev/quotes/${encodeURIComponent(quote.id)}/simulate-payment`,
				{ method: 'POST', body: '{}' }
			);
			window.location.href = `/booking/${encodeURIComponent(result.booking_id)}`;
		} catch (error) {
			message = error instanceof Error ? error.message : 'The local booking could not be confirmed.';
			busy = false;
		}
	}

	async function start(method: string) {
		if (!quote) return;
		busy = true;
		message = '';
		try {
			const result = await api<Checkout>(`/api/v1/quotes/${encodeURIComponent(quote.id)}/checkout`, {
				method: 'POST',
				body: JSON.stringify({ method })
			});
			reference = result.reference;
			if (result.authorization_url) {
				window.location.assign(result.authorization_url);
				return;
			}
			if (result.transfer) {
				transfer = result.transfer;
				status = 'waiting';
				clearInterval(poll);
				clearInterval(tick);
				poll = setInterval(check, 5000);
				tick = setInterval(() => {
					now = Date.now();
					if (transfer && new Date(transfer.expires_at).getTime() <= now) {
						status = 'expired';
						clearInterval(poll);
						clearInterval(tick);
					}
				}, 1000);
			}
		} catch (error) {
			message = error instanceof Error ? error.message : 'Payment could not be started. Your time is still held.';
		} finally {
			busy = false;
		}
	}

	async function check() {
		if (!quote || !reference) return;
		try {
			const result = await api<{ state: string; booking_id: string | null }>(
				`/api/v1/quotes/${encodeURIComponent(quote.id)}/verify-payment`,
				{ method: 'POST', body: JSON.stringify({ reference }) }
			);
			if (result.booking_id) {
				clearInterval(poll);
				clearInterval(tick);
				window.location.replace(`/booking/${encodeURIComponent(result.booking_id)}`);
			} else if (result.state === 'underpaid') {
				status = 'underpaid';
			} else if (result.state === 'review') {
				clearInterval(poll);
				message =
					'Your payment arrived, but the time couldn’t be booked. Don’t pay again: the full amount is being returned to you, and we’ve emailed you the details.';
			}
		} catch {
			// A missed check is fine; the next one retries.
		}
	}

	async function copy(label: string, value: string) {
		try {
			await navigator.clipboard.writeText(value);
			copied = label;
			setTimeout(() => (copied = ''), 2000);
		} catch {
			copied = '';
		}
	}

	function methodNote(method: string): string {
		const fee = feeFor(method);
		const feeLine =
			fee > 0
				? `The payment provider’s charge of about ${money(fee)} is added to the price; you see the exact total before you pay. `
				: 'The payment provider’s charge is added to the price; you see the exact total before you pay. ';
		switch (method) {
			case 'bank_transfer':
				return `${feeLine}You’ll get an account number for this booking only. Transfer from any bank app; your booking is confirmed the moment the money arrives.`;
			case 'mobile_money':
				return `${feeLine}Approve the payment on your phone when your mobile money network asks.`;
			case 'pay_with_bank':
				return `${feeLine}Log in to your bank from the payment page and approve it there.`;
		}
		return feeLine;
	}

	const dateLabel = (value: string) =>
		new Intl.DateTimeFormat(undefined, {
			weekday: 'long',
			day: 'numeric',
			month: 'long',
			year: 'numeric',
			hour: 'numeric',
			minute: '2-digit',
			timeZoneName: 'short'
		}).format(new Date(value));
	const clock = (value: string) =>
		new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: '2-digit' }).format(new Date(value));
</script>

<svelte:head><title>Review your time — WantMyTime</title></svelte:head>
<section class="form-page">
	<p class="eyebrow">Almost done</p>
	<h1 class="page-heading">{transfer ? 'Make the transfer.' : 'Pay and you’re booked.'}</h1>
	{#if loading}
		<p class="page-intro">Loading your booking details…</p>
	{:else if !quote}
		<div class="notice notice-warning">{message}</div>
	{:else}
		<div class="appointment-slip">
			<div><small>WITH</small><strong>{quote.seller_name || quote.seller}</strong></div>
			<div><small>WHEN</small><strong>{dateLabel(quote.starts_at)}</strong></div>
			<div><small>HOW LONG</small><strong>{quote.duration_minutes} minutes</strong></div>
			<div><small>PRICE</small><strong>{money(quote.gross_minor)}</strong></div>
		</div>

		{#if quote.state === 'expired'}
			<div class="notice notice-warning">
				That hold ran out, so the time was released. Pick it again if it’s still free.
			</div>
			<a class="button button-secondary" href={againHref}>Pick a time again</a>
		{:else if quote.local_simulator && quote.state === 'held'}
			<div class="notice notice-warning">Test setup: this button confirms the booking without any money moving.</div>
			{#if message}<p class="notice notice-warning" role="alert">{message}</p>{/if}
			<button class="button" onclick={simulate} disabled={busy}
				>{busy ? 'Confirming local test…' : 'Confirm test booking'} <span>↗</span></button
			>
		{:else if quote.state === 'converted'}
			<div class="notice notice-info">
				{quote.booking_payment_state === 'simulated'
					? 'This booking was confirmed in the local simulator. No money was collected.'
					: 'Your payment was received and your booking is confirmed.'}
			</div>
			{#if quote.booking_id}<a class="button" href={`/booking/${encodeURIComponent(quote.booking_id)}`}
					>View booking ↗</a
				>{/if}
		{:else if quote.provider_checkout_enabled && quote.state === 'held' && methods.length}
			{#if message}<p class="notice notice-warning" role="alert">{message}</p>{/if}

			{#if transfer && status !== 'expired'}
				<div class="transfer-box" aria-live="polite">
					<p class="transfer-lead">
						Transfer exactly <strong>{money(transfer.amount_minor)}</strong> from your bank app to:
					</p>
					<dl>
						<div>
							<dt>Bank</dt>
							<dd>{transfer.bank_name}</dd>
						</div>
						<div>
							<dt>Account number</dt>
							<dd class="transfer-number">
								{transfer.account_number}
								<button type="button" class="copy-button" onclick={() => copy('number', transfer!.account_number)}
									>{copied === 'number' ? 'Copied' : 'Copy'}</button
								>
							</dd>
						</div>
						<div>
							<dt>Account name</dt>
							<dd>{transfer.account_name}</dd>
						</div>
						<div>
							<dt>Amount</dt>
							<dd>
								{money(transfer.amount_minor)}
								<button
									type="button"
									class="copy-button"
									onclick={() =>
										copy('amount', (transfer!.amount_minor / 100).toFixed(transfer!.amount_minor % 100 ? 2 : 0))}
									>{copied === 'amount' ? 'Copied' : 'Copy'}</button
								>
							</dd>
						</div>
					</dl>
					{#if transfer.fee_minor}<p class="form-note">
							That’s {money(quote.gross_minor)} for the session plus the payment provider’s charge of {money(
								transfer.fee_minor
							)}.
						</p>{/if}
					<p class="form-note">
						This account is for this booking only. Use it before {clock(transfer.expires_at)} ({remainingLabel} left).
					</p>
					{#if status === 'underpaid'}
						<p class="notice notice-warning">
							The amount that arrived was less than {money(transfer.amount_minor)}, so it will be sent back to you.
							Please transfer the exact amount.
						</p>
					{:else}
						<p class="transfer-waiting">
							<span class="pulse" aria-hidden="true"></span> Waiting for your transfer. This page confirms your booking as
							soon as it arrives, usually within a minute. You can close it; we’ll email you when it’s confirmed.
						</p>
					{/if}
				</div>
				{#each others as method (method)}<button class="text-link" onclick={() => start(method)} disabled={busy}
						>Pay by {(methodLabels[method] ?? method).toLowerCase()} instead</button
					>{/each}
			{:else}
				{#if status === 'expired'}<p class="notice notice-warning">
						That account has expired. Don’t transfer to it. If you already did, the money is returned automatically. Get
						new details below.
					</p>{/if}
				<button class="button" onclick={() => start(primary)} disabled={busy}
					>{busy ? 'Getting your payment ready…' : `Pay by ${(methodLabels[primary] ?? primary).toLowerCase()}`}</button
				>
				<p class="form-note">{methodNote(primary)}</p>
				{#each others as method (method)}
					<button class="text-link" onclick={() => start(method)} disabled={busy}
						>Pay by {(methodLabels[method] ?? method).toLowerCase()} instead</button
					>
					<p class="form-note">{methodNote(method)}</p>
				{/each}
			{/if}

			{#if quote.cancellation_policy}<p class="form-note">
					<strong>{quote.cancellation_policy.name} cancellation.</strong>
					{quote.cancellation_policy.summary} If the seller cancels, you get a full refund.
				</p>{/if}
			<p class="form-note">
				WantMyTime holds your payment until after the session. If something goes wrong, report it from your booking page
				within {windowHours} hour{windowHours === 1 ? '' : 's'} of the end, and the seller isn’t paid until it’s sorted out.
			</p>
		{:else}
			<div class="notice notice-warning">Payments for this booking aren’t switched on yet. No payment was taken.</div>
		{/if}
	{/if}
</section>
