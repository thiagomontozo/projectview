# Settings mirror

The settings screen writes a `.env`-shaped copy of the managed configuration
here, as `app.env`.

**The database is authoritative.** This file is a mirror kept for backup,
review and for rebuilding the stack elsewhere — the running application reads
its settings from PostgreSQL, applies them immediately, and does not read this
file back. Editing it by hand changes nothing.

It is not committed, because it carries the directory bind password and the
mail account. Back it up the way you back up `.env`.
