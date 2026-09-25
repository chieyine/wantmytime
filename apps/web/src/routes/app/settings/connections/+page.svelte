<script lang="ts">
	import { untrack } from 'svelte';
	import { page } from '$app/state';
	import { api } from '$lib/api';

	import type { Calendar } from './+page';
	let { data } = $props();

	const outcomes: Record<string, { tone: 'info' | 'warning'; text: string }> = {
		connected: {
			tone: 'info',
			text: 'Google Calendar is connected. Your busy times now block bookings, and new bookings will appear on your calendar.'
		},
		cancelled: { tone: 'warning', text: 'Google Calendar was not connected. You can try again whenever you like.' },
		permissions: {
			tone: 'warning',
			text: 'WantMyTime needs both permissions: seeing when you are busy, and adding booking events. Try again and leave both boxes ticked.'
		},
		expired: { tone: 'warning', text: 'That connection attempt expired. Start again from this page.' },
		failed: { tone: 'warning', text: 'Google Calendar could not be connected. Try again in a moment.' }
	};

	let calendar = $state<Calendar | null>(null);
	let message = $state(untrack(() => data.loadError));
	let busy = $state(false);
	let outcome = $derived(outcomes[page.url.searchParams.get('google') ?? '']);
	let checkBusy = $state(true);
	let addEvents = $state(true);
	let meetLinks = $state(true);

	function apply(c: Calendar) {
		calendar = c;
		checkBusy = c.check_busy ?? true;
		addEvents = c.add_events ?? true;
		meetLinks = c.create_meet_links ?? true;
	}

	untrack(() => {
		if (data.calendar) apply(data.calendar);
	});

	async function load() {
		try {
			apply(await api<Calendar>('/api/v1/me/calendar'));
		} catch (e) {
			message = e instanceof Error ? e.message : 'Calendar status could not be loaded.';
		}
	}

	async function connect() {
		busy = true;
		message = '';
		try {
			const r = await api<{ authorization_url: string }>('/api/v1/me/calendar/google', { method: 'POST', body: '{}' });
			window.location.assign(r.authorization_url);
		} catch (e) {
			message = e instanceof Error ? e.message : 'The connection could not be started.';
			busy = false;
		}
	}

	async function save() {
		busy = true;
		message = '';
		try {
			apply(
				await api<Calendar>('/api/v1/me/calendar', {
					method: 'PATCH',
					body: JSON.stringify({
						check_busy: checkBusy,
						add_events: addEvents || meetLinks,
						create_meet_links: meetLinks
					})
				})
			);
			message = 'Calendar settings saved.';
		} catch (e) {
			message = e instanceof Error ? e.message : 'Calendar settings could not be saved.';
		} finally {
			busy = false;
		}
	}

	async function disconnect() {
		busy = true;
		message = '';
		try {
			await api('/api/v1/me/calendar', { method: 'DELETE' });
			await load();
			message =
				'Google Calendar is disconnected and WantMyTime’s access has been removed. Events already on your calendar stay there.';
		} catch (e) {
			message = e instanceof Error ? e.message : 'Google Calendar could not be disconnected.';
		} finally {
			busy = false;
		}
	}

	const synced = (value?: string | null) =>
		value
			? new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
			: 'not yet';
</script>

<svelte:head><title>Calendar and meetings — WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href="/app/settings">← Settings</a>
	<p class="eyebrow">Calendar and meetings</p>
	<h1 class="page-heading">Keep your calendar in step.</h1>
	<p class="page-intro">
		Connect Google Calendar so times you’re already busy can’t be booked, each booking lands on your calendar, and every
		booking gets its own Google Meet link automatically.
	</p>

	{#if outcome}<div class={`notice notice-${outcome.tone}`} role="status">{outcome.text}</div>{/if}

	{#if !calendar}
		<p class="page-intro">{message || 'Loading…'}</p>
	{:else if !calendar.configured}
		<div class="notice notice-info">
			Google Calendar connections aren’t switched on for WantMyTime yet. Until then, add a meeting link on each
			booking’s page; calendar files are available on every booking.
		</div>
	{:else if !calendar.connected}
		<div class="setup-form">
			<p class="form-note">
				WantMyTime asks Google for two things only: when you’re busy (not what your events are), and permission to add
				and update events for your bookings. It never sees your other events’ details.
			</p>
			<button class="button" onclick={connect} disabled={busy}
				>{busy ? 'Opening Google…' : 'Connect Google Calendar'} <span>↗</span></button
			>
		</div>
	{:else}
		<div class="appointment-slip">
			<div><small>GOOGLE ACCOUNT</small><strong>{calendar.account_email}</strong></div>
			<div>
				<small>STATUS</small><strong
					>{calendar.status === 'active'
						? 'Connected'
						: calendar.status === 'revoked'
							? 'Access removed'
							: 'Having trouble'}</strong
				>
			</div>
			<div><small>BUSY TIMES LAST CHECKED</small><strong>{synced(calendar.last_synced_at)}</strong></div>
			<div><small>CALENDAR</small><strong>Primary</strong></div>
		</div>
		{#if calendar.status !== 'active' && calendar.last_error}
			<div class="notice notice-warning" role="alert">
				{calendar.last_error}{#if calendar.status === 'revoked'}
					<button class="link-button" onclick={connect} disabled={busy}>Reconnect</button>{/if}
			</div>
		{/if}
		<form
			class="setup-form"
			onsubmit={(e) => {
				e.preventDefault();
				save();
			}}
		>
			<label class="check-line"
				><input type="checkbox" bind:checked={checkBusy} /> Don’t offer times when my Google Calendar is busy</label
			>
			<label class="check-line"
				><input type="checkbox" bind:checked={meetLinks} /> Create a Google Meet link for each booking</label
			>
			<label class="check-line"
				><input type="checkbox" bind:checked={addEvents} disabled={meetLinks} /> Add each booking to my calendar{meetLinks
					? ' (needed for Meet links)'
					: ''}</label
			>
			<p class="form-note">
				Buyers aren’t added as guests, so neither of you sees the other’s email address. The buyer gets the Meet link
				from WantMyTime, and you let them in from the call. A link you add yourself on a booking always takes priority.
			</p>
			<div class="ops-nav">
				<button class="button" type="submit" disabled={busy}>Save settings</button>
				<button class="button button-secondary" type="button" onclick={disconnect} disabled={busy}>Disconnect</button>
			</div>
		</form>
	{/if}
	{#if message && calendar}<p class="notice notice-info" aria-live="polite">{message}</p>{/if}
</section>
