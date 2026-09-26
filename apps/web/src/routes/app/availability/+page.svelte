<script lang="ts">
	import { untrack } from 'svelte';
	import { api } from '$lib/api';
	import { timezoneOptions } from '$lib/timezones';

	import type { Availability, WindowRule } from './+page';
	let { data } = $props();
	const days = [
		{ value: 1, short: 'MON', name: 'Monday' },
		{ value: 2, short: 'TUE', name: 'Tuesday' },
		{ value: 3, short: 'WED', name: 'Wednesday' },
		{ value: 4, short: 'THU', name: 'Thursday' },
		{ value: 5, short: 'FRI', name: 'Friday' },
		{ value: 6, short: 'SAT', name: 'Saturday' },
		{ value: 0, short: 'SUN', name: 'Sunday' }
	];
	// The form starts from what the route loaded, then belongs to the page while the seller edits.
	function formFrom(saved: Availability | null) {
		// Times are exchanged as HH:MM; trim seconds defensively.
		const savedWindows = (saved?.windows ?? []).map((window) => ({
			...window,
			start: window.start.slice(0, 5),
			end: window.end.slice(0, 5)
		}));
		const fingerprint = saved
			? JSON.stringify({
					windows: savedWindows,
					timezone: saved.timezone,
					notice: saved.minimum_notice_minutes,
					horizon: saved.booking_horizon_days,
					buffer: saved.buffer_minutes
				})
			: '';
		// First visit: start from weekdays 9 to 5 so there is something to save, not seven empty days.
		const starter = !!saved && savedWindows.length === 0;
		return {
			windows: starter ? [1, 2, 3, 4, 5].map((weekday) => ({ weekday, start: '09:00', end: '17:00' })) : savedWindows,
			starter,
			overrides: saved?.overrides ?? [],
			timezone: saved?.timezone ?? 'UTC',
			notice: saved?.minimum_notice_minutes ?? 60,
			horizon: saved?.booking_horizon_days ?? 30,
			buffer: saved?.buffer_minutes ?? 0,
			fingerprint
		};
	}
	const initial = untrack(() => formFrom(data.availability));
	let windows = $state<WindowRule[]>(initial.windows);
	let starterHours = $state(initial.starter);
	let overrides = $state(initial.overrides);
	// Hours a day had before it was switched off, so switching it back on restores them.
	let rememberedHours: Record<number, WindowRule[]> = {};
	let overrideDate = $state('');
	let timezone = $state(initial.timezone);
	let notice = $state(initial.notice);
	let horizon = $state(initial.horizon);
	let buffer = $state(initial.buffer);
	const loaded = untrack(() => data.availability !== null);
	let saving = $state(false);
	let overrideBusy = $state(false);
	let message = $state(untrack(() => data.loadError));
	let overrideMessage = $state('');
	let savedFingerprint = $state(initial.fingerprint);
	let saveState = $state<'idle' | 'saved' | 'error'>('idle');
	let openDays = $derived(days.filter((day) => windows.some((window) => window.weekday === day.value)));
	let invalidWindow = $derived(windows.some((window) => !window.start || !window.end || window.start >= window.end));
	let fingerprint = $derived(
		JSON.stringify({ windows, timezone, notice: Number(notice), horizon: Number(horizon), buffer: Number(buffer) })
	);
	let dirty = $derived(loaded && fingerprint !== savedFingerprint);

	const asMinutes = (time: string) => {
		const [hour, minute] = time.split(':').map(Number);
		return hour * 60 + minute;
	};
	const asTime = (minutes: number) =>
		`${String(Math.floor(minutes / 60)).padStart(2, '0')}:${String(minutes % 60).padStart(2, '0')}`;
	const sorted = (list: WindowRule[]) => list.sort((a, b) => a.weekday - b.weekday || a.start.localeCompare(b.start));
	function hoursFor(weekday: number) {
		return windows.filter((window) => window.weekday === weekday);
	}
	// Hours to offer when a day is switched on: what it had before, else the first open day's.
	function defaultHours(weekday: number): WindowRule[] {
		if (rememberedHours[weekday]?.length) return rememberedHours[weekday];
		const model = openDays.length ? hoursFor(openDays[0].value) : [{ weekday, start: '09:00', end: '17:00' }];
		return model.map((window) => ({ weekday, start: window.start, end: window.end }));
	}
	function toggleDay(weekday: number, open: boolean) {
		if (open) {
			windows = sorted([...windows, ...defaultHours(weekday)]);
		} else {
			rememberedHours[weekday] = hoursFor(weekday).map((window) => ({ ...window }));
			windows = windows.filter((window) => window.weekday !== weekday);
		}
		saveState = 'idle';
	}
	function addWindow(weekday: number) {
		const latestEnd = Math.max(0, ...hoursFor(weekday).map((window) => asMinutes(window.end)));
		if (latestEnd >= 23 * 60) return;
		const startMinutes = latestEnd ? latestEnd + 15 : 9 * 60;
		const endMinutes = Math.min(startMinutes + (latestEnd ? 60 : 480), 23 * 60 + 45);
		windows = sorted([...windows, { weekday, start: asTime(startMinutes), end: asTime(endMinutes) }]);
		saveState = 'idle';
	}
	// Gives every open day the same hours as the given day.
	function copyToAll(weekday: number) {
		const model = hoursFor(weekday);
		windows = sorted(
			openDays.flatMap((day) => model.map((window) => ({ weekday: day.value, start: window.start, end: window.end })))
		);
		saveState = 'idle';
	}
	function withCurrent(options: [number, string][], current: number, unit: string) {
		return options.some(([value]) => value === Number(current))
			? options
			: [...options, [Number(current), `${current} ${unit}`] as [number, string]].sort((a, b) => a[0] - b[0]);
	}
	const noticeOptions: [number, string][] = [
		[0, 'No notice needed'],
		[30, '30 minutes before'],
		[60, '1 hour before'],
		[120, '2 hours before'],
		[240, '4 hours before'],
		[720, '12 hours before'],
		[1440, '1 day before'],
		[2880, '2 days before'],
		[10080, '1 week before']
	];
	const horizonOptions: [number, string][] = [
		[7, 'Up to 1 week ahead'],
		[14, 'Up to 2 weeks ahead'],
		[30, 'Up to 30 days ahead'],
		[60, 'Up to 60 days ahead'],
		[90, 'Up to 90 days ahead'],
		[180, 'Up to 6 months ahead'],
		[365, 'Up to a year ahead']
	];
	const bufferOptions: [number, string][] = [
		[0, 'No break'],
		[5, '5 minutes'],
		[10, '10 minutes'],
		[15, '15 minutes'],
		[30, '30 minutes'],
		[60, '1 hour']
	];
	function prettyDate(date: string) {
		const parsed = new Date(`${date}T12:00:00`);
		return Number.isNaN(parsed.getTime())
			? date
			: parsed.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short', year: 'numeric' });
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
		const payload = {
			windows: windows.map((window) => ({ ...window })),
			timezone,
			minimum_notice_minutes: Number(notice),
			booking_horizon_days: Number(horizon),
			buffer_minutes: Number(buffer)
		};
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
			await api(`/api/v1/me/availability/overrides/${overrideDate}`, {
				method: 'PUT',
				body: JSON.stringify({ closed: true })
			});
			overrides = [
				...overrides.filter((item) => item.date !== overrideDate),
				{ date: overrideDate, closed: true }
			].sort((a, b) => a.date.localeCompare(b.date));
			overrideMessage = `${prettyDate(overrideDate)} is now a day off.`;
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
			overrideMessage = `${prettyDate(date)} is back to your usual hours.`;
		} catch (error) {
			overrideMessage = error instanceof Error ? error.message : 'Date override could not be removed.';
		} finally {
			overrideBusy = false;
		}
	}
</script>

<svelte:head><title>Availability — WantMyTime</title></svelte:head>
<section class="form-page app-page workspace-availability">
	<a class="back-link" href="/app">← Home</a>
	<p class="eyebrow">Availability</p>
	<h1 class="page-heading">YOUR HOURS.</h1>
	<p class="page-intro">Tick the days you work and set the hours people can book.</p>
	{#if starterHours}<div class="notice notice-info">
			We’ve filled in weekdays, 9am to 5pm, to get you started. Change anything you like, then save.
		</div>{/if}
	{#if !loaded}<div class="notice notice-warning" role="alert">{message}</div>
		<button class="button button-secondary" onclick={() => window.location.reload()}>Try loading again</button>
	{:else}
		<form
			onsubmit={(event) => {
				event.preventDefault();
				save();
			}}
		>
			<div class="hours-list" role="group" aria-label="Weekly hours">
				{#each days as day (day.value)}
					{@const dayHours = hoursFor(day.value)}
					<div class="hours-day" class:closed={!dayHours.length}>
						<label class="hours-toggle"
							><input
								type="checkbox"
								checked={dayHours.length > 0}
								onchange={(event) => toggleDay(day.value, event.currentTarget.checked)}
							/><span>{day.name}</span></label
						>
						{#if dayHours.length}
							<div class="hours-times">
								{#each windows as window, index (index)}{#if window.weekday === day.value}<div class="hours-window">
											<input
												class="field"
												type="time"
												step="900"
												bind:value={window.start}
												required
												aria-label={`${day.name} from`}
											/><span aria-hidden="true">–</span><input
												class="field"
												type="time"
												step="900"
												bind:value={window.end}
												required
												aria-label={`${day.name} until`}
											/><button
												type="button"
												class="hours-icon"
												onclick={() => removeWindow(index)}
												aria-label={`Remove ${day.name} hours ${window.start} to ${window.end}`}>×</button
											>
										</div>{/if}{/each}
								<div class="hours-actions">
									<button
										type="button"
										onclick={() => addWindow(day.value)}
										disabled={dayHours.some((window) => window.end >= '23:00')}>+ Add hours</button
									>{#if openDays.length > 1 && openDays[0].value === day.value}<button
											type="button"
											onclick={() => copyToAll(day.value)}>Copy to every open day</button
										>{/if}
								</div>
							</div>
						{:else}<p class="hours-closed">Not available</p>{/if}
					</div>
				{/each}
			</div>
			<label class="hours-timezone"
				><span>Times are in</span><select class="field" bind:value={timezone}
					>{#each timezoneOptions(timezone) as zone (zone)}<option value={zone}>{zone}</option>{/each}</select
				></label
			>
			<p class="hours-note">People booking from elsewhere see these times in their own timezone.</p>
			<details class="hours-more">
				<summary>More options <small>notice, how far ahead, breaks between bookings</small></summary>
				<div class="hours-limits">
					<label
						>How soon can people book?<select class="field" bind:value={notice}
							>{#each withCurrent(noticeOptions, notice, 'minutes before') as [value, label] (value)}<option {value}
									>{label}</option
								>{/each}</select
						></label
					><label
						>How far ahead?<select class="field" bind:value={horizon}
							>{#each withCurrent(horizonOptions, horizon, 'days ahead') as [value, label] (value)}<option {value}
									>{label}</option
								>{/each}</select
						></label
					><label
						>Break after each booking<select class="field" bind:value={buffer}
							>{#each withCurrent(bufferOptions, buffer, 'minutes') as [value, label] (value)}<option {value}
									>{label}</option
								>{/each}</select
						></label
					>
				</div>
			</details>
			<div class="availability-save-bar">
				<div>
					<span class:dirty class:saved={saveState === 'saved' && !dirty} class="availability-save-dot"></span><strong
						>{saving
							? 'SAVING CHANGES'
							: dirty
								? 'UNSAVED CHANGES'
								: saveState === 'saved'
									? 'HOURS SAVED'
									: 'ALL CHANGES SAVED'}</strong
					><small>Bookings you already have stay in place.</small>
				</div>
				<button class="button" type="submit" disabled={saving || !dirty || invalidWindow}
					>{saving ? 'SAVING…' : 'SAVE HOURS'} <span aria-hidden="true">↗</span></button
				>
			</div>
			{#if invalidWindow}<p class="notice notice-warning" role="alert">
					Each set of hours needs an end time after its start time.
				</p>{/if}
			{#if message && (!dirty || saveState === 'error')}<p
					class:notice-warning={saveState === 'error'}
					class:notice-info={saveState !== 'error'}
					class="notice"
					aria-live="polite"
				>
					{message}
				</p>{/if}
		</form>
		<section class="availability-overrides">
			<div>
				<h2>Days off</h2>
				<p>Away on a particular date? Close it here. Your weekly hours stay as they are.</p>
			</div>
			<div>
				<label>Date<input class="field" type="date" bind:value={overrideDate} /></label><button
					class="button button-secondary"
					type="button"
					onclick={closeDate}
					disabled={!overrideDate || overrideBusy}>TAKE THE DAY OFF</button
				>{#if overrideMessage}<p class="notice notice-info" aria-live="polite">
						{overrideMessage}
					</p>{/if}{#each overrides as item (item.date)}<div class="availability-override-row">
						<span>{prettyDate(item.date)}</span><strong>DAY OFF</strong><button
							type="button"
							onclick={() => removeOverride(item.date)}
							disabled={overrideBusy}>Undo</button
						>
					</div>{/each}
			</div>
		</section>
	{/if}
</section>
