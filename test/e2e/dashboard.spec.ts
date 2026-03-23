import { test, expect } from '@playwright/test';

test.describe('Dashboard', () => {
  // Critical test: catches JavaScript syntax errors like the optional chaining bug
  test('loads without JavaScript syntax errors', async ({ page }) => {
    const jsErrors: string[] = [];
    
    // Capture any JavaScript errors (syntax errors, runtime errors)
    page.on('pageerror', (error) => {
      jsErrors.push(`PageError: ${error.message}`);
    });

    // Navigate to the dashboard
    const response = await page.goto('/', { waitUntil: 'domcontentloaded' });
    expect(response?.status()).toBe(200);
    
    // Wait a moment for any JS to execute and potentially error
    await page.waitForTimeout(2000);

    // The critical assertion: no JS errors
    if (jsErrors.length > 0) {
      console.error('JavaScript errors detected:', jsErrors);
    }
    expect(jsErrors, `JavaScript errors found: ${jsErrors.join('; ')}`).toHaveLength(0);
  });

  // Test that the dashboard HTML structure is correct
  test('has required DOM elements', async ({ page }) => {
    await page.goto('/', { waitUntil: 'domcontentloaded' });
    
    // Check essential elements exist
    await expect(page.locator('#topology')).toBeAttached();
    await expect(page.locator('#total-services')).toBeAttached();
    await expect(page.locator('#detail-panel')).toBeAttached();
    await expect(page.locator('.header')).toBeAttached();
  });

  // API tests - these always work and validate the backend
  test('topology API returns valid JSON', async ({ request }) => {
    const response = await request.get('/api/topology');
    expect(response.status()).toBe(200);
    
    const data = await response.json();
    expect(data).toHaveProperty('nodes');
    expect(data).toHaveProperty('links');
    expect(data).toHaveProperty('stats');
    expect(Array.isArray(data.nodes)).toBe(true);
    expect(data.nodes.length).toBeGreaterThan(0);
  });

  test('health endpoint returns ok', async ({ request }) => {
    const response = await request.get('/health');
    expect(response.status()).toBe(200);
    expect(await response.text()).toBe('ok');
  });
});
