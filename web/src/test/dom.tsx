// Minimal DOM harness for component tests running under happy-dom.
import React from 'react';
import { act } from 'react';
import { createRoot, Root } from 'react-dom/client';

(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

export interface Mounted {
  container: HTMLDivElement;
  rerender: (ui: React.ReactElement) => Promise<void>;
  unmount: () => void;
}

const mounted: Mounted[] = [];

export async function mount(ui: React.ReactElement): Promise<Mounted> {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root: Root = createRoot(container);
  await act(async () => {
    root.render(ui);
  });
  const m: Mounted = {
    container,
    rerender: async (next) => {
      await act(async () => {
        root.render(next);
      });
    },
    unmount: () => {
      act(() => root.unmount());
      container.remove();
    },
  };
  mounted.push(m);
  return m;
}

export function cleanup() {
  while (mounted.length) mounted.pop()!.unmount();
}

/** Clicks the first button-like element whose text starts with `text`. */
export async function clickText(root: ParentNode, text: string) {
  const el = Array.from(root.querySelectorAll<HTMLElement>('button, [role="button"], a')).find((b) =>
    (b.textContent || '').trim().startsWith(text),
  );
  if (!el) throw new Error(`No clickable element starting with "${text}"`);
  await act(async () => {
    el.click();
  });
}

export async function flush(ms = 0) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

export function setSearch(query: string) {
  window.history.replaceState(null, '', `/${query}`);
}
