// Time-zone helpers. Times are always stored as instants; these only decide
// which zone a person sees them in.

/** The viewer's IANA zone from the browser, or UTC when unavailable. */
export function viewerTimeZone(): string {
	try {
		return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
	} catch {
		return 'UTC';
	}
}

/** Today's date (YYYY-MM-DD) in a zone, for date inputs. */
export function todayIn(zone: string): string {
	try {
		return new Intl.DateTimeFormat('en-CA', { timeZone: zone, year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date());
	} catch {
		return new Date().toISOString().slice(0, 10);
	}
}

/** "America/New_York" -> "New York". */
export function zoneCity(zone: string): string {
	const last = zone.split('/').pop() ?? zone;
	return last.replaceAll('_', ' ');
}

function offsetAt(zone: string, at: Date): string {
	try {
		return new Intl.DateTimeFormat('en-US', { timeZone: zone, timeZoneName: 'longOffset' }).formatToParts(at).find((p) => p.type === 'timeZoneName')?.value ?? zone;
	} catch {
		return zone;
	}
}

/** Whether two zones show the same wall-clock time at an instant. */
export function sameClock(a: string, b: string, at: Date = new Date()): boolean {
	return a === b || offsetAt(a, at) === offsetAt(b, at);
}

/** A short time in a given zone with its abbreviation, e.g. "3:00 PM WAT". */
export function clockIn(iso: string, zone: string): string {
	try {
		return new Intl.DateTimeFormat(undefined, { timeZone: zone, hour: 'numeric', minute: '2-digit', timeZoneName: 'short' }).format(new Date(iso));
	} catch {
		return new Date(iso).toLocaleTimeString();
	}
}

/** Just the clock time in a zone, e.g. "3:00 PM", for lists under a chosen date. */
export function timeOnly(iso: string, zone: string): string {
	try {
		return new Intl.DateTimeFormat(undefined, { timeZone: zone, hour: 'numeric', minute: '2-digit' }).format(new Date(iso));
	} catch {
		return new Date(iso).toLocaleTimeString();
	}
}

/** A full date and time in a given zone, e.g. "Thursday, 24 September 2026 at 3:00 PM WAT". */
export function dateTimeIn(iso: string, zone: string): string {
	try {
		return new Intl.DateTimeFormat(undefined, { timeZone: zone, dateStyle: 'full', timeStyle: 'short' }).format(new Date(iso)) + ' ' + (new Intl.DateTimeFormat('en-US', { timeZone: zone, timeZoneName: 'short' }).formatToParts(new Date(iso)).find((p) => p.type === 'timeZoneName')?.value ?? '');
	} catch {
		return new Date(iso).toLocaleString();
	}
}
