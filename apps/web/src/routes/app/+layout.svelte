<script lang="ts">
	import { page, navigating } from '$app/state';
	import { api } from '$lib/api';

	let { data, children } = $props();
	let account = $derived(data.account as { name: string; email: string; handle: string });
	let signOutError = $state('');
	// Five places, no more. Settings sits behind the account initial.
	const navigation = [
		{ href: '/app', label: 'Home', number: '01', also: [] as string[] },
		{ href: '/app/bookings', label: 'Bookings', number: '02', also: ['/app/offers'] },
		{ href: '/app/link', label: 'Your page', number: '03', also: [] },
		{ href: '/app/availability', label: 'Hours', number: '04', also: [] },
		{ href: '/app/money', label: 'Money', number: '05', also: [] }
	];
	let activePath = $derived(navigating.to?.url.pathname ?? page.url.pathname);
	const under = (href: string) => activePath === href || activePath.startsWith(`${href}/`);
	function current(item: { href: string; also: string[] }) {
		return item.href === '/app' ? activePath === '/app' : [item.href, ...item.also].some(under);
	}
	let inSettings = $derived(under('/app/settings'));
	async function signOut() {
		signOutError = '';
		try {
			await api('/api/v1/auth/logout', { method: 'POST' });
			window.location.href = '/login';
		} catch {
			signOutError = 'Could not sign out. Please try again.';
		}
	}
</script>

<div class="workspace">
	<aside class="workspace-rail" aria-label="Workspace navigation">
		<div class="workspace-rail-head">
			<a class="workspace-brand" href="/">WantMyTime<span>.</span></a><span>YOUR SPACE</span>
		</div>
		<nav aria-label="Workspace" data-sveltekit-preload-data="hover">
			{#each navigation as item (item.href)}
				<a
					href={item.href}
					class:active={current(item)}
					aria-current={current(item) ? 'page' : undefined}
					data-sveltekit-preload-data="tap"
					><small>{item.number}</small><span>{item.label}</span><span aria-hidden="true">↗</span></a
				>
			{/each}
		</nav>
		<div class="workspace-rail-foot">
			<span>ACCOUNT</span><strong>{account.name}</strong><small>{account.email}</small><a
				href="/app/settings"
				class:active={inSettings}
				aria-current={inSettings ? 'page' : undefined}
				data-sveltekit-preload-data="tap">SETTINGS <span aria-hidden="true">↗</span></a
			><button type="button" onclick={signOut}>SIGN OUT <span aria-hidden="true">↗</span></button>{#if signOutError}<p
					role="alert"
				>
					{signOutError}
				</p>{/if}
		</div>
	</aside>
	<div class="workspace-main">
		<header class="workspace-topbar">
			<span>WANTMYTIME / YOUR WORKSPACE</span>
			<div>
				{#if account.handle}<a href={`/${account.handle}`}>VIEW PUBLIC PAGE <span aria-hidden="true">↗</span></a>{/if}<a
					class="workspace-account-mark"
					class:active={inSettings}
					href="/app/settings"
					aria-label="Settings"
					title="Settings"
					data-sveltekit-preload-data="tap">{account.name?.slice(0, 1).toUpperCase() || 'A'}</a
				>
			</div>
		</header>
		{#if signOutError}<p class="workspace-signout-error" role="alert">{signOutError}</p>{/if}
		<nav class="workspace-mobile-nav" aria-label="Workspace sections" data-sveltekit-preload-data="tap">
			{#each navigation as item (item.href)}<a
					href={item.href}
					class:active={current(item)}
					aria-current={current(item) ? 'page' : undefined}
					data-sveltekit-preload-data="tap">{item.label}</a
				>{/each}
		</nav>
		<div class="workspace-body" id="workspace-content" tabindex="-1">{@render children()}</div>
	</div>
</div>
