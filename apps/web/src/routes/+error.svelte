<script lang="ts">
	import { page } from '$app/state';
</script>

<svelte:head><title>{page.status === 404 ? 'Not found' : 'Something went wrong'} — WantMyTime</title></svelte:head>
<section class="form-page">
	<p class="eyebrow">{page.status === 404 ? 'Not found' : 'Error'}</p>
	<h1 class="page-heading">{page.status === 404 ? 'This page isn’t here.' : 'Something went wrong.'}</h1>
	<p class="page-intro">{page.error?.message ?? 'Please try again.'}</p>
	{#if page.status >= 500 && page.error?.reference}<p class="form-note">
			If you contact support, quote reference <code>{page.error.reference}</code>.
		</p>{/if}
	<a class="button button-secondary" href="/">Go to the homepage</a>
</section>
