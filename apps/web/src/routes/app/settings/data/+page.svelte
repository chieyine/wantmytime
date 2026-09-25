<script lang="ts">
	let { data } = $props();
	let check = $derived(data.check);
	let loadError = $derived(data.loadError);
	let confirmEmail = $state('');
	let busy = $state(false);
	let message = $state('');
	let blockers = $derived(data.check?.blockers ?? []);
	let matches = $derived(!!check && confirmEmail.trim().toLowerCase() === check.email.toLowerCase());

	async function deleteAccount(event: SubmitEvent) {
		event.preventDefault();
		if (!matches) return;
		busy = true;
		message = '';
		try {
			const response = await fetch('/api/v1/me/deletion', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ confirm_email: confirmEmail.trim() })
			});
			const body = await response.json().catch(() => null);
			if (response.status === 409 && body?.blockers) {
				blockers = body.blockers;
				throw new Error('Your account can’t be deleted yet.');
			}
			if (!response.ok) throw new Error(body?.error?.message ?? 'Your account could not be deleted.');
			window.location.href = '/';
		} catch (e) {
			message = e instanceof Error ? e.message : 'Your account could not be deleted.';
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>Your data — WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href="/app/settings">← Settings</a>
	<p class="eyebrow">Your data</p>
	<h1 class="page-heading">What we hold, and how to leave.</h1>
	<p class="page-intro">
		Download everything WantMyTime holds about you, or delete your account. <a class="text-link" href="/privacy"
			>How we handle your data ↗</a
		>
	</p>

	<section class="readiness-panel">
		<h2>Download your data</h2>
		<p class="form-note">
			One file with your account, link, availability, bookings, offers, payments, payouts, reviews and the emails we’ve
			sent you. Bank account numbers appear only as their last four digits. You can download it three times a day.
		</p>
		<a class="button button-secondary" href="/api/v1/me/data-export" download>Download my data ↓</a>
	</section>

	<section class="readiness-panel">
		<h2>Delete your account</h2>
		<p class="form-note">
			Your link goes offline straight away and your name, email, photo, bank account, calendar connection and
			availability are erased. Nobody else can claim your link for six months.
		</p>
		<p class="form-note">
			By law we keep records of payments, refunds and payouts for six years, with your name removed from them.
		</p>
		{#if loadError}
			<div class="notice notice-warning" role="alert">{loadError}</div>
		{:else if !check}
			<p class="form-note">Checking your account…</p>
		{:else}
			{#if blockers.length}
				<div class="notice notice-warning" role="status">
					<strong>Not yet. First:</strong>
					<ul>
						{#each blockers as blocker (blocker)}<li>{blocker}</li>{/each}
					</ul>
				</div>
			{/if}
			<form class="setup-form" onsubmit={deleteAccount}>
				<label
					>Type your email address to confirm
					<input
						class="field"
						type="email"
						autocomplete="off"
						bind:value={confirmEmail}
						placeholder={check.email}
						disabled={blockers.length > 0}
					/>
				</label>
				<button class="button button-danger" type="submit" disabled={!matches || busy || blockers.length > 0}
					>{busy ? 'Deleting…' : 'Delete my account'}</button
				>
			</form>
		{/if}
		{#if message}<p class="notice notice-error" role="alert">{message}</p>{/if}
	</section>
</section>
