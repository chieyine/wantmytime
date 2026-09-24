// Common zones for this market. A saved zone that is not in the list is kept
// as an option so the select never silently shows a blank value.
const common = ['Africa/Lagos', 'Africa/Accra', 'Africa/Nairobi', 'Africa/Johannesburg', 'Africa/Cairo', 'Europe/London', 'Europe/Paris', 'America/New_York', 'America/Chicago', 'America/Los_Angeles', 'America/Toronto', 'Asia/Dubai', 'UTC'];

export function timezoneOptions(current: string): string[] {
	return current && !common.includes(current) ? [current, ...common] : common;
}
