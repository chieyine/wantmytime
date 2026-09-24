<script lang="ts">
	import BookingDetail from '$lib/components/BookingDetail.svelte';
	let { params } = $props();
</script>

<BookingDetail bookingId={params.id} backHref="/app/bookings" inWorkspace={true} />
