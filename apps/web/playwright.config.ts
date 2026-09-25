import { defineConfig, devices } from '@playwright/test';

// End-to-end tests run the real site against the real Go API and a fresh
// PostgreSQL database (see tests/e2e/global-setup.ts). They need
// E2E_DATABASE_URL: a connection string for a role that may create databases.
export const webPort = 5174;
export const apiPort = 18081;

export default defineConfig({
	testDir: 'tests/e2e',
	fullyParallel: false,
	workers: 1,
	retries: process.env.CI ? 1 : 0,
	timeout: 60_000,
	reporter: process.env.CI ? [['github'], ['html', { open: 'never' }]] : 'list',
	globalSetup: './tests/e2e/global-setup.ts',
	globalTeardown: './tests/e2e/global-teardown.ts',
	use: {
		baseURL: `http://127.0.0.1:${webPort}`,
		trace: 'retain-on-failure',
		launchOptions: process.env.PLAYWRIGHT_CHROMIUM_PATH ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM_PATH } : {}
	},
	projects: [
		{ name: 'desktop', use: { ...devices['Desktop Chrome'] } },
		{ name: 'phone', use: { ...devices['Pixel 7'] } }
	],
	webServer: {
		// A production build, as people get it; vite preview forwards /api/v1 like the dev server.
		command: `npx vite build && npx vite preview --host 127.0.0.1 --port ${webPort} --strictPort`,
		url: `http://127.0.0.1:${webPort}`,
		reuseExistingServer: false,
		timeout: 240_000,
		env: {
			API_INTERNAL_BASE: `http://127.0.0.1:${apiPort}`,
			PUBLIC_APP_ORIGIN: `http://127.0.0.1:${webPort}`
		}
	}
});
