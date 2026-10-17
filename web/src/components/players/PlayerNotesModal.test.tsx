// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest';
import { act } from 'react';
import { PlayerNotesModal } from './PlayerNotesModal';
import { cleanup, clickText, mount } from '../../test/dom';

describe('PlayerNotesModal', () => {
  afterEach(() => {
    cleanup();
  });

  it('renders initial tags and notes', async () => {
    const { container } = await mount(
      <PlayerNotesModal
        isOpen={true}
        initialNotes="Always rotates back post"
        initialTags={['clutch', 'reliable']}
        onClose={vi.fn()}
        onSave={vi.fn()}
      />
    );

    expect(container.textContent).toContain('EDIT SCOUTING NOTES & TAGS');
    expect(container.textContent).toContain('clutch');
    expect(container.textContent).toContain('reliable');
    const textarea = container.querySelector('textarea');
    expect(textarea?.value).toBe('Always rotates back post');
  });

  it('allows adding suggested tags', async () => {
    const { container } = await mount(
      <PlayerNotesModal
        isOpen={true}
        initialNotes=""
        initialTags={['clutch']}
        onClose={vi.fn()}
        onSave={vi.fn()}
      />
    );

    const suggestionBtn = Array.from(container.querySelectorAll('button')).find((b) =>
      b.textContent?.includes('+smurf')
    );
    expect(suggestionBtn).toBeTruthy();

    await act(async () => {
      suggestionBtn?.click();
    });

    expect(container.textContent).toContain('smurf');
  });

  it('allows removing an existing tag', async () => {
    const { container } = await mount(
      <PlayerNotesModal
        isOpen={true}
        initialNotes=""
        initialTags={['toxic', 'clutch']}
        onClose={vi.fn()}
        onSave={vi.fn()}
      />
    );

    expect(container.querySelector('[data-testid="tag-chip-toxic"]')).toBeTruthy();
    const removeBtn = container.querySelector('[data-testid="tag-chip-toxic"] button');
    expect(removeBtn).toBeTruthy();

    await act(async () => {
      (removeBtn as HTMLElement)?.click();
    });

    expect(container.querySelector('[data-testid="tag-chip-toxic"]')).toBeNull();
    expect(container.querySelector('[data-testid="tag-chip-clutch"]')).toBeTruthy();
  });

  it('submits updated notes and tags on SAVE CHANGES', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    const onClose = vi.fn();

    const { container } = await mount(
      <PlayerNotesModal
        isOpen={true}
        initialNotes="Initial note"
        initialTags={['clutch']}
        onClose={onClose}
        onSave={onSave}
      />
    );

    const textarea = container.querySelector('textarea')!;
    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(
        window.HTMLTextAreaElement.prototype,
        'value'
      )?.set;
      setter?.call(textarea, 'Updated scout observation');
      textarea.dispatchEvent(new Event('input', { bubbles: true }));
      textarea.dispatchEvent(new Event('change', { bubbles: true }));
    });

    await clickText(container, 'SAVE CHANGES');

    expect(onSave).toHaveBeenCalledWith('Updated scout observation', ['clutch']);
    expect(onClose).toHaveBeenCalled();
  });
});
