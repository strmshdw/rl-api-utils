// @vitest-environment happy-dom
import { describe, it, expect, afterEach } from 'vitest';
import { Sparkline } from './Sparkline';
import { cleanup, mount } from '../../test/dom';

describe('Sparkline', () => {
  afterEach(() => {
    cleanup();
  });

  it('renders dashed baseline when data is empty', async () => {
    const { container } = await mount(<Sparkline data={[]} width={100} height={28} />);
    const svg = container.querySelector('[data-testid="sparkline"]');
    expect(svg).toBeTruthy();
    const line = svg?.querySelector('line');
    expect(line).toBeTruthy();
    expect(line?.getAttribute('stroke-dasharray')).toBe('3 3');
  });

  it('renders dot and dashed line for a single data point', async () => {
    const { container } = await mount(<Sparkline data={[{ mmr: 1000 }]} width={100} height={28} />);
    const svg = container.querySelector('[data-testid="sparkline"]');
    expect(svg).toBeTruthy();
    const circle = svg?.querySelector('circle');
    expect(circle).toBeTruthy();
    expect(circle?.getAttribute('cx')).toBe('50');
  });

  it('renders green stroke for positive MMR delta', async () => {
    const { container } = await mount(
      <Sparkline data={[{ mmr: 1000 }, { mmr: 1020 }, { mmr: 1050 }]} width={100} height={28} />
    );
    const svg = container.querySelector('[data-testid="sparkline"]');
    expect(svg).toBeTruthy();
    const path = svg?.querySelector('path[stroke]');
    expect(path?.getAttribute('stroke')).toBe('#10b981'); // emerald/green
  });

  it('renders red stroke for negative MMR delta', async () => {
    const { container } = await mount(
      <Sparkline data={[{ mmr: 1050 }, { mmr: 1020 }, { mmr: 1000 }]} width={100} height={28} />
    );
    const svg = container.querySelector('[data-testid="sparkline"]');
    expect(svg).toBeTruthy();
    const path = svg?.querySelector('path[stroke]');
    expect(path?.getAttribute('stroke')).toBe('#ef4444'); // red
  });

  it('supports raw number arrays as data points', async () => {
    const { container } = await mount(<Sparkline data={[100, 150, 120]} width={120} height={32} />);
    const svg = container.querySelector('[data-testid="sparkline"]');
    expect(svg).toBeTruthy();
    const paths = svg?.querySelectorAll('path');
    expect(paths?.length).toBe(2); // area fill path + line path
  });
});
