<script lang="ts">
	import { api } from '$lib/api';
	import { viewerTimeZone, zoneCity } from '$lib/time';
	import { formatNaira } from '$lib/money';

	let { bookingId, backHref = '/access/bookings', inWorkspace = false } = $props<{ bookingId: string; backHref?: string; inWorkspace?: boolean }>();

	type Booking = { id: string; seller: string; timezone: string; seller_name: string; buyer: string; duration_minutes: number; starts_at: string; meeting_deadline: string; amount_minor: string; state: string; payment_state: string; meeting_url: string; role: 'seller' | 'buyer'; issue_reason: string | null; buyer_completed: boolean; seller_completed: boolean; cancellation_open?: boolean };
	type Policy = { key: string; name: string; summary: string };
	type Lifecycle = {
		cancellation_policy: Policy;
		cancelled_at: string | null;
		cancelled_by_role: string;
		refund?: { amount_minor: number; state: string };
		no_show?: { absent_role: string; state: string; resolves_at: string; reported_by_me: boolean; about_me: boolean };
		review?: { id: string; rating: number; body: string; seller_reply: string | null };
		can_review: boolean;
		can_report_no_show: boolean;
		can_report_problem: boolean;
		problem_deadline: string;
		problem: { open: boolean; reported_by: string; resolution: string };
		payout?: { state: string; entitlement_minor: number; transfer_minor?: number; recovery_minor: number; release_at: string; paid_at: string | null; bank_name: string; account_last4: string; hold: string; hold_text?: string; note?: string };
	};
	type Preview = { can_cancel: boolean; reason?: string; role: string; refund_minor: number; refund_percent: number; policy_name: string; policy_summary: string; refund_drops_at?: string; paid: boolean };
	type Slot = { starts_at: string; local_label: string; timezone: string };
	type Reschedule = { id: string; requested_by_me: boolean; proposed_starts_at: string; state: string; expires_at: string };

	let booking = $state<Booking | null>(null);
	let life = $state<Lifecycle | null>(null);
	let reschedules = $state<Reschedule[]>([]);
	let loading = $state(true);
	let busy = $state(false);
	let message = $state('');
	let meeting = $state('');
	let issue = $state('');
	let exceptionReason = $state('');
	let exceptionOpen = $state(false);
	let date = $state('');
	let slots = $state<Slot[]>([]);
	let selected = $state('');
	let preview = $state<Preview | null>(null);
	let cancelReason = $state('');
	let disputeReason = $state('');
	let rating = $state(0);
	let reviewBody = $state('');
	let reply = $state('');
	let currentBookingId = '';

	const upcoming = $derived(!!booking && booking.state === 'confirmed' && new Date(booking.starts_at) > new Date());
	const dateLabel = (value: string) => new Intl.DateTimeFormat(undefined, { dateStyle: 'full', timeStyle: 'short' }).format(new Date(value));
	const stateLabels: Record<string, string> = { confirmed: 'Confirmed', completed: 'Completed', cancelled: 'Cancelled', no_show_buyer: 'Missed by the buyer', no_show_seller: 'Missed by the seller' };
	const paymentLabels: Record<string, string> = { paid: 'Paid', simulated: 'Local simulation', refunded: 'Refunded', partially_refunded: 'Partly refunded' };

	async function load() {
		try {
			const response = await api<Lifecycle & { booking: Booking }>(`/api/v1/bookings/${encodeURIComponent(bookingId)}`);
			booking = response.booking;
			life = response;
			meeting = booking.meeting_url || '';
			exceptionOpen = !!booking.cancellation_open;
			const history = await api<{ requests: Reschedule[] }>(`/api/v1/bookings/${encodeURIComponent(booking.id)}/reschedules`);
			reschedules = history.requests;
		} catch (error) {
			message = error instanceof Error ? error.message : 'This booking could not be loaded.';
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		if (typeof window === 'undefined' || bookingId === currentBookingId) return;
		currentBookingId = bookingId;
		loading = true;
		booking = null;
		void load();
	});

	async function act(fn: () => Promise<unknown>, success: string, failure: string) {
		busy = true;
		message = '';
		try {
			await fn();
			message = success;
			await load();
		} catch (e) {
			message = e instanceof Error ? e.message : failure;
		} finally {
			busy = false;
		}
	}
	const post = (path: string, body: unknown = {}) => api(`/api/v1/bookings/${encodeURIComponent(booking!.id)}${path}`, { method: 'POST', body: JSON.stringify(body) });

	async function loadSlots() {
		if (!booking || !date) return;
		selected = '';
		message = '';
		try {
			const data = await api<{ slots: Slot[] }>(`/api/v1/people/${encodeURIComponent(booking.seller)}/slots?date=${encodeURIComponent(date)}&duration=${booking.duration_minutes}&tz=${encodeURIComponent(viewerTimeZone())}`);
			slots = data.slots;
		} catch (e) {
			message = e instanceof Error ? e.message : 'Available times could not be loaded.';
		}
	}
	const requestReschedule = () => act(async () => { await post('/reschedules', { proposed_starts_at: selected }); slots = []; selected = ''; }, 'Request sent. The current time stays booked until the other person accepts.', 'The request could not be sent.');
	const respond = (id: string, action: 'accept' | 'decline') => act(() => api(`/api/v1/reschedules/${encodeURIComponent(id)}/${action}`, { method: 'POST', body: '{}' }), action === 'accept' ? 'The booking time has changed for both of you.' : 'The original time stays.', 'The request could not be updated.');
	const saveMeeting = () => act(() => api(`/api/v1/bookings/${encodeURIComponent(booking!.id)}/meeting`, { method: 'PATCH', body: JSON.stringify({ meeting_url: meeting }) }), 'The private meeting link is saved.', 'The link could not be saved.');
	const reportIssue = () => act(async () => { await post('/issue', { reason: issue }); issue = ''; }, booking?.role === 'buyer' ? 'Thanks. WantMyTime will look into it and email you. The seller isn’t paid for this booking until it’s settled.' : 'Your report is with WantMyTime.', 'The report could not be saved.');
	const complete = () => act(() => post('/completion'), 'Thanks. Your confirmation is saved.', 'Completion could not be saved.');
	const requestException = () => act(async () => { await post('/cancellation', { reason: exceptionReason }); exceptionReason = ''; }, 'Your request is with WantMyTime. The booking stays as it is while it is reviewed.', 'The request could not be saved.');

	async function openCancel() {
		message = '';
		try {
			preview = await api<Preview>(`/api/v1/bookings/${encodeURIComponent(booking!.id)}/cancellation-preview`);
		} catch (e) {
			message = e instanceof Error ? e.message : 'Cancellation details could not be loaded.';
		}
	}
	async function confirmCancel() {
		if (!preview) return;
		busy = true;
		message = '';
		try {
			await post('/cancel', { expected_refund_minor: preview.refund_minor, reason: cancelReason.trim() });
			preview = null;
			message = 'The booking is cancelled. You’ll both get an email.';
			await load();
		} catch (e) {
			message = e instanceof Error ? e.message : 'The booking could not be cancelled.';
			await openCancel(); // show the up-to-date refund if it changed
		} finally {
			busy = false;
		}
	}
	const reportNoShow = () => act(() => post('/no-show'), 'Reported. They have 24 hours to respond before it stands.', 'The report could not be saved.');
	const disputeNoShow = () => act(async () => { await post('/no-show/dispute', { reason: disputeReason }); disputeReason = ''; }, 'Thanks. WantMyTime will review both sides and email you.', 'Your response could not be saved.');
	const submitReview = () => act(() => post('/review', { rating, body: reviewBody.trim() }), 'Thanks for your review.', 'Your review could not be saved.');
	const submitReply = () => act(() => api(`/api/v1/reviews/${encodeURIComponent(life!.review!.id)}/reply`, { method: 'POST', body: JSON.stringify({ body: reply.trim() }) }), 'Your reply is published under the review.', 'Your reply could not be saved.');

	function refundLine(r: { amount_minor: number; state: string }) {
		if (r.state === 'processed') return `${formatNaira(r.amount_minor)} refunded.`;
		if (r.state === 'not_required') return 'No refund needed: no money was collected for this test booking.';
		if (r.state === 'failed') return `A refund of ${formatNaira(r.amount_minor)} ran into a problem. WantMyTime is looking into it.`;
		return `A refund of ${formatNaira(r.amount_minor)} is being processed.`;
	}
</script>

<svelte:head><title>Your time — WantMyTime</title></svelte:head>
<section class={inWorkspace ? 'form-page app-page workspace-booking-detail' : 'form-page'}>
	<a class="back-link" href={backHref}>← Bookings</a>
	<p class="eyebrow">Booked</p>
	<h1 class="page-heading">{loading ? 'Loading…' : !booking ? 'Booking unavailable.' : booking.state === 'completed' ? 'This time is complete.' : upcoming ? 'You’re booked.' : booking.state === 'confirmed' ? 'This time has passed.' : booking.state === 'cancelled' ? 'This booking was cancelled.' : 'This booking was missed.'}</h1>

	{#if loading}
		<p class="page-intro">Loading your private booking details…</p>
	{:else if !booking || !life}
		<div class="notice notice-warning">{message}</div>
		{#if !inWorkspace}<p class="page-intro">Opened this from an email? <a href={`/access?next=${encodeURIComponent(`/booking/${bookingId}`)}`}>Open it with your email</a>.</p>{/if}
		<a class="button button-secondary" href={backHref}>Back to bookings</a>
	{:else}
		<div class="booking-detail-priority">
			<span>{(stateLabels[booking.state] ?? booking.state).toUpperCase()} · {(paymentLabels[booking.payment_state] ?? booking.payment_state).toUpperCase()}</span>
			<strong>{booking.role === 'seller' && upcoming && !booking.meeting_url ? 'ADD THE PRIVATE MEETING LINK' : booking.role === 'seller' ? 'MANAGE THIS BOOKING' : 'YOUR BOOKING DETAILS'}</strong>
			<p>{booking.role === 'seller' && upcoming && !booking.meeting_url ? `Due by ${dateLabel(booking.meeting_deadline)}. The buyer sees it when you save it.` : 'Everything about this call lives here: the time, the meeting link and any changes.'}</p>
		</div>
		<div class="appointment-slip">
			<div><small>WITH</small><strong>{booking.role === 'seller' ? booking.buyer : booking.seller_name}</strong></div>
			<div><small>WHEN</small><strong>{dateLabel(booking.starts_at)}</strong></div>
			<div><small>HOW LONG</small><strong>{booking.duration_minutes} minutes</strong></div>
			<div><small>PRICE</small><strong>{formatNaira(Number(booking.amount_minor))}</strong></div>
		</div>

		{#if booking.state === 'cancelled'}
			<div class="notice notice-info">Cancelled by {life.cancelled_by_role === booking.role ? 'you' : life.cancelled_by_role === 'seller' ? booking.seller_name : life.cancelled_by_role === 'buyer' ? booking.buyer : 'WantMyTime'}{life.cancelled_at ? ` on ${dateLabel(life.cancelled_at)}` : ''}.{#if life.refund} {refundLine(life.refund)}{:else if booking.role === 'buyer' && booking.payment_state === 'paid'} No refund was due under the {life.cancellation_policy.name.toLowerCase()} policy.{/if}</div>
		{:else if booking.state.startsWith('no_show')}
			<div class="notice notice-info">{booking.state === 'no_show_seller' ? 'Recorded as missed by the seller.' : 'Recorded as missed by the buyer.'}{#if life.refund} {refundLine(life.refund)}{/if}</div>
		{/if}

		{#if booking.payment_state === 'paid' || booking.payment_state.includes('refunded')}<a class="button button-secondary" href={`/booking/${encodeURIComponent(booking.id)}/receipt`}>View payment receipt ↗</a>{/if}
		{#if booking.payment_state === 'simulated'}<div class="notice notice-warning">Test booking: no money moved.</div>{/if}

		<div class="setup-form">
			{#if booking.state === 'confirmed' || booking.state === 'completed'}<a class="button button-secondary" href={`/api/v1/bookings/${encodeURIComponent(booking.id)}/calendar`}>Add to calendar ↗</a>{/if}

			{#if booking.role === 'seller' && upcoming}
				<label>Private meeting link · HTTPS <span class="form-note">Due by {dateLabel(booking.meeting_deadline)}</span><input class="field" type="url" bind:value={meeting} placeholder="https://…" /></label>
				<button class="button" onclick={saveMeeting} disabled={busy || !meeting.startsWith('https://')}>Save meeting link</button>
			{:else if booking.role === 'buyer' && booking.meeting_url && booking.state === 'confirmed'}
				<p class="notice notice-info">Your private meeting link is ready.</p><a class="text-link" href={booking.meeting_url} target="_blank" rel="noopener noreferrer">Open the meeting ↗</a>
			{:else if booking.role === 'buyer' && upcoming}
				<p class="notice notice-info">Your meeting link isn’t ready yet. It is due by {dateLabel(booking.meeting_deadline)}.</p>
			{/if}

			{#if upcoming || reschedules.some((r) => r.state === 'pending')}
				<section class="reschedule-section">
					<h2>Change the time</h2>
					<p>The current booking stays in place unless the other person accepts a new time.</p>
					{#each reschedules as request}
						<article class="list-card"><div><strong>{request.state === 'pending' ? 'New time proposed' : `Request ${request.state}`}</strong><p>{dateLabel(request.proposed_starts_at)}</p>{#if request.state === 'pending'}<small>{request.requested_by_me ? 'Waiting for the other person' : 'Expires ' + dateLabel(request.expires_at)}</small>{/if}</div>
							{#if request.state === 'pending' && !request.requested_by_me}<div class="ops-nav"><button class="button" onclick={() => respond(request.id, 'accept')} disabled={busy}>Accept new time</button><button class="button button-secondary" onclick={() => respond(request.id, 'decline')} disabled={busy}>Keep current time</button></div>{/if}
						</article>
					{/each}
					{#if upcoming && !reschedules.some((r) => r.state === 'pending')}
						<label>Choose another available date <span class="form-note">Times are in your time zone ({zoneCity(viewerTimeZone())}).</span><input class="field" type="date" bind:value={date} onchange={loadSlots} /></label>
						{#if slots.length}<fieldset><legend>Available times</legend><div class="slot-list">{#each slots as slot}<button type="button" class="slot-option" class:selected={selected === slot.starts_at} onclick={() => (selected = slot.starts_at)}>{slot.local_label}</button>{/each}</div><button class="button" onclick={requestReschedule} disabled={!selected || busy}>Request this time</button></fieldset>{:else if date}<p class="form-note">No available times on that date.</p>{/if}
					{/if}
				</section>
			{/if}

			{#if upcoming}
				<section class="reschedule-section" id="cancel">
					<h2>Cancel</h2>
					<p class="form-note">{life.cancellation_policy.name} policy: {life.cancellation_policy.summary}{booking.role === 'seller' ? ' If you cancel, the buyer is always refunded in full.' : ''}</p>
					{#if !preview}
						<button class="button button-secondary" onclick={openCancel} disabled={busy}>Cancel this booking…</button>
					{:else if !preview.can_cancel}
						<p class="notice notice-warning">{preview.reason}</p>
					{:else}
						<div class="notice notice-warning" role="alert">
							{#if !preview.paid}Cancelling frees the time for both of you.
							{:else if preview.role === 'seller'}The buyer will be refunded {formatNaira(preview.refund_minor)} in full, and there will be no payout for this booking.
							{:else if preview.refund_minor > 0}You’ll be refunded {formatNaira(preview.refund_minor)} ({preview.refund_percent}%).{#if preview.refund_drops_at} This drops after {dateLabel(preview.refund_drops_at)}.{/if}
							{:else}Under the {preview.policy_name.toLowerCase()} policy, cancelling now gives no refund.{/if}
						</div>
						<label>Reason (optional, shared with WantMyTime only)<textarea class="field" rows="2" maxlength="500" bind:value={cancelReason}></textarea></label>
						<div class="ops-nav"><button class="button" onclick={confirmCancel} disabled={busy}>{busy ? 'Cancelling…' : 'Yes, cancel the booking'}</button><button class="button button-secondary" onclick={() => (preview = null)} disabled={busy}>Keep it</button></div>
						{#if preview.role === 'buyer' && preview.refund_percent < 100 && !exceptionOpen}
							<details class="cancellation-request"><summary>Exceptional circumstances? Ask WantMyTime instead</summary><p>WantMyTime reviews the request. The booking stays in place until then.</p><label>What happened?<textarea class="field" rows="3" bind:value={exceptionReason} maxlength="500"></textarea></label><button class="button button-secondary" onclick={requestException} disabled={busy || exceptionReason.trim().length < 8}>Send request</button></details>
						{/if}
					{/if}
					{#if exceptionOpen}<p class="notice notice-info">Your request to WantMyTime is open. The booking stays as it is while it’s reviewed.</p>{/if}
				</section>
			{/if}

			{#if booking.state === 'confirmed' && !upcoming}
				<p>Took place? You {booking.role === 'buyer' ? (booking.buyer_completed ? '✓' : '—') : booking.seller_completed ? '✓' : '—'} · the other person {booking.role === 'buyer' ? (booking.seller_completed ? '✓' : '—') : booking.buyer_completed ? '✓' : '—'}</p>
				<button class="button button-secondary" onclick={complete} disabled={busy || (booking.role === 'buyer' ? booking.buyer_completed : booking.seller_completed)}>It took place</button>
			{/if}

			{#if life.no_show}
				{#if life.no_show.about_me && life.no_show.state === 'open'}
					<section class="reschedule-section"><h2>You were reported as not joining</h2><p>If you were there, tell us before {dateLabel(life.no_show.resolves_at)} and WantMyTime will review both sides.</p><label>What happened?<textarea class="field" rows="3" bind:value={disputeReason} maxlength="1000"></textarea></label><button class="button" onclick={disputeNoShow} disabled={busy || disputeReason.trim().length < 8}>I was there</button></section>
				{:else}
					<p class="notice notice-info">{life.no_show.state === 'open' ? (life.no_show.reported_by_me ? `You reported a no-show. It stands on ${dateLabel(life.no_show.resolves_at)} unless it’s disputed.` : 'A no-show report is open.') : life.no_show.state === 'disputed' ? 'The no-show report was disputed and is being reviewed by WantMyTime.' : life.no_show.state === 'rejected' ? 'The no-show report was reviewed and not upheld.' : 'The no-show report stands.'}</p>
				{/if}
			{:else if life.can_report_no_show && !(booking.role === 'buyer' ? booking.buyer_completed : booking.seller_completed)}
				<details class="cancellation-request"><summary>{booking.role === 'buyer' ? `${booking.seller_name} didn’t show up?` : `${booking.buyer} didn’t show up?`}</summary><p>{booking.role === 'buyer' ? 'If it isn’t disputed within 24 hours, you get a full refund.' : 'If it isn’t disputed within 24 hours, the booking is recorded as missed and stands.'}</p><button class="button button-secondary" onclick={reportNoShow} disabled={busy}>Report a no-show</button></details>
			{/if}

			<section class="reschedule-section" id="review">
				{#if life.review}
					<h2>Review</h2>
					<p class="review-stars" aria-label={`${life.review.rating} out of 5`}>{'★'.repeat(life.review.rating)}{'☆'.repeat(5 - life.review.rating)}</p>
					{#if life.review.body}<p>“{life.review.body}”</p>{/if}
					{#if life.review.seller_reply}<p class="form-note">Reply from {booking.seller_name}: {life.review.seller_reply}</p>
					{:else if booking.role === 'seller'}<label>Reply publicly (once)<textarea class="field" rows="2" maxlength="1000" bind:value={reply}></textarea></label><button class="button button-secondary" onclick={submitReply} disabled={busy || !reply.trim()}>Publish reply</button>{/if}
				{:else if life.can_review}
					<h2>How was it?</h2>
					<div class="star-input" role="radiogroup" aria-label="Rating">{#each [1, 2, 3, 4, 5] as n}<button type="button" role="radio" aria-checked={rating === n} aria-label={`${n} star${n > 1 ? 's' : ''}`} class:on={rating >= n} onclick={() => (rating = n)}>★</button>{/each}</div>
					<label>Anything to add? (optional, public, first name only)<textarea class="field" rows="3" maxlength="1000" bind:value={reviewBody}></textarea></label>
					<button class="button" onclick={submitReview} disabled={busy || rating === 0}>Post review</button>
				{/if}
			</section>

			{#if life.problem.open}
				<div class="notice notice-warning">{life.problem.reported_by === booking.role ? 'You reported a problem' : life.problem.reported_by === 'buyer' ? `${booking.buyer} reported a problem` : 'A problem was reported'}: “{(booking.issue_reason ?? '').replace(/[.!?\s]+$/, '')}”. WantMyTime is looking into it{booking.role === 'seller' && life.problem.reported_by === 'buyer' ? ' and your payout for this booking waits until then' : ''}.</div>
			{:else if life.problem.resolution}
				<p class="notice notice-info">WantMyTime reviewed the reported problem: {life.problem.resolution}</p>
			{/if}
			{#if life.can_report_problem}
				<details class="cancellation-request"><summary>Something went wrong?</summary>
					{#if booking.role === 'buyer' && booking.payment_state === 'paid'}<p>Tell WantMyTime by {dateLabel(life.problem_deadline)}. After that the seller is paid and a refund can no longer be requested.</p>{/if}
					<label>Tell WantMyTime what happened<textarea class="field" rows="3" bind:value={issue} maxlength="1000"></textarea></label><button class="button button-secondary" onclick={reportIssue} disabled={busy || issue.trim().length < 8}>Report a problem</button></details>
			{:else if booking.role === 'buyer' && !life.problem.open && (booking.state === 'confirmed' || booking.state === 'completed') && booking.payment_state === 'paid'}
				<p class="form-note">The time to report a problem with this booking ended {dateLabel(life.problem_deadline)}.</p>
			{/if}

			{#if booking.role === 'seller' && life.payout}
				<section class="reschedule-section" id="payout">
					<h2>Your payout</h2>
					{#if life.payout.state === 'paid'}<p>{formatNaira(life.payout.transfer_minor ?? 0)} sent to {life.payout.bank_name} ••{life.payout.account_last4}{life.payout.paid_at ? ` on ${dateLabel(life.payout.paid_at)}` : ''}.{#if life.payout.recovery_minor > 0} {formatNaira(life.payout.recovery_minor)} was kept to repay an earlier refund.{/if}</p>
					{:else if life.payout.state === 'cancelled'}<p>No payout: the buyer was refunded in full.</p>
					{:else if life.payout.state === 'processing'}<p>{formatNaira(life.payout.transfer_minor ?? 0)} is on its way to your bank.{#if life.payout.note} {life.payout.note}{/if}</p>
					{:else if life.payout.state === 'failed'}<p class="notice notice-warning">The transfer didn’t go through. WantMyTime has been alerted; check your bank details under <a href="/app/settings/payouts">Payouts</a>.</p>
					{:else if life.payout.hold}<p>{life.payout.hold_text}</p>
					{:else}<p>{formatNaira(life.payout.entitlement_minor)}{booking.state === 'cancelled' ? ' less any refund' : ''}, paid to your bank around {dateLabel(life.payout.release_at)}.{#if life.payout.note} {life.payout.note}{/if}</p>{/if}
				</section>
			{/if}
		</div>
		{#if message}<p class="notice notice-info" aria-live="polite">{message}</p>{/if}
	{/if}
</section>
