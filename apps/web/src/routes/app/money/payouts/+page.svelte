<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { api } from '$lib/api';

	import type { Account, Market, Operator } from './+page';
	type Bank = { code: string; name: string };

	let { data } = $props();
	// Starts from what the route loaded; saving or removing an account updates it in place.
	let account = $state<Account | null>(untrack(() => data.payoutAccount?.account ?? null));
	let market = $state<Market | null>(untrack(() => data.payoutAccount?.market ?? null));
	let delayMinutes = $state(untrack(() => data.payoutAccount?.payout_delay_minutes ?? 180));
	let windowMinutes = $state(untrack(() => data.payoutAccount?.dispute_window_minutes ?? 120));
	let paused = $state(untrack(() => data.paused));
	let message = $state(untrack(() => data.loadError));

	let editing = $state(false);
	let banks = $state<Bank[]>([]);
	let operators = $state<Operator[]>([]);
	let kind = $state<'bank_account' | 'mobile_money'>('bank_account');
	let bankCode = $state('');
	let accountNumber = $state('');
	let typedName = $state('');
	let resolvedName = $state('');
	let checking = $state(false);
	let saving = $state(false);
	let formMessage = $state('');

	const hours = (m: number) => (m % 60 === 0 ? `${m / 60} hour${m === 60 ? '' : 's'}` : `${m} minutes`);
	const when = (v: string) =>
		new Date(v).toLocaleString(undefined, {
			weekday: 'short',
			day: 'numeric',
			month: 'short',
			hour: 'numeric',
			minute: '2-digit'
		});
	const digits = $derived(accountNumber.replace(/[\s-]/g, ''));
	const exactDigits = $derived(market?.account_digits ?? 0);
	const nameChecked = $derived(kind === 'bank_account' && !!market?.name_check);
	const numberReady = $derived(
		kind === 'mobile_money' ? digits.length >= 9 : exactDigits > 0 ? digits.length === exactDigits : digits.length >= 6
	);
	const canSave = $derived(!!bankCode && numberReady && (nameChecked ? !!resolvedName : typedName.trim().length >= 2));

	// With no account yet, open the form straight away.
	onMount(() => {
		if (!account && !message) void startEditing();
	});

	async function startEditing() {
		editing = true;
		formMessage = '';
		if (banks.length === 0 && operators.length === 0) {
			try {
				const list = await api<{ banks: Bank[]; mobile_money: Operator[] | null }>('/api/v1/payout-banks');
				banks = list.banks.sort((a, b) => a.name.localeCompare(b.name));
				operators = list.mobile_money ?? [];
				if (banks.length === 0 && operators.length) kind = 'mobile_money';
			} catch (e) {
				formMessage = e instanceof Error ? e.message : 'The list of banks could not be loaded.';
			}
		}
	}

	function switchKind(next: 'bank_account' | 'mobile_money') {
		kind = next;
		bankCode = accountNumber = resolvedName = typedName = '';
		checkedKey = '';
		formMessage = '';
	}

	let checkedKey = '';
	async function check() {
		if (!nameChecked) return;
		const key = `${bankCode}:${digits}`;
		if (key === checkedKey && resolvedName) return; // already confirmed; blur on "Save" must not undo it
		resolvedName = '';
		formMessage = '';
		checkedKey = key;
		if (!bankCode || !numberReady) return;
		checking = true;
		try {
			const name = (
				await api<{ account_name: string }>('/api/v1/me/payout-account/resolve', {
					method: 'POST',
					body: JSON.stringify({ type: kind, bank_code: bankCode, account_number: digits })
				})
			).account_name;
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
			account = (
				await api<{ account: Account }>('/api/v1/me/payout-account', {
					method: 'PUT',
					body: JSON.stringify({
						type: kind,
						bank_code: bankCode,
						account_number: digits,
						account_name: nameChecked ? '' : typedName.trim()
					})
				})
			).account;
			editing = false;
			accountNumber = bankCode = resolvedName = typedName = '';
			message = replacing
				? 'Payout account changed. For your safety, payouts go to the new account from 24 hours from now. Any payout that failed is sent again then.'
				: 'Payout account saved. You’re open for bookings, and payouts will be sent here.';
		} catch (e) {
			formMessage = e instanceof Error ? e.message : 'The account could not be saved.';
		} finally {
			saving = false;
		}
	}
</script>

<svelte:head><title>Payouts — WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href="/app/money">← Money</a>
	<p class="eyebrow">Payouts</p>
	<h1 class="page-heading">Where your money goes.</h1>
	<p class="page-intro">
		People pay when they book. We hold the money until the call is over and the buyer’s {hours(windowMinutes)} to report a
		problem have passed, then send your share here, about {hours(delayMinutes)} after the call{market
			? `, in ${market.currency}`
			: ''}. If they report a problem, that payout waits until we’ve looked at it.
	</p>
	{#if message}<p class="notice notice-info" aria-live="polite">{message}</p>{/if}
	{#if paused}<p class="notice notice-warning">
			Payouts are briefly paused by WantMyTime. Nothing is lost; they will be sent when the pause is lifted.
		</p>{/if}

	<section class="readiness-panel">
		<h2>Payout account{market ? ` · ${market.name}` : ''}</h2>
		{#if account && !editing}
			<p><strong>{account.bank_name} ••{account.account_last4}</strong><br />{account.account_name}</p>
			{#if account.in_safety_hold}<p class="form-note">
					New account: payouts go here from {when(account.usable_from)}. Until then they wait, so no money goes to an
					account you didn’t add.
				</p>{/if}
			<button class="button button-secondary" onclick={startEditing}>Change payout account</button>
		{:else}
			<form
				class="setup-form"
				onsubmit={(e) => {
					e.preventDefault();
					save();
				}}
			>
				{#if banks.length && operators.length}
					<fieldset>
						<legend>Get paid into</legend>
						<div class="choice-row">
							<label class:chosen={kind === 'bank_account'}
								><input type="radio" checked={kind === 'bank_account'} onchange={() => switchKind('bank_account')} /> A bank
								account</label
							><label class:chosen={kind === 'mobile_money'}
								><input type="radio" checked={kind === 'mobile_money'} onchange={() => switchKind('mobile_money')} /> Mobile
								money</label
							>
						</div>
					</fieldset>
				{/if}
				{#if kind === 'bank_account'}
					<label
						>Bank
						<select class="field" bind:value={bankCode} onchange={check} required>
							<option value="" disabled>Choose your bank</option>
							{#each banks as b (b.code)}<option value={b.code}>{b.name}</option>{/each}
						</select>
					</label>
					<label
						>Account number
						<input
							class="field"
							inputmode="numeric"
							autocomplete="off"
							maxlength="24"
							bind:value={accountNumber}
							onblur={check}
							oninput={() => {
								if (`${bankCode}:${digits}` !== checkedKey) resolvedName = '';
								if (nameChecked && numberReady) check();
							}}
							placeholder={exactDigits ? `${exactDigits}-digit account number` : 'Account number'}
							required
						/>
					</label>
				{:else}
					<label
						>Network
						<select class="field" bind:value={bankCode} required>
							<option value="" disabled>Choose your network</option>
							{#each operators as op (op.code)}<option value={op.code}>{op.name}</option>{/each}
						</select>
					</label>
					<label
						>Wallet phone number
						<input
							class="field"
							inputmode="tel"
							autocomplete="tel"
							maxlength="20"
							bind:value={accountNumber}
							placeholder={market?.phone_prefix ? `+${market.phone_prefix} …` : 'Phone number'}
							required
						/>
					</label>
				{/if}
				{#if nameChecked}
					{#if checking}<p class="form-note" aria-live="polite">Checking the account…</p>{/if}
					{#if resolvedName}<p class="notice notice-info" aria-live="polite">
							Account name: <strong>{resolvedName}</strong>. Make sure this is you or your business.
						</p>{/if}
				{:else}
					<label
						>Name on the {kind === 'mobile_money' ? 'wallet' : 'account'}<input
							class="field"
							bind:value={typedName}
							maxlength="80"
							autocomplete="name"
							required
						/><span class="form-note"
							>Exactly as your {kind === 'mobile_money' ? 'network' : 'bank'} shows it, so the payout isn’t refused.</span
						></label
					>
				{/if}
				{#if formMessage}<p class="notice notice-warning" aria-live="polite">{formMessage}</p>{/if}
				<div class="ops-nav">
					<button class="button" type="submit" disabled={saving || !canSave}
						>{account ? 'Use this account' : 'Save payout account'}</button
					>
					{#if account}<button
							class="button button-secondary"
							type="button"
							onclick={() => {
								editing = false;
								formMessage = '';
							}}>Keep current account</button
						>{/if}
				</div>
				{#if account}<p class="form-note">
						For your safety, payouts go to a new account from 24 hours after you change it.
					</p>{/if}
			</form>
		{/if}
	</section>

	<p class="form-note">
		Your payouts, paid and on the way, are under <a class="text-link" href="/app/money">Money</a>.
	</p>
</section>
