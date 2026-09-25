<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { timezoneOptions } from '$lib/timezones';

	type WindowRule = { weekday: number; start: string; end: string };
	type Override = { date: string; closed: boolean };
	const days = [
		{ value: 1, short: 'MON', name: 'Monday' },
		{ value: 2, short: 'TUE', name: 'Tuesday' },
		{ value: 3, short: 'WED', name: 'Wednesday' },
		{ value: 4, short: 'THU', name: 'Thursday' },
		{ value: 5, short: 'FRI', name: 'Friday' },
		{ value: 6, short: 'SAT', name: 'Saturday' },
		{ value: 0, short: 'SUN', name: 'Sunday' }
	];
	let windows = $state<WindowRule[]>([]);
	let starterHours = $state(false);
	let overrides = $state<Override[]>([]);
	let selectedDay = $state(1);
	let overrideDate = $state('');
	let timezone = $state('Africa/Lagos');
	let notice = $state(60);
	let horizon = $state(30);
	let buffer = $state(0);
	let loading = $state(true);
	let loaded = $state(false);
	let saving = $state(false);
	let overrideBusy = $state(false);
	let message = $state('');
	let overrideMessage = $state('');
	let savedFingerprint = $state('');
	let saveState = $state<'idle' | 'saved' | 'error'>('idle');
	let selectedName = $derived(days.find((day) => day.value === selectedDay)?.name || 'Day');
	let selectedWindows = $derived(windows.filter((window) => window.weekday === selectedDay));
	let invalidWindow = $derived(windows.some((window) => !window.start || !window.end || window.start >= window.end));
	let fingerprint = $derived(JSON.stringify({ windows, timezone, notice: Number(notice), horizon: Number(horizon), buffer: Number(buffer) }));
	let dirty = $derived(loaded && fingerprint !== savedFingerprint);

	function rangeStyle(window: WindowRule) {
		const [startHour, startMinute] = window.start.split(':').map(Number);
		const [endHour, endMinute] = window.end.split(':').map(Number);
		const start = Math.max(0, Math.min(1440, startHour * 60 + startMinute));
		const end = Math.max(start, Math.min(1440, endHour * 60 + endMinute));
		return `left:${(start / 1440) * 100}%;width:${((end - start) / 1440) * 100}%`;
	}

	onMount(async () => {
		try {
			const data = await api<{ windows: WindowRule[]; overrides: Override[]; timezone: string; minimum_notice_minutes: number; booking_horizon_days: number; buffer_minutes: number }>('/api/v1/me/availability');
			// Times are exchanged as HH:MM; trim seconds defensively.
			windows = data.windows.map((window) => ({ ...window, start: window.start.slice(0, 5), end: window.end.slice(0, 5) }));
			overrides = data.overrides;
			timezone = data.timezone;
			notice = data.minimum_notice_minutes;
			horizon = data.booking_horizon_days;
			buffer = data.buffer_minutes;
			savedFingerprint = JSON.stringify({ windows, timezone: data.timezone, notice: data.minimum_notice_minutes, horizon: data.booking_horizon_days, buffer: data.buffer_minutes });
			// First visit: start from weekdays 9 to 5 so there is something to save, not seven empty days.
			if (windows.length === 0) {
				windows = [1, 2, 3, 4, 5].map((weekday) => ({ weekday, start: '09:00', end: '17:00' }));
				starterHours = true;
			}
			loaded = true;
		} catch (error) {
			message = error instanceof Error ? error.message : 'Availability could not be loaded.';
		} finally {
			loading = false;
		}
	});

	function addWindow() {
		const latestEnd = Math.max(0, ...selectedWindows.map((window) => { const [hour, minute] = window.end.split(':').map(Number); return hour * 60 + minute; }));
		if (latestEnd >= 23 * 60) return;
		const startMinutes = latestEnd ? latestEnd + 15 : 10 * 60;
		const endMinutes = Math.min(startMinutes + (latestEnd ? 60 : 360), 23 * 60 + 45);
		const asTime = (minutes: number) => `${String(Math.floor(minutes / 60)).padStart(2, '0')}:${String(minutes % 60).padStart(2, '0')}`;
		const next = { weekday: selectedDay, start: asTime(startMinutes), end: asTime(endMinutes) };
		windows = [...windows, next].sort((a, b) => a.weekday - b.weekday || a.start.localeCompare(b.start));
		saveState = 'idle';
	}
	function removeWindow(index: number) {
		windows = windows.filter((_, position) => position !== index);
		saveState = 'idle';
	}
	async function save() {
		if (invalidWindow || !dirty) return;
		saving = true;
		message = '';
		const submittedFingerprint = fingerprint;
		const payload = { windows: windows.map((window) => ({ ...window })), timezone, minimum_notice_minutes: Number(notice), booking_horizon_days: Number(horizon), buffer_minutes: Number(buffer) };
		try {
			await api('/api/v1/me/availability', { method: 'PUT', body: JSON.stringify(payload) });
			savedFingerprint = submittedFingerprint;
			saveState = 'saved';
			starterHours = false;
			message = 'Your hours are saved.';
		} catch (error) {
			saveState = 'error';
			message = error instanceof Error ? error.message : 'Availability could not be saved.';
		} finally {
			saving = false;
		}
	}
	async function closeDate() {
		if (!overrideDate) return;
		overrideBusy = true;
		overrideMessage = '';
		try {
			await api(`/api/v1/me/availability/overrides/${overrideDate}`, { method: 'PUT', body: JSON.stringify({ closed: true }) });
			overrides = [...overrides.filter((item) => item.date !== overrideDate), { date: overrideDate, closed: true }].sort((a, b) => a.date.localeCompare(b.date));
			overrideMessage = `${overrideDate} is closed.`;
			overrideDate = '';
		} catch (error) {
			overrideMessage = error instanceof Error ? error.message : 'Date override could not be saved.';
		} finally {
			overrideBusy = false;
		}
	}
	async function removeOverride(date: string) {
		overrideBusy = true;
		overrideMessage = '';
		try {
			await api(`/api/v1/me/availability/overrides/${date}`, { method: 'DELETE' });
			overrides = overrides.filter((item) => item.date !== date);
			overrideMessage = `Weekly hours restored for ${date}.`;
		} catch (error) {
			overrideMessage = error instanceof Error ? error.message : 'Date override could not be removed.';
		} finally {
			overrideBusy = false;
		}
	}
</script>

<svelte:head><title>Availability — WantMyTime</title></svelte:head>
<section class="form-page app-page workspace-availability">
	<a class="back-link" href="/app">← Overview</a>
	<p class="eyebrow">Availability / your hours</p>
	<h1 class="page-heading">MAKE TIME<br />ON YOUR TERMS.</h1>
	<p class="page-intro">Set the hours people can book each week. Close a single date whenever you need to.</p>
	{#if starterHours}<div class="notice notice-info">We’ve filled in weekdays, 9am to 5pm, to get you started. Change anything you like, then save.</div>{/if}
	{#if loading}<p class="page-intro">Loading your saved hours…</p>
	{:else if !loaded}<div class="notice notice-warning" role="alert">{message}</div><button class="button button-secondary" onclick={() => window.location.reload()}>Try loading again</button>
	{:else}
		<form onsubmit={(event) => { event.preventDefault(); save(); }}>
			<div class="availability-editor-head"><div><p class="workspace-kicker">01 / WEEKLY RHYTHM</p><h2>Your week, at a glance.</h2></div><label>TIMEZONE<select class="field" bind:value={timezone}>{#each timezoneOptions(timezone) as zone}<option value={zone}>{zone}</option>{/each}</select></label></div>
			<div class="availability-week" role="group" aria-label="Choose a day to edit">
				{#each days as day}
					{@const dayWindows = windows.filter((window) => window.weekday === day.value)}
					<button type="button" class:active={selectedDay === day.value} aria-pressed={selectedDay === day.value} onclick={() => selectedDay = day.value}><span>{day.short}</span><strong>{dayWindows.length ? `${dayWindows.length} ${dayWindows.length === 1 ? 'window' : 'windows'}` : 'Closed'}</strong><span class="availability-week-track" aria-hidden="true">{#each dayWindows as window}<i style={rangeStyle(window)}></i>{/each}</span></button>
				{/each}
			</div>
			<div class="availability-day"><div class="availability-day-head"><div><p class="workspace-kicker">EDIT SELECTED DAY</p><h3>{selectedName}</h3></div><span>{selectedWindows.length ? `${selectedWindows.length} ${selectedWindows.length === 1 ? 'WINDOW' : 'WINDOWS'}` : 'NO HOURS'}</span></div>
				{#if !selectedWindows.length}<p class="availability-day-empty">This day is closed. Add hours to make it available.</p>{/if}
				{#each windows as window, index}{#if window.weekday === selectedDay}<div class="availability-window"><label>FROM<input class="field" type="time" step="900" bind:value={window.start} required /></label><span aria-hidden="true">→</span><label>UNTIL<input class="field" type="time" step="900" bind:value={window.end} required /></label><button type="button" onclick={() => removeWindow(index)} aria-label={`Remove ${selectedName} hours ${window.start} to ${window.end}`}>REMOVE</button></div>{/if}{/each}
				<button class="availability-add-window" type="button" onclick={addWindow} disabled={selectedWindows.some((window) => window.end >= '23:00')}>+ ADD HOURS TO {selectedName.toUpperCase()}</button>
			</div>
			<div class="availability-bottom-grid"><section><p class="workspace-kicker">02 / BOOKING LIMITS</p><h2>Leave room around it.</h2><div class="availability-limits"><label>MINIMUM NOTICE <span>MINUTES</span><input class="field" type="number" min="0" max="10080" bind:value={notice} required /></label><label>BOOKING HORIZON <span>DAYS</span><input class="field" type="number" min="1" max="365" bind:value={horizon} required /></label><label>BUFFER AFTER A MEETING <span>MINUTES</span><input class="field" type="number" min="0" max="240" bind:value={buffer} required /></label></div></section><aside><p class="workspace-kicker">YOUR LOCAL VIEW</p><p>All weekly hours use <strong>{timezone}</strong>. People booking your link see available times in that timezone.</p><div class="availability-day-track" aria-hidden="true"><span>00:00</span><span>12:00</span><span>24:00</span>{#each selectedWindows as window}<i style={rangeStyle(window)}></i>{/each}</div></aside></div>
			<div class="availability-save-bar"><div><span class:dirty class:saved={saveState === 'saved' && !dirty} class="availability-save-dot"></span><strong>{saving ? 'SAVING CHANGES' : dirty ? 'UNSAVED CHANGES' : saveState === 'saved' ? 'HOURS SAVED' : 'ALL CHANGES SAVED'}</strong><small>Existing confirmed bookings stay in place when you edit these hours.</small></div><button class="button" type="submit" disabled={saving || !dirty || invalidWindow}>{saving ? 'SAVING…' : 'SAVE WEEKLY HOURS'} <span aria-hidden="true">↗</span></button></div>
			{#if invalidWindow}<p class="notice notice-warning" role="alert">Each window needs an end time after its start time.</p>{/if}
			{#if message && (!dirty || saveState === 'error')}<p class:notice-warning={saveState === 'error'} class:notice-info={saveState !== 'error'} class="notice" aria-live="polite">{message}</p>{/if}
		</form>
		<section class="availability-overrides"><div><p class="workspace-kicker">03 / DATE EXCEPTIONS</p><h2>Close a particular day.</h2><p>Your weekly rhythm stays saved. Closing a date changes only that day.</p></div><div><label>DATE TO CLOSE<input class="field" type="date" bind:value={overrideDate} /></label><button class="button button-secondary" type="button" onclick={closeDate} disabled={!overrideDate || overrideBusy}>CLOSE DATE ↗</button>{#if overrideMessage}<p class="notice notice-info" aria-live="polite">{overrideMessage}</p>{/if}{#each overrides as item}<div class="availability-override-row"><span>{item.date}</span><strong>CLOSED</strong><button type="button" onclick={() => removeOverride(item.date)} disabled={overrideBusy}>RESTORE HOURS ↗</button></div>{/each}</div></section>
		<div class="notice notice-warning">People can book any free slot inside these hours, minus your notice and buffer times.</div>
	{/if}
</section>
