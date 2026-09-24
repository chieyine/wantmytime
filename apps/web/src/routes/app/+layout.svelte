<script lang="ts">
	import { page } from '$app/state';
	import { api } from '$lib/api';

	let { data, children } = $props();
	let account = $derived(data.account as { name: string; email: string; handle: string });
	let signOutError = $state('');
	const navigation = [
		{ href: '/app', label: 'Overview', number: '01' },
		{ href: '/app/bookings', label: 'Bookings', number: '02' },
		{ href: '/app/offers', label: 'Offers', number: '03' },
		{ href: '/app/link', label: 'Your link', number: '04' },
		{ href: '/app/availability', label: 'Availability', number: '05' },
		{ href: '/app/money', label: 'Money', number: '06' },
		{ href: '/app/share', label: 'Share', number: '07' },
		{ href: '/app/settings', label: 'Settings', number: '08' }
	];
	function current(href: string) {
		return href === '/app' ? page.url.pathname === href : page.url.pathname === href || page.url.pathname.startsWith(`${href}/`);
	}
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
		<div class="workspace-rail-head"><a class="workspace-brand" href="/">WantMyTime<span>.</span></a><span>YOUR SPACE</span></div>
		<nav aria-label="Workspace">
			{#each navigation as item}
				<a href={item.href} class:active={current(item.href)} aria-current={current(item.href) ? 'page' : undefined}><small>{item.number}</small><span>{item.label}</span><span aria-hidden="true">↗</span></a>
			{/each}
		</nav>
		<div class="workspace-rail-foot"><span>ACCOUNT</span><strong>{account.name}</strong><small>{account.email}</small><button type="button" onclick={signOut}>SIGN OUT <span aria-hidden="true">↗</span></button>{#if signOutError}<p role="alert">{signOutError}</p>{/if}</div>
	</aside>
	<div class="workspace-main">
		<header class="workspace-topbar"><span>WANTMYTIME / YOUR WORKSPACE</span><div>{#if account.handle}<a href={`/${account.handle}`}>VIEW PUBLIC PAGE <span aria-hidden="true">↗</span></a>{/if}<button class="workspace-mobile-signout" type="button" onclick={signOut}>SIGN OUT</button><span class="workspace-account-mark" aria-label={account.name}>{account.name?.slice(0, 1).toUpperCase() || 'A'}</span></div></header>
		{#if signOutError}<p class="workspace-signout-error" role="alert">{signOutError}</p>{/if}
		<nav class="workspace-mobile-nav" aria-label="Workspace sections">{#each navigation as item}<a href={item.href} class:active={current(item.href)} aria-current={current(item.href) ? 'page' : undefined}>{item.label}</a>{/each}</nav>
		<div class="workspace-body" id="workspace-content">{@render children()}</div>
	</div>
</div>
