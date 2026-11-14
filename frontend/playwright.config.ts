import { defineConfig } from '@playwright/test';

export default defineConfig({
 testDir: './tests', fullyParallel: false, workers: 1, reporter: 'line',
 use: { baseURL: 'http://127.0.0.1:5173', channel: 'chrome', viewport: { width: 1180, height: 760 }, trace: 'retain-on-failure' },
 webServer: { command: 'npm run preview -- --host 127.0.0.1 --port 5173 --strictPort', url: 'http://127.0.0.1:5173', reuseExistingServer: !process.env.CI, timeout: 30000 },
});
