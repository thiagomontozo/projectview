import { describe, expect, it } from 'vitest';
import { endsSession } from '../lib/api';

/**
 * Regression tests for a blank login screen.
 *
 * A visitor who is not signed in gets a 401 from /auth/me and cannot refresh —
 * exactly like someone whose session expired. Treating the two the same way
 * looped: the expiry handler cleared the query cache, the cleared `me` query
 * refetched immediately, got another 401, and cleared the cache again. The
 * query never settled, so `loading` stayed true and the app rendered skeletons
 * forever instead of the login form.
 */
describe('endsSession', () => {
  it('ends the session when a held token could not be refreshed', () => {
    expect(endsSession(true, null)).toBe(true);
  });

  it('is not an expiry when the client never had a token', () => {
    // The normal state of anyone who has not signed in yet.
    expect(endsSession(false, null)).toBe(false);
  });

  it('is not an expiry when the refresh succeeded', () => {
    expect(endsSession(true, 'new-access-token')).toBe(false);
  });

  it('does not fire again once the token has already been cleared', () => {
    // The second 401 of the clear-and-refetch cycle: without this, the loop
    // that caused the blank screen never terminates.
    expect(endsSession(false, null)).toBe(false);
  });
});
