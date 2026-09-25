<script lang="ts">
	import { onMount } from 'svelte';
	import { pushState, enablePush, disablePush, type PushState } from '$lib/push';

	// seller: settings wording. buyer: a reminder for the call.
	let { audience = 'seller', hideWhenOn = false } = $props<{ audience?: 'seller' | 'buyer'; hideWhenOn?: boolean }>();
	let status = $state<PushState | null>(null);
	let busy = $state(false);
	let error = $state('');

	onMount(async () => {
		try {
			status = await pushState();
		} catch {
			status = 'unsupported';
		}
	});

	async function run(fn: () => Promise<PushState>) {
		busy = true;
		error = '';
		try {
			status = await fn();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Notifications could not be changed.';
		} finally {
			busy = false;
		}
	}

	const what = $derived(audience === 'seller' ? 'new bookings, problem reports and calls about to start' : 'a reminder 10 minutes before your call');
</script>

{#if status && status !== 'unsupported' && status !== 'unavailable' && !(hideWhenOn && status === 'on')}
	<div class="push-toggle">
		{#if status === 'on'}
			<p><strong>Notifications are on for this device.</strong> You’ll get {what}.</p>
			<button class="text-link" onclick={() => run(disablePush)} disabled={busy}>Turn off on this device</button>
		{:else if status === 'off'}
			<p>{audience === 'seller' ? 'Get a notification on this phone or computer for' : 'Want'} {what}{audience === 'seller' ? '.' : '?'} Emails still come as usual.</p>
			<button class="button button-secondary" onclick={() => run(enablePush)} disabled={busy}>{busy ? 'Turning on…' : audience === 'seller' ? 'Turn on notifications' : 'Remind me'}</button>
		{:else if status === 'blocked'}
			<p>Notifications are blocked for WantMyTime in this browser. To get {what}, allow notifications for this site in your browser settings, then come back.</p>
		{:else if status === 'install-first'}
			<p>On iPhone, notifications work once WantMyTime is on your home screen: tap the Share button, choose <strong>Add to Home Screen</strong>, open WantMyTime from there and switch this on.</p>
		{/if}
		{#if error}<p class="notice notice-warning" role="alert">{error}</p>{/if}
	</div>
{/if}
