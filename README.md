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
3. Choose who can get in. In **Testing**, list every account under **Test users**; tokens reportedly expire after 7 days, and Streaks then asks you to reconnect. **Publishing** the app removes both limits, but then anyone with a Google account can sign in, because Streaks has no list of its own. Google also shows an "unverified app" warning you can click through for your own app.
4. Create an OAuth client of type **Web application**, with this **Authorized redirect URI**:

   ```
   http://localhost:5173/api/oauth/google/callback
   ```

5. In `.env`, set:
   - `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET`
   - `GOOGLE_TOKEN_KEY`, from `openssl rand -hex 32`

Then create the tables:

```sh
make migrate
```

### 3. GitHub (optional, per account)

Do this once for the Connect GitHub button:

1. Create an [OAuth app](https://github.com/settings/applications/new). Use `http://localhost:5173` as the homepage URL, and this **Authorization callback URL**:

   ```
   http://localhost:5173/api/oauth/github/callback
   ```

2. Generate a client secret, then set `GITHUB_CLIENT_ID` and `GITHUB_CLIENT_SECRET` in `.env`.

Each Streaks account then connects its own GitHub accounts under **Settings**, as many as you like. Activity from all of them is pooled, so the GitHub activity habit counts a push or PR from any of them. To add another, use **Connect another account**: GitHub shows an account picker if you're signed in to several accounts on github.com; otherwise, sign in there to the other account first. Connecting an account that's already linked just refreshes its token.

Connect asks for no permissions, so it **only counts public repositories**. The OAuth permission GitHub documents for private repositories is `repo`, which also allows writing to your code, so Streaks doesn't ask for it.

To count private repositories, use a token instead: create a [fine-grained token](https://github.com/settings/personal-access-tokens/new), set **Events** to **Read-only** under **Account permissions**, and paste it under **Settings → GitHub**.

### 4. Reminder emails (optional)

Streaks can email each account a summary of today's habits and streaks at **11 AM, 5 PM and 10 PM IST**. The 10 PM email warns about streaks that end at midnight.

To send through [Resend](https://resend.com):

1. Create an API key with **Sending access**.
2. In `.env`, set:

   ```
   SMTP_HOST=smtp.resend.com
   SMTP_USERNAME=resend
   SMTP_PASSWORD=re_your_api_key
   EMAIL_FROM=onboarding@resend.dev
   ```

   Until you verify a domain in Resend, `onboarding@resend.dev` can only send to the email address you signed up to Resend with. To email other accounts, verify a domain and use an address on it as `EMAIL_FROM`.

3. Restart, then use **Settings → Email reminders → Send a test email**.

Or, to send through Gmail:

1. Turn on 2-Step Verification for the Google account that will send the emails.
2. Create an [app password](https://myaccount.google.com/apppasswords).
3. In `.env`, set:

   ```
   SMTP_HOST=smtp.gmail.com
   SMTP_USERNAME=you@gmail.com
   SMTP_PASSWORD=the-16-character-app-password
   ```

4. Restart, then use **Settings → Email reminders → Send a test email**.

Any SMTP server works. Port 465 uses TLS from the start, and other ports must offer STARTTLS. Each account can turn its emails off in Settings.

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
- **Settings:** connect GitHub, reconnect Google, or delete the account and its data.

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
   - **GitHub activity:** the sync checks it in on days you push to any repo or open a PR, including code pushed by tools like NeetCode's GitHub sync. It can't be checked in by hand. You can have any number of manual habits but only one active GitHub activity habit, since a second would count exactly the same pushes. To replace it, archive it first.
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

**Reminder emails**

- Each email lists today's habits as to do, done, or rest day, with each streak. A streak shown as at risk ends if the habit isn't done by midnight.
- GitHub is synced just before each email, so GitHub habits are up to date.
- Emails are only sent while the API runs. If the laptop wakes up within an hour of a reminder time, that reminder still goes out; later than that, it's skipped.
- Each reminder is sent at most once per account, even across restarts. A failed send is retried every 10 minutes within that hour.
- Accounts with no active habits get no emails.

## Security

- **Sign-in:** any Google account that gets past Google's consent screen can sign in, as long as Google has verified its email. While the Google app is in Testing, that means only its test users. Publishing the app would let anyone with a Google account in, each with their own empty account.
- **Sessions:** an HttpOnly, SameSite=Lax cookie that lasts 30 days. Only its SHA-256 hash is stored, and signing out invalidates it on the server.
- **Isolation:** every data query is scoped to the signed-in user. Another user's habit returns 404.
- **Tokens:** Google and GitHub tokens are encrypted with AES-256-GCM using `GOOGLE_TOKEN_KEY`, and the API never returns them. Disconnecting a GitHub account or deleting a Streaks account revokes the connected tokens. Connect requests no GitHub scopes.
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
apps/api/internal/reminder   Reminder email schedule and content
apps/api/internal/mailer     SMTP sending
apps/api/internal/syncer   In-process sync loop
apps/api/migrations        Goose SQL, embedded in the binary
apps/web/src/routes        Login, Today, habits list, habit detail, settings
```
