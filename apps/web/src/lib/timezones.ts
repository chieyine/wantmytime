// Every IANA zone the browser knows, most-used first, so a seller or buyer
// anywhere can pick their own. A saved zone that is not in the list is kept
// as an option so the select never silently shows a blank value.
const common = ['Africa/Lagos', 'Africa/Accra', 'Africa/Nairobi', 'Africa/Johannesburg', 'Africa/Cairo', 'Europe/London', 'Europe/Paris', 'America/New_York', 'America/Chicago', 'America/Los_Angeles', 'America/Toronto', 'Asia/Dubai', 'UTC'];

function allZones(): string[] {
	try {
		const zones = (Intl as unknown as { supportedValuesOf?: (key: string) => string[] }).supportedValuesOf?.('timeZone') ?? [];
		return zones.length ? zones : [];
	} catch {
		return [];
	}
}

export function timezoneOptions(current: string): string[] {
	const rest = allZones().filter((zone) => !common.includes(zone));
	const list = [...common, ...rest];
	return current && !list.includes(current) ? [current, ...list] : list;
}

/** The browser's own zone, falling back to UTC. */
export function browserZone(): string {
	try {
		return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
	} catch {
		return 'UTC';
	}
}
