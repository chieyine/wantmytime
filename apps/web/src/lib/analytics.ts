export async function recordProductEvent(event: string, handle = '', props: Record<string, string | number | boolean> = {}): Promise<void> {
	try {
		await fetch('/api/v1/analytics/events', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ event, handle, ...(event === 'seller_cta_clicked' && typeof props.booking_id === 'string' ? { booking_id: props.booking_id } : {}) })
		});
	} catch {
		// Product analytics is optional and must not interfere with the core flow.
	}
}
