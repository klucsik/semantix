import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: '.',
  timeout: 30000,
  retries: 1,
  use: {
    baseURL: process.env.BASE_URL || 'http://localhost:8080',
    trace: 'on-first-retry',
  },
  webServer: process.env.BASE_URL ? undefined : {
    command: 'go run ../../cmd/semantix/main.go --config ../../configs/examples/ecommerce.yaml',
    url: 'http://localhost:8080/health',
    timeout: 30000,
    reuseExistingServer: !process.env.CI,
  },
  projects: [
    {
      name: 'chromium',
      use: { browserName: 'chromium' },
    },
  ],
});
