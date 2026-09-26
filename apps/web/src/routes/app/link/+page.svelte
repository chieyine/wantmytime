<script lang="ts">
	import { siteOrigin } from '$lib/site';
	import { untrack } from 'svelte';
	import { api } from '$lib/api';
	import { currencySymbol } from '$lib/money';
	import PublicPersonPage from '$lib/components/PublicPersonPage.svelte';
	import { handlePattern, linkLabel, validHandle } from '$lib/handle';
	import { fetchHandleSuggestions } from '$lib/handle-suggestions';
	import HandleSuggestions from '$lib/components/HandleSuggestions.svelte';
	let { data } = $props();
	// The form starts from the saved profile the route loaded, then belongs to the page while the seller edits.
	const saved = untrack(() => data.profile);
	let handle = $state(saved?.handle ?? '');
	let name = $state(saved?.name ?? '');
	let identity_url = $state(saved?.identity_url || '');
	let amount = $state(saved ? saved.base_30_minor / 100 : 10000);
	// A price, plus one switch for offers.
	let acceptOffers = $state(saved?.mode === 'both');
	let mode = $derived<'fixed' | 'both'>(acceptOffers ? 'both' : 'fixed');
	let durations = $state<number[]>(saved?.durations ?? [15, 30, 60]);
	let timezone = $state(saved?.timezone ?? 'UTC');
	let currency = $state(saved?.currency || 'NGN');
	let paused = $state(saved?.paused ?? false);
	const exists = !!saved;
	let saving = $state(false);
	let message = $state(untrack(() => data.loadError));
	let ready = $state(saved?.ready ?? false);
	let activeView = $state<'edit' | 'preview'>('edit');
	let avatarPreview = $state(
		saved?.avatar_version ? `/api/v1/people/${encodeURIComponent(saved.handle)}/avatar?v=${saved.avatar_version}` : ''
	);
	let avatarMessage = $state('');
	let avatarBusy = $state(false);
	let avatarInput = $state<HTMLInputElement>();
	let savedFingerprint = $state(
		saved
			? JSON.stringify({
					name: saved.name,
					identity_url: saved.identity_url || '',
					mode: saved.mode,
					amount: saved.base_30_minor,
					durations: saved.durations,
					timezone: saved.timezone,
					paused: saved.paused
				})
			: ''
	);
	let saveState = $state<'idle' | 'saved' | 'error'>('idle');
	let fingerprint = $derived(
		JSON.stringify({
			name,
			identity_url,
			mode,
			amount: Math.round(Number(amount || 0) * 100),
			durations,
			timezone,
			paused
		})
	);
	let dirty = $derived(exists && fingerprint !== savedFingerprint);
	let identityLabel = $derived.by(() => {
		try {
			const h = new URL(identity_url).hostname.toLowerCase().replace(/^www\./, '');
			return (
				(
					{
						'instagram.com': 'Instagram',
						'linkedin.com': 'LinkedIn',
						'x.com': 'X',
						'twitter.com': 'X',
						'tiktok.com': 'TikTok',
						'youtube.com': 'YouTube',
						'github.com': 'GitHub'
					} as Record<string, string>
				)[h] ?? ''
			);
		} catch {
			return '';
		}
	});
	let previewPerson = $derived({
		handle,
		name,
		identity_url,
		identity_label: identityLabel,
		avatar_url: avatarPreview,
		mode,
		base_30_minor: Math.round(Number(amount || 0) * 100),
		durations,
		timezone,
		paused,
		ready,
		currency,
		local_simulator: false
	});
	// The link itself changes on its own, because people may already have the old one.
	let newHandle = $state(saved?.handle ?? '');
	let handleState = $state<'same' | 'checking' | 'available' | 'taken' | 'invalid'>('same');
	let handleBusy = $state(false);
	let handleMessage = $state('');
	let handleSuggestions = $state<string[]>([]);
	let linkHost = $derived(linkLabel(siteOrigin()));
	// A link can change once every six months; until then the field is read-only.
	let nextChange = $state(saved?.next_handle_change_at ?? '');
	let locked = $derived(!!nextChange && new Date(nextChange) > new Date());
	const longDate = (value: string) =>
		new Date(value).toLocaleDateString(undefined, { day: 'numeric', month: 'long', year: 'numeric' });
	$effect(() => {
		const wanted = newHandle.trim().toLowerCase();
		if (!exists || wanted === handle) {
			handleState = 'same';
			return;
		}
		if (!validHandle(wanted)) {
			handleState = 'invalid';
			return;
		}
		handleState = 'checking';
		const timer = setTimeout(async () => {
			try {
				// One request: is it free, and if not, which links from the seller's name are.
				const r = await fetchHandleSuggestions(name, wanted);
				if (newHandle.trim().toLowerCase() !== wanted) return;
				handleState = r.wanted?.available ? 'available' : 'taken';
				handleSuggestions = r.suggestions;
			} catch {
				handleState = 'same';
			}
		}, 350);
		return () => clearTimeout(timer);
	});
	async function changeLink() {
		if (handleState !== 'available' || handleBusy) return;
		handleBusy = true;
		handleMessage = '';
		try {
			const r = await api<{ handle: string; previous: string; next_handle_change_at: string }>(
				'/api/v1/me/link/handle',
				{
					method: 'PUT',
					body: JSON.stringify({ handle: newHandle.trim().toLowerCase() })
				}
			);
			handle = r.handle;
			nextChange = r.next_handle_change_at;
			newHandle = r.handle;
			if (avatarPreview)
				avatarPreview = avatarPreview.replace(
					`/people/${encodeURIComponent(r.previous)}/`,
					`/people/${encodeURIComponent(r.handle)}/`
				);
			handleMessage = `Your link is now ${linkHost}/${r.handle}. The old one forwards here. You can change it again on ${longDate(r.next_handle_change_at)}.`;
		} catch (error) {
			handleMessage = error instanceof Error ? error.message : 'Your link could not be changed.';
		} finally {
			handleBusy = false;
		}
	}
	function toggle(d: number) {
		durations = durations.includes(d) ? durations.filter((x) => x !== d) : [...durations, d].sort((a, b) => a - b);
	}
	async function save() {
		if (!dirty) return;
		saving = true;
		message = '';
		const submittedFingerprint = fingerprint;
		const payload = {
			handle,
			name,
			identity_url,
			mode,
			base_30_minor: Math.round(Number(amount || 0) * 100),
			durations: [...durations],
			timezone,
			paused
		};
		try {
			await api('/api/v1/me/link', { method: 'PATCH', body: JSON.stringify(payload) });
			savedFingerprint = submittedFingerprint;
			saveState = 'saved';
			message = 'Your changes are saved.';
		} catch (error) {
			saveState = 'error';
			message = error instanceof Error ? error.message : 'Your changes could not be saved.';
		} finally {
			saving = false;
		}
	}
	async function uploadAvatar() {
		const file = avatarInput?.files?.[0];
		if (!file) return;
		avatarMessage = '';
		if (!['image/png', 'image/jpeg'].includes(file.type)) {
			avatarMessage = 'Choose a PNG or JPEG image.';
			return;
		}
		if (file.size > 2 * 1024 * 1024) {
			avatarMessage = 'Choose an image smaller than 2 MB.';
			return;
		}
		avatarBusy = true;
		try {
			const data = new FormData();
			data.append('avatar', file);
			const response = await fetch('/api/v1/me/avatar', { method: 'PUT', body: data });
			const body = await response.json();
			if (!response.ok) throw new Error(body?.error?.message || 'The image could not be saved.');
			avatarPreview = `${body.url}?v=${body.avatar_version}`;
			avatarMessage = 'Profile image saved.';
		} catch (e) {
			avatarMessage = e instanceof Error ? e.message : 'The image could not be saved.';
		} finally {
			avatarBusy = false;
		}
	}
	async function removeAvatar() {
		avatarBusy = true;
		avatarMessage = '';
		try {
			const response = await fetch('/api/v1/me/avatar', { method: 'DELETE' });
			if (!response.ok) throw new Error('The image could not be removed.');
			avatarPreview = '';
			avatarMessage = 'Profile image removed.';
		} catch (e) {
			avatarMessage = e instanceof Error ? e.message : 'The image could not be removed.';
		} finally {
			avatarBusy = false;
		}
	}
</script>

<svelte:head><title>Your link — WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href="/app">← Home</a>
	<p class="eyebrow">Your page</p>
	<h1 class="page-heading">A page that sounds like you.</h1>
	{#if !exists}<p class="page-intro">
			You have not claimed a link yet. Set up your name, link, price and conversation lengths.
		</p>
		<a class="button" href="/claim">Create your link ↗</a>{#if message}<div class="notice notice-warning" role="alert">
				{message}
			</div>{/if}
	{:else}<div class="link-editor-status">
			<div>
				<span class:dirty class:saved={saveState === 'saved' && !dirty} class="link-editor-status-dot"></span><strong
					>{saving
						? 'SAVING'
						: dirty
							? 'UNSAVED CHANGES'
							: saveState === 'saved'
								? 'CHANGES SAVED'
								: 'ALL CHANGES SAVED'}</strong
				>
			</div>
			<p>Changes appear in the preview immediately. Your public page updates when you save.</p>
		</div>
		<div class="link-editor-tabs" aria-label="Link editor view">
			<button class:active={activeView === 'edit'} onclick={() => (activeView = 'edit')}>Edit your page</button><button
				class:active={activeView === 'preview'}
				onclick={() => (activeView = 'preview')}>Preview</button
			>
		</div>
		<div class="link-editor-layout">
			<form
				class="setup-form link-editor-form"
				class:mobile-hidden={activeView === 'preview'}
				onsubmit={(e) => {
					e.preventDefault();
					save();
				}}
			>
				<div class="link-handle">
					<label for="link-handle-input">Your link</label>
					<div class="handle-input">
						<span>{linkHost}/</span><input
							id="link-handle-input"
							class="field"
							bind:value={newHandle}
							readonly={locked}
							minlength="3"
							maxlength="24"
							pattern={handlePattern}
							autocapitalize="off"
							autocomplete="off"
							spellcheck="false"
							aria-describedby="link-handle-note"
							onkeydown={(e) => {
								if (e.key === 'Enter') {
									e.preventDefault();
									changeLink();
								}
							}}
						/>
					</div>
					<p id="link-handle-note" class="form-note" aria-live="polite">
						{#if handleMessage}{handleMessage}{:else if handleState === 'checking'}Checking…{:else if handleState === 'available'}{linkHost}/{newHandle
								.trim()
								.toLowerCase()} is free. Your old link will keep working and forward here. After this, you can’t change it
							again for 6 months.{:else if handleState === 'taken'}That link is taken. Pick a free one below or try
							another.{:else if handleState === 'invalid'}Use 3 to 24 letters, numbers or single hyphens.{:else if locked}This
							is the address people use to book you. You can change it again on {longDate(nextChange)}, and we’ll email
							you then.{:else}This is the address people use to book you. You can change it once every 6 months.{/if}
					</p>
					{#if handleState === 'taken'}<HandleSuggestions
							suggestions={handleSuggestions}
							current={newHandle}
							label="Free"
							onpick={(next) => (newHandle = next)}
						/>{/if}
					{#if handleState === 'available'}<button
							type="button"
							class="button button-small"
							onclick={changeLink}
							disabled={handleBusy}>{handleBusy ? 'Changing…' : 'Change link'}</button
						>{/if}
				</div>
				<label>Display name<input class="field" bind:value={name} maxlength="80" required /></label>
				<label
					>Your price for 30 minutes
					<div class="money-input">
						<span>{currencySymbol(currency).trim()}</span><input
							type="number"
							bind:value={amount}
							inputmode="decimal"
							min="1"
							step="0.01"
							required
						/>
					</div>
					<span class="form-note">15 minutes costs half this, 60 minutes costs double.</span></label
				>
				<label class="check-line"
					><input type="checkbox" bind:checked={acceptOffers} /> Also let people offer a different price</label
				>
				{#if acceptOffers}<span class="form-note"
						>People can book at your price straight away, or send an offer you can accept, counter or decline.</span
					>{/if}
				<fieldset>
					<legend>Available conversation lengths</legend>
					<div class="duration-row">
						{#each [15, 30, 60] as d (d)}<label class:selected={durations.includes(d)}
								><input type="checkbox" checked={durations.includes(d)} onchange={() => toggle(d)} /> {d} min</label
							>{/each}
					</div>
				</fieldset>
				<fieldset class="link-extras">
					<legend>Optional</legend>
					<label
						>Profile photo<input
							class="field"
							bind:this={avatarInput}
							type="file"
							accept="image/png,image/jpeg"
							onchange={uploadAvatar}
							disabled={avatarBusy}
						/><span class="form-note">PNG or JPEG, up to 2 MB. We remove embedded metadata when saving.</span
						>{#if avatarMessage}<span class="form-note" aria-live="polite">{avatarMessage}</span
							>{/if}{#if avatarPreview}<img src={avatarPreview} class="avatar-upload-preview" alt="" /><button
								type="button"
								class="text-link"
								onclick={removeAvatar}
								disabled={avatarBusy}>Remove photo</button
							>{/if}</label
					><label
						>Link to one social profile<input
							class="field"
							type="url"
							bind:value={identity_url}
							placeholder="https://instagram.com/…"
						/></label
					>
				</fieldset>
				<label class="check-line"><input type="checkbox" bind:checked={paused} /> Pause new booking requests</label>
				<div class="notice notice-warning">
					Paused means your page stays up but nobody can book. Bookings you already have stay in place.
				</div>
				<div class="link-editor-save">
					<span>{dirty ? 'YOUR PUBLIC PAGE HAS NOT CHANGED YET' : 'YOUR PUBLIC PAGE MATCHES THIS EDITOR'}</span><button
						class="button"
						type="submit"
						disabled={saving || !dirty || durations.length === 0}
						>{saving ? 'Saving…' : 'Save changes'} <span>↗</span></button
					>
				</div>
				{#if message && (!dirty || saveState === 'error')}<p
						class:notice-warning={saveState === 'error'}
						class:notice-info={saveState !== 'error'}
						class="notice"
						aria-live="polite"
					>
						{message}
					</p>{/if}
			</form>
			<div class="link-editor-preview" class:mobile-hidden={activeView === 'edit'}>
				<div class="link-editor-preview-head">
					<p class="eyebrow">Live private preview</p>
					<span>{dirty ? 'DRAFT / NOT PUBLISHED' : 'MATCHES PUBLIC PAGE'}</span>
				</div>
				<PublicPersonPage person={previewPerson} preview={true} />
			</div>
		</div>{/if}
</section>
