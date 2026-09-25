<script lang="ts">
	import { env } from '$env/dynamic/public';
	import { page } from '$app/state';
	import { brand } from '$lib/brand';
	// Title, description, canonical address and share previews for public pages.
	let { title, description } = $props<{ title: string; description: string }>();
	const origin = (env.PUBLIC_APP_ORIGIN || `https://${brand.domain}`).replace(/\/$/, '');
	let url = $derived(`${origin}${page.url.pathname === '/' ? '/' : page.url.pathname}`);
</script>

<svelte:head>
	<title>{title}</title>
	<meta name="description" content={description} />
	<link rel="canonical" href={url} />
	<meta property="og:type" content="website" />
	<meta property="og:site_name" content={brand.name} />
	<meta property="og:title" content={title} />
	<meta property="og:description" content={description} />
	<meta property="og:url" content={url} />
	<meta property="og:image" content={`${origin}/og-default.png`} />
	<meta property="og:image:width" content="1200" />
	<meta property="og:image:height" content="630" />
	<meta property="og:image:alt" content={`${brand.name}: put a price on your time`} />
	<meta name="twitter:card" content="summary_large_image" />
</svelte:head>
