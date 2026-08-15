import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { I18nextProvider } from 'react-i18next';
import i18n from '../i18n';
import { Button } from '../ui/Button';
import { Field, Input } from '../ui/Field';
import { Avatar, Badge, EmptyState } from '../ui/display';

function renderWithI18n(ui: React.ReactElement) {
  return render(<I18nextProvider i18n={i18n}>{ui}</I18nextProvider>);
}

describe('Button', () => {
  it('is disabled and marked busy while loading', () => {
    renderWithI18n(<Button loading>Save</Button>);
    const button = screen.getByRole('button', { name: /save/i });
    expect(button).toBeDisabled();
    // The busy state has to reach assistive tech, not just the spinner.
    expect(button).toHaveAttribute('aria-busy', 'true');
  });

  it('does not fire onClick while loading', async () => {
    const onClick = vi.fn();
    renderWithI18n(
      <Button loading onClick={onClick}>
        Save
      </Button>
    );
    await userEvent.click(screen.getByRole('button')).catch(() => undefined);
    expect(onClick).not.toHaveBeenCalled();
  });
});

describe('Field', () => {
  it('associates its label with the control', () => {
    renderWithI18n(<Field label="Project name">{({ id }) => <Input id={id} />}</Field>);
    // getByLabelText only finds it if the association actually exists.
    expect(screen.getByLabelText('Project name')).toBeInTheDocument();
  });

  it('announces the error and marks the control invalid', () => {
    renderWithI18n(
      <Field label="Key" error="Already taken">
        {({ id, describedBy, invalid }) => <Input id={id} aria-describedby={describedBy} invalid={invalid} />}
      </Field>
    );

    const input = screen.getByLabelText('Key');
    expect(input).toHaveAttribute('aria-invalid', 'true');
    // role="alert" is what makes the message spoken the moment it appears.
    const error = screen.getByRole('alert');
    expect(error).toHaveTextContent('Already taken');
    expect(input.getAttribute('aria-describedby')).toContain(error.id);
  });

  it('hides the hint once an error takes its place', () => {
    renderWithI18n(
      <Field label="Key" hint="Short identifier" error="Already taken">
        {({ id }) => <Input id={id} />}
      </Field>
    );
    expect(screen.queryByText('Short identifier')).not.toBeInTheDocument();
  });
});

describe('Avatar', () => {
  it('exposes the person, not their initials', () => {
    renderWithI18n(<Avatar name="Ana Paula Souza" />);
    // The image role carries the full name; the letters are decorative.
    expect(screen.getByRole('img', { name: 'Ana Paula Souza' })).toBeInTheDocument();
  });

  it('derives initials from the first and last name', () => {
    renderWithI18n(<Avatar name="Ana Paula Souza" />);
    expect(screen.getByText('AS')).toBeInTheDocument();
  });

  it('handles a single-word name', () => {
    renderWithI18n(<Avatar name="Ana" />);
    expect(screen.getByText('AN')).toBeInTheDocument();
  });
});

describe('EmptyState', () => {
  it('renders its title, body and action', () => {
    renderWithI18n(
      <EmptyState title="No projects yet" body="Create the first one." action={<Button>New</Button>} />
    );
    expect(screen.getByText('No projects yet')).toBeInTheDocument();
    expect(screen.getByText('Create the first one.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'New' })).toBeInTheDocument();
  });
});

describe('Badge', () => {
  it('renders its content', () => {
    renderWithI18n(<Badge tone="danger">Overdue</Badge>);
    expect(screen.getByText('Overdue')).toBeInTheDocument();
  });
});
