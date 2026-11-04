# Streaks

A personal habit and daily-task tracker for one user. It computes streaks from
check-in records, snapshots Google Tasks daily, and lets GitHub activity complete
a "coding" habit automatically.

- **API:** one Go binary (`apps/api`), `net/http`, pgx, goose. No ORM.
- **Web:** React, TanStack Start in SPA mode, TanStack Query, Tailwind, shadcn/ui (`apps/web`).
- **Database:** Postgres on Neon. Day boundaries use Asia/Kolkata.

## Setup

You need Go 1.27+, Node 24+, pnpm, and a Neon account.

```sh
cp .env.example .env
make install
```

### 1. Neon (required)

1. Create a project at [console.neon.tech](https://console.neon.tech).
2. On the project dashboard, click **Connect**.
3. With **Connection pooling on**, copy the string into `DATABASE_URL`. Its host contains `-pooler`.
4. With **Connection pooling off**, copy the string into `DATABASE_URL_DIRECT`.
5. Make sure both end in `sslmode=require`.

Set a secret, then create the tables:

```sh
openssl rand -hex 32   # Paste into API_SECRET in .env
make migrate
```

### 2. GitHub (optional)

1. Go to **GitHub → Settings → Developer settings → Fine-grained tokens** and generate a token.
2. Under **Account permissions**, set **Events** to **Read-only**. This is what exposes private activity.
3. Set `GITHUB_USERNAME` and `GITHUB_TOKEN` in `.env`.

### 3. Google Tasks (optional, one or more accounts)

1. In the [Google Cloud console](https://console.cloud.google.com), create a project and enable the **Google Tasks API**.
2. Configure the OAuth consent screen as **External**, and add the `openid`, `email` and `tasks.readonly` scopes.
3. **Publish the app (In production).** In Testing, refresh tokens reportedly expire after 7 days, and every account must be listed as a test user. Google will warn that the app is unverified; that's expected for your own app.
4. Create an OAuth client of type **Web application**, with this **Authorized redirect URI**. No JavaScript origin is needed:

   ```
   http://localhost:5173/api/oauth/google/callback
   ```

5. In `.env`, set `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET`. Set `GOOGLE_TOKEN_KEY` from `openssl rand -hex 32`.
6. Run `make migrate` and `make dev`, open **Accounts**, and click **Connect Google account**. Repeat for each account.

Refresh tokens are stored in Neon, encrypted with `GOOGLE_TOKEN_KEY`. Keep that key safe: if it changes, every account has to reconnect.

If Google stops accepting an account's token, the Accounts and Today pages ask you to reconnect it. The other accounts keep syncing.

## Running

```sh
make dev
```

This starts the API on `127.0.0.1:8080` and the web app on <http://localhost:5173>. Stop both with Ctrl-C.

| Command | What it does |
|---|---|
| `make dev` | Run the API and web app |
| `make build` | Build the API binary and the web app |
| `make test` | Run Go tests and the TypeScript typecheck |
| `make lint` | Run `go vet`, the `gofmt` check and the typecheck |
| `make migrate` | Apply migrations using `DATABASE_URL_DIRECT` |
| `make migrate-status` | Show migration state |

Streak tests don't need a database. To also run the database tests, point them at a
**disposable, migrated** database, because they delete rows:

```sh
cd apps/api && TEST_DATABASE_URL='postgres://...' go test -p 1 ./internal/store/ ./internal/httpapi/
```

## Adding a habit

1. Open **Habits → New habit**.
2. Enter a name and choose a source:
   - **Manual:** you check it in from the Today view.
   - **GitHub:** the sync checks it in on days with activity. It can't be checked in by hand.
3. Pick target days, or leave them all off for every day.

To backdate a check-in, open the habit and use **Recent days**. Today and the 2 days before it are allowed.

## How things are counted

**Streaks**

- A target day with a check-in adds 1. A target day without one resets the streak to 0.
- Today can't break a streak until the day is over in IST.
- Non-target days hold the streak. A check-in on one is recorded and updates the last completed date, but adds nothing.
- Changing target days applies from today, so past streaks keep the schedule they had.
- Archiving holds the streak and stops scheduling the habit. Delete removes its history and can change the daily completion streak.

**Daily completion streak:** a day counts when every habit scheduled that day is done. Days with nothing scheduled are skipped.

**GitHub**

- Activity comes from the REST Events API, which keeps 30 days and up to 300 events. Syncs run every `SYNC_INTERVAL` (6h by default), so nothing is missed unless the API stays off for more than 30 days.
- The per-day count is **pushes plus PRs opened, not commits**, because push events don't include a commit count.
- A day is based on when you pushed, in IST. A commit at 23:50 pushed at 00:10 counts for the next day.

**Google Tasks**

- `overdue` is open tasks due before today, and `completed` is tasks completed today.
- `total` is open tasks due today or earlier, plus tasks completed today.
- Undated tasks aren't counted or shown.
- Counts combine all connected accounts. An account that fails to sync keeps its last known tasks until it recovers or is disconnected.
- Disconnecting an account removes its tasks and corrects today's snapshot. Earlier snapshots aren't changed.
- Snapshots are only taken while the API runs, so days the laptop is off have none.

## Security

The API listens on loopback and checks `API_SECRET` in the `X-Streaks-Secret` header.
The web app reads the same value at build time, so **the secret ends up in the browser
bundle**. That's fine on localhost. Before hosting this publicly, put a real login in front of it.

Only the Google callback route skips the secret, because Google's redirect can't send it.
It accepts a request only with a one-time `state` value, which expires after 10 minutes.

## Layout

```
apps/api/cmd/streaks       Entry point: serve, migrate
apps/api/internal/streak   Pure streak calculation and tests
apps/api/internal/store    All SQL, with timeouts and one retry on connection errors
apps/api/internal/httpapi  JSON routes and the shared-secret check
apps/api/internal/github   GitHub Events client
apps/api/internal/gtasks   Google login, Tasks client and daily counts
apps/api/internal/secretbox  Encryption for stored refresh tokens
apps/api/internal/syncer   In-process sync loop
apps/api/migrations        Goose SQL, embedded in the binary
apps/web/src/routes        Today, habits list, habit detail, accounts
```
