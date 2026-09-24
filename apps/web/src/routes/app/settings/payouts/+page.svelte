<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { formatNaira } from '$lib/money';

	type Account = { bank_name: string; account_last4: string; account_name: string; verified_at: string; usable_from: string; in_safety_hold: boolean };
	type Payout = { id: string; booking_id: string; buyer_name: string; state: string; entitlement_minor: number; amount_minor: number | null; transfer_minor?: number; recovery_minor: number; release_at: string; paid_at: string | null; bank_name: string; account_last4: string; hold: string; hold_text?: string; note?: string };
	type Bank = { code: string; name: string };

	let account = $state<Account | null>(null);
	let delayMinutes = $state(180);
	let windowMinutes = $state(120);
	let payouts = $state<Payout[]>([]);
	let upcoming = $state(0);
	let paid = $state(0);
	let paused = $state(false);
	let loaded = $state(false);
	let message = $state('');

	let editing = $state(false);
	let banks = $state<Bank[]>([]);
	let bankCode = $state('');
	let accountNumber = $state('');
	let resolvedName = $state('');
	let checking = $state(false);
	let saving = $state(false);
	let formMessage = $state('');

	const hours = (m: number) => (m % 60 === 0 ? `${m / 60} hour${m === 60 ? '' : 's'}` : `${m} minutes`);
	const when = (v: string) => new Date(v).toLocaleString(undefined, { weekday: 'short', day: 'numeric', month: 'short', hour: 'numeric', minute: '2-digit' });
	const digits = $derived(accountNumber.replace(/[\s-]/g, ''));

	async function load() {
		try {
			const a = await api<{ account: Account | null; payout_delay_minutes: number; dispute_window_minutes: number }>('/api/v1/me/payout-account');
			account = a.account;
			delayMinutes = a.payout_delay_minutes;
			windowMinutes = a.dispute_window_minutes;
			const p = await api<{ payouts: Payout[]; upcoming_minor: number; paid_minor: number; paused: boolean }>('/api/v1/me/payouts');
			payouts = p.payouts;
			upcoming = p.upcoming_minor;
			paid = p.paid_minor;
			paused = p.paused;
			if (!account) await startEditing();
		} catch (e) {
			message = e instanceof Error ? e.message : 'Payouts could not be loaded.';
		} finally {
			loaded = true;
		}
	}
	onMount(load);

	async function startEditing() {
		editing = true;
		formMessage = '';
		if (banks.length === 0) {
			try {
				banks = (await api<{ banks: Bank[] }>('/api/v1/payout-banks')).banks.sort((a, b) => a.name.localeCompare(b.name));
			} catch (e) {
				formMessage = e instanceof Error ? e.message : 'The bank list could not be loaded.';
			}
		}
	}

	let checkedKey = '';
	async function check() {
		const key = `${bankCode}:${digits}`;
		if (key === checkedKey && resolvedName) return; // already confirmed; blur on "Save" must not undo it
		resolvedName = '';
		formMessage = '';
		checkedKey = key;
		if (!bankCode || digits.length !== 10) return;
		checking = true;
		try {
			const name = (await api<{ account_name: string }>('/api/v1/me/payout-account/resolve', { method: 'POST', body: JSON.stringify({ bank_code: bankCode, account_number: digits }) })).account_name;
			if (checkedKey === key) resolvedName = name; // ignore answers for an earlier entry
		} catch (e) {
			formMessage = e instanceof Error ? e.message : 'The account could not be checked.';
		} finally {
			checking = false;
		}
	}

	async function save() {
		saving = true;
		formMessage = '';
		try {
			const replacing = account !== null;
			account = (await api<{ account: Account }>('/api/v1/me/payout-account', { method: 'PUT', body: JSON.stringify({ bank_code: bankCode, account_number: digits }) })).account;
			editing = false;
			accountNumber = bankCode = resolvedName = '';
			message = replacing ? 'Bank account changed. For your safety, payouts go to the new account from 24 hours from now.' : 'Bank account saved. Payouts will be sent here.';
		} catch (e) {
			formMessage = e instanceof Error ? e.message : 'The account could not be saved.';
		} finally {
			saving = false;
		}
	}

	function status(p: Payout): { label: string; detail: string; tone: string } {
		switch (p.state) {
			case 'paid':
				return { label: 'Paid', detail: `Sent ${p.paid_at ? when(p.paid_at) : ''} to ${p.bank_name} ••${p.account_last4}`, tone: '' };
			case 'processing':
				return { label: 'Sending', detail: p.note || 'On its way to your bank.', tone: '' };
			case 'failed':
				return { label: 'Needs attention', detail: 'The transfer did not go through. WantMyTime has been alerted and will retry once it is fixed.', tone: 'alert-critical' };
			case 'cancelled':
				return { label: 'No payout', detail: 'The buyer was refunded in full.', tone: '' };
			default:
				if (p.hold) return { label: 'On hold', detail: p.hold_text || 'On hold.', tone: 'alert-warning' };
				if (new Date(p.release_at) > new Date()) return { label: 'Scheduled', detail: `Due ${when(p.release_at)}`, tone: '' };
				return { label: 'Due', detail: p.note || 'Being prepared.', tone: '' };
		}
	}
	const shown = (p: Payout) => (p.state === 'scheduled' ? p.entitlement_minor : (p.transfer_minor ?? 0));
</script>

<svelte:head><title>Payouts — WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href="/app/settings">← Settings</a>
	<p class="eyebrow">Payouts</p>
	<h1 class="page-heading">Getting paid.</h1>
	<p class="page-intro">Buyers pay WantMyTime when they book. WantMyTime holds the money until the session is over, gives the buyer {hours(windowMinutes)} after it ends to report a problem, then sends your share to your bank about {hours(delayMinutes)} after the end. If the buyer reports a problem, that booking’s payout waits until WantMyTime has looked at it.</p>
	{#if message}<p class="notice notice-info" aria-live="polite">{message}</p>{/if}
	{#if paused}<p class="notice notice-warning">Payouts are briefly paused by WantMyTime. Nothing is lost; they will be sent when the pause is lifted.</p>{/if}

	{#if loaded}
		<section class="readiness-panel">
			<h2>Bank account</h2>
			{#if account && !editing}
				<p><strong>{account.bank_name} ••{account.account_last4}</strong><br />{account.account_name}</p>
				{#if account.in_safety_hold}<p class="form-note">New account: payouts go here from {when(account.usable_from)}. Until then they wait, so no money goes to an account you didn’t add.</p>{/if}
				<button class="button button-secondary" onclick={startEditing}>Change bank account</button>
			{:else}
				<form class="setup-form" onsubmit={(e) => { e.preventDefault(); save(); }}>
					<label>Bank
						<select class="field" bind:value={bankCode} onchange={check} required>
							<option value="" disabled>Choose your bank</option>
							{#each banks as b (b.code)}<option value={b.code}>{b.name}</option>{/each}
						</select>
					</label>
					<label>Account number
						<input class="field" inputmode="numeric" autocomplete="off" maxlength="13" bind:value={accountNumber} onblur={check} oninput={() => { if (`${bankCode}:${digits}` !== checkedKey) resolvedName = ''; if (digits.length === 10) check(); }} placeholder="10-digit NUBAN" required />
					</label>
					{#if checking}<p class="form-note" aria-live="polite">Checking the account…</p>{/if}
					{#if resolvedName}<p class="notice notice-info" aria-live="polite">Account name: <strong>{resolvedName}</strong>. Make sure this is you or your business.</p>{/if}
					{#if formMessage}<p class="notice notice-warning" aria-live="polite">{formMessage}</p>{/if}
					<div class="ops-nav">
						<button class="button" type="submit" disabled={saving || !resolvedName}>{account ? 'Use this account' : 'Save bank account'}</button>
						{#if account}<button class="button button-secondary" type="button" onclick={() => { editing = false; formMessage = ''; }}>Keep current account</button>{/if}
					</div>
					{#if account}<p class="form-note">For your safety, payouts go to a new account from 24 hours after you change it.</p>{/if}
				</form>
			{/if}
		</section>

		<section class="readiness-panel">
			<h2>Your payouts</h2>
			<div class="appointment-slip"><div><small>ON THE WAY</small><strong>{formatNaira(upcoming)}</strong></div><div><small>PAID</small><strong>{formatNaira(paid)}</strong></div></div>
			{#if payouts.length === 0}
				<p>No payouts yet. They appear here once a buyer pays for a booking.</p>
			{:else}
				<div class="list-stack">
					{#each payouts as p (p.id)}
						{@const s = status(p)}
						<article class="list-card">
							<div>
								<strong>{formatNaira(shown(p))} · {p.buyer_name}</strong>
								<p>{s.detail}</p>
								{#if p.recovery_minor > 0}<small>{formatNaira(p.recovery_minor)} kept to repay an earlier refund.</small>{/if}
								<small><a href={`/app/bookings/${encodeURIComponent(p.booking_id)}`}>View booking</a></small>
							</div>
							<span class={s.tone}>{s.label.toUpperCase()}</span>
						</article>
					{/each}
				</div>
			{/if}
		</section>
	{:else}
		<p class="page-intro">Loading…</p>
	{/if}
</section>
