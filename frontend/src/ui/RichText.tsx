import { useEditor, EditorContent, type Editor } from '@tiptap/react';
import StarterKit from '@tiptap/starter-kit';
import Link from '@tiptap/extension-link';
import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import clsx from 'clsx';
import controls from './controls.module.css';

/**
 * Rich text, where plain text was not enough.
 *
 * The backlog carried "no rich-text editor" as a deliberate choice for a long
 * time, and the reasoning still holds for *documents*: Markdown stays greppable,
 * diffable and portable, and a proprietary document model would be a format the
 * database has to understand. So documents keep Markdown, and this is for the
 * places where the alternative was not Markdown but a bare textarea — task
 * descriptions and comments, where people were already pasting lists and links
 * and getting a wall of text back.
 *
 * TipTap over ProseMirror, MIT-licensed. The editor stores **HTML**, which is
 * what makes it a bounded change: the column is already text, the value is
 * still a string, and a task whose description was typed before this shipped
 * renders unchanged because plain text is valid HTML.
 *
 * What it deliberately does not offer: images, tables, colours, fonts. Those
 * turn a description into a document, and this application already has
 * documents.
 */

interface Props {
  value: string;
  onChange: (html: string) => void;
  placeholder?: string;
  /** Wired to the label of the surrounding Field. */
  id?: string;
  ariaLabel?: string;
  disabled?: boolean;
}

export function RichText({ value, onChange, id, ariaLabel, disabled }: Props) {
  const { t } = useTranslation();

  const editor = useEditor({
    extensions: [
      StarterKit.configure({
        // Headings belong in a document, not in a task description. Left out so
        // the toolbar stays short enough to be read rather than scanned.
        heading: false
      }),
      Link.configure({
        openOnClick: false,
        autolink: true,
        // Anything that is not http(s) or mailto is refused. A description is
        // written by one colleague and read by another, so "javascript:" in an
        // href is a stored script waiting for a click.
        protocols: ['http', 'https', 'mailto'],
        HTMLAttributes: { rel: 'noopener noreferrer nofollow', target: '_blank' }
      })
    ],
    content: value,
    editable: !disabled,
    onUpdate: ({ editor: current }) => {
      // isEmpty rather than comparing to "<p></p>": an editor cleared by the
      // person should store an empty string, or every emptied field would come
      // back as a paragraph tag and count as content.
      onChange(current.isEmpty ? '' : current.getHTML());
    },
    editorProps: {
      attributes: {
        id: id ?? '',
        'aria-label': ariaLabel ?? '',
        class: controls.richTextArea
      }
    }
  });

  // Keeps the editor in step when the value changes from outside — switching to
  // another task in the same dialog, or a save that returns normalised HTML.
  // Guarded against its own output, or every keystroke would reset the cursor.
  useEffect(() => {
    if (!editor) return;
    if (value !== editor.getHTML() && !(editor.isEmpty && value === '')) {
      // `false` is this version's "do not emit an update": re-emitting
      // here would call onChange with the value that just arrived and loop.
      editor.commands.setContent(value, false);
    }
  }, [value, editor]);

  if (!editor) return null;

  return (
    <div className={controls.richText}>
      <Toolbar editor={editor} disabled={disabled} label={t('richText.formatting')} />
      <EditorContent editor={editor} />
    </div>
  );
}

function Toolbar({ editor, disabled, label }: { editor: Editor; disabled?: boolean; label: string }) {
  const { t } = useTranslation();

  const buttons = [
    { key: 'bold', run: () => editor.chain().focus().toggleBold().run(), active: editor.isActive('bold') },
    { key: 'italic', run: () => editor.chain().focus().toggleItalic().run(), active: editor.isActive('italic') },
    { key: 'strike', run: () => editor.chain().focus().toggleStrike().run(), active: editor.isActive('strike') },
    { key: 'code', run: () => editor.chain().focus().toggleCode().run(), active: editor.isActive('code') },
    {
      key: 'bulletList',
      run: () => editor.chain().focus().toggleBulletList().run(),
      active: editor.isActive('bulletList')
    },
    {
      key: 'orderedList',
      run: () => editor.chain().focus().toggleOrderedList().run(),
      active: editor.isActive('orderedList')
    }
  ];

  return (
    // A toolbar rather than a row of divs: the role and the pressed state are
    // what make the formatting reachable and reportable without a mouse.
    <div className={controls.richTextToolbar} role="toolbar" aria-label={label}>
      {buttons.map((button) => (
        <button
          key={button.key}
          type="button"
          disabled={disabled}
          aria-pressed={button.active}
          aria-label={t(`richText.${button.key}`)}
          title={t(`richText.${button.key}`)}
          className={clsx(controls.richTextButton, button.active && controls.richTextButtonActive)}
          // Mouse-down rather than click: clicking a toolbar button moves focus
          // out of the editor first, which collapses the selection the command
          // was meant to act on.
          onMouseDown={(event) => {
            event.preventDefault();
            button.run();
          }}
        >
          {t(`richText.${button.key}Short`)}
        </button>
      ))}
    </div>
  );
}

/**
 * Renders stored rich text for reading.
 *
 * Through a read-only editor rather than `dangerouslySetInnerHTML`, and that is
 * the security boundary rather than a stylistic preference. The value is a
 * string in a text column: the editor produces bounded HTML, but any API client
 * can PUT a description containing a `<script>` or an `onerror` attribute, and
 * injecting that straight into the DOM would make it stored cross-site
 * scripting that every reader executes.
 *
 * ProseMirror parses the HTML against its schema and keeps only the nodes and
 * marks that schema defines. Anything else — script tags, event handlers,
 * iframes, styles — is dropped on the way in, because there is nowhere in the
 * schema to put it. Sanitising as a side effect of parsing is stronger than a
 * deny-list, since it can only ever render what it knows how to represent.
 */
export function RichTextView({ html, className }: { html: string; className?: string }) {
  const editor = useEditor(
    {
      extensions: [
        StarterKit.configure({ heading: false }),
        Link.configure({
          openOnClick: true,
          protocols: ['http', 'https', 'mailto'],
          HTMLAttributes: { rel: 'noopener noreferrer nofollow', target: '_blank' }
        })
      ],
      content: html,
      editable: false
    },
    [html]
  );

  if (!html || !editor) return null;
  return <EditorContent editor={editor} className={clsx(controls.richTextReadOnly, className)} />;
}
