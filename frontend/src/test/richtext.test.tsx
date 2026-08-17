import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { RichTextView } from '../ui/RichText';

/**
 * Stored rich text is rendered through the editor's own schema, not injected
 * into the DOM.
 *
 * That is a security boundary, not a styling choice: the description and
 * comment columns are plain text columns, so any API client can PUT whatever
 * it likes into them. Rendering that with dangerouslySetInnerHTML would make it
 * stored cross-site scripting executed by every reader.
 *
 * ProseMirror keeps only what its schema can represent, so these tests assert
 * the *effect* — the dangerous element is gone and the legitimate text
 * survives — rather than the mechanism.
 */
describe('RichTextView sanitises what it renders', () => {
  it('drops a script tag but keeps the text around it', async () => {
    const { container } = render(
      <RichTextView html={'<p>Before</p><script>window.__pwned = true;</script><p>After</p>'} />
    );

    expect(container.querySelector('script')).toBeNull();
    expect((window as unknown as { __pwned?: boolean }).__pwned).toBeUndefined();
    expect(await screen.findByText('Before')).toBeTruthy();
    expect(screen.getByText('After')).toBeTruthy();
  });

  it('drops an inline event handler', async () => {
    const { container } = render(<RichTextView html={'<p onmouseover="window.__pwned=1">Hover me</p>'} />);

    await screen.findByText('Hover me');
    const paragraph = container.querySelector('p');
    expect(paragraph?.getAttribute('onmouseover')).toBeNull();
  });

  it('drops an iframe', async () => {
    const { container } = render(
      <RichTextView html={'<p>Text</p><iframe src="https://evil.example"></iframe>'} />
    );

    await screen.findByText('Text');
    expect(container.querySelector('iframe')).toBeNull();
  });

  it('keeps the formatting the editor is allowed to produce', async () => {
    const { container } = render(
      <RichTextView html={'<p><strong>bold</strong> and <em>italic</em></p><ul><li>one</li></ul>'} />
    );

    await screen.findByText('bold');
    expect(container.querySelector('strong')).not.toBeNull();
    expect(container.querySelector('em')).not.toBeNull();
    expect(container.querySelector('li')).not.toBeNull();
  });

  // Text written before the editor existed is plain text, which is valid HTML.
  // It has to keep rendering, or every old description would come back blank.
  it('renders plain text stored before the editor shipped', async () => {
    render(<RichTextView html={'a plain description typed years ago'} />);
    expect(await screen.findByText('a plain description typed years ago')).toBeTruthy();
  });

  it('renders nothing for an empty value', () => {
    const { container } = render(<RichTextView html="" />);
    expect(container.textContent).toBe('');
  });
});
