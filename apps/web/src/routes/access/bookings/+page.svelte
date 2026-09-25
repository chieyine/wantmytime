<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { formatNaira } from '$lib/money';
	import CursorPager from '$lib/components/CursorPager.svelte';
	type Booking={id:string;seller:string;buyer:string;duration_minutes:number;starts_at:string;amount_minor:number;state:string;payment_state:string};
	let bookings=$state<Booking[]>([]), loading=$state(true), message=$state(''), nextCursor=$state('');
	async function load(cursor=''){loading=true;try{const q=cursor?`?cursor=${encodeURIComponent(cursor)}`:'';const page=await api<{bookings:Booking[];next_cursor:string}>(`/api/v1/me/bookings${q}`);bookings=cursor?[...bookings,...page.bookings]:page.bookings;nextCursor=page.next_cursor}catch(e){message=e instanceof Error?e.message:'Bookings could not be loaded.'}finally{loading=false}}
	onMount(()=>{void load()});
</script>
<svelte:head><title>Your bookings — WantMyTime</title><meta name="robots" content="noindex,nofollow" /></svelte:head>
<section class="form-page"><a class="back-link" href="/">← WantMyTime</a><p class="eyebrow">Private access</p><h1 class="page-heading">Your bookings.</h1>
	{#if loading&&!bookings.length}<p class="page-intro">Loading your bookings…</p>{:else if message&&!bookings.length}<div class="notice notice-warning">{message}</div>{:else if !bookings.length}<p class="page-intro">No bookings are connected to this verified email.</p>{:else}<div class="list-stack">{#each bookings as booking}<a class="list-card" href={`/booking/${encodeURIComponent(booking.id)}`}><div><strong>Time with {booking.seller}</strong><p>{booking.duration_minutes} minutes · {new Date(booking.starts_at).toLocaleString()}</p><small>{booking.state} · {booking.payment_state}</small></div><strong>{formatNaira(booking.amount_minor)}</strong></a>{/each}</div><CursorPager cursor={nextCursor} busy={loading} onNext={()=>load(nextCursor)}/>{/if}
<p class="form-note">Want a copy of your data, or your account deleted? <a class="text-link" href="/app/settings/data">Sign in to manage your data ↗</a></p>
</section>
