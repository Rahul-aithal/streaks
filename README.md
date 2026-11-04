# Streaks

A habit and daily-task tracker with Google sign-in. Each signed-in account has its own
habits, streaks and tasks, and you can swap between accounts like on GitHub. It computes
streaks from check-in records, snapshots Google Tasks daily, and lets GitHub activity
complete a "coding" habit automatically.

- **API:** one Go binary (`apps/api`), `net/http`, pgx, goose. No ORM.
- **Web:** React, TanStack Start in SPA mode, TanStack Query, Tailwind, shadcn/ui (`apps/web`).
- **Database:** Postgres on Neon. Day boundaries use Asia/Kolkata.

## Setup

You need Go 1.27+, Node 24+, pnpm, a Neon account and a Google Cloud project.

```sh
cp .env.example .env
make install
```

### 1. Neon

1. Create a project at [console.neon.tech](https://console.neon.tech). If it's shared with another app, create a separate `streaks` database.
2. On the project dashboard, click **Connect** and select that database.
3. With **Connection pooling on**, copy the string into `DATABASE_URL`. Its host contains `-pooler`.
4. With **Connection pooling off**, copy the string into `DATABASE_URL_DIRECT`.

### 2. Google sign-in

1. In the [Google Cloud console](https://console.cloud.google.com), create a project and enable the **Google Tasks API**.
2. Under **Google Auth Platform**, set the audience to **External**, and add the `openid`, `email`, `profile` and `tasks.readonly` scopes.
3. Choose who can get in. In **Testing**, list every account under **Test users**; tokens reportedly expire after 7 days, and Streaks then asks you to reconnect. **Publishing** the app removes both limits, and Google shows an "unverified app" warning you can click through for your own app.
4. Create an OAuth client of type **Web application**, with this **Authorized redirect URI**:

   ```
   http://localhost:5173/api/oauth/google/callback
   ```

5. In `.env`, set:
   - `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET`
   - `GOOGLE_TOKEN_KEY`, from `openssl rand -hex 32`
   - `ALLOWED_EMAILS`, listing every Google account that may sign in

Then create the tables:

```sh
make migrate
```

### 3. GitHub (optional, per account)

1. Create a [fine-grained token](https://github.com/settings/personal-access-tokens/new). Under **Account permissions**, set **Events** to **Read-only**. This is what exposes private activity.
2. In Streaks, open the account menu, go to **Settings**, and paste the token under **GitHub**.

Each account links its own token.

## Running

```sh
make dev
```

This starts the API on `127.0.0.1:8080` and the web app on <http://localhost:5173>. Stop both with Ctrl-C.

## Accounts

Click your avatar for the account menu:

- **Add account:** sign in with another Google account. Both stay signed in on this browser.
- **Switch account:** swap instantly, without signing in again. The whole app changes to that account's data.
- **Sign out:** signs out the active account and moves to the next. **Sign out of all accounts** ends the session.
- **Settings:** link GitHub, reconnect Google, or delete the account and its data.

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
- A failed sync keeps the last known tasks until it recovers.
- Snapshots are only taken while the API runs, so days the laptop is off have none.

## Security

- **Sign-in:** only accounts in `ALLOWED_EMAILS` can get in, and their email must be verified by Google.
- **Sessions:** an HttpOnly, SameSite=Lax cookie that lasts 30 days. Only its SHA-256 hash is stored, and signing out invalidates it on the server.
- **Isolation:** every data query is scoped to the signed-in user. Another user's habit returns 404.
- **Tokens:** Google and GitHub tokens are encrypted with AES-256-GCM using `GOOGLE_TOKEN_KEY`, and the API never returns them.
- **Requests:** state-changing requests from another origin are refused. The Google callback needs a one-time `state` value that matches a cookie in the same browser.

The API only listens on loopback. To host it, serve it over HTTPS and update `GOOGLE_REDIRECT_URI` and the OAuth client; cookies then become `Secure` automatically.

## Layout

```
apps/api/cmd/streaks       Entry point: serve, migrate
apps/api/internal/streak   Pure streak calculation and tests
apps/api/internal/store    All SQL, with timeouts and one retry on connection errors
apps/api/internal/httpapi  JSON routes, sign-in and sessions
apps/api/internal/github   GitHub Events client
apps/api/internal/gtasks   Google sign-in, Tasks client and daily counts
apps/api/internal/secretbox  Encryption for stored tokens
apps/api/internal/syncer   In-process sync loop
apps/api/migrations        Goose SQL, embedded in the binary
apps/web/src/routes        Login, Today, habits list, habit detail, settings
```
