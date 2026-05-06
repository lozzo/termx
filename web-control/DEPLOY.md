# Web Control Deployment

`web-control` is the TermX management-plane service. It handles user login,
hub directory state, machine listings, subscriptions, quotas, and forced
disconnect control. It does not proxy terminal runtime traffic, issue
connection tickets, or relay offer/answer signaling.

## Minimal Local Startup

```bash
cd web-control
cp .env.example .env
npm install
npm run dev
```

The development server listens on port `12306` by default because
`package.json` runs `next dev -p 12306`.

Verify the service:

```bash
curl http://localhost:12306/api/health
```

Expected response includes:

```json
{"status":"ok"}
```

Register the first user through the browser at
`http://localhost:12306/register`, or with the API:

```bash
curl -X POST http://localhost:12306/api/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"demo","email":"demo@example.com","password":"secret123"}'
```

## Required Environment

- `JWT_SECRET`: access-token signing secret. Replace the development value in
  production.
- `HUB_SECRET`: shared secret for termx-hub management-plane heartbeat. This
  must match `TERMX_HUB_CONTROL_SECRET` in the hub environment.
- `APP_URL`: public Web Controller URL, for example `https://control.example.com`.
- `SQLITE_PATH`: SQLite database path. Defaults to `./data/termx-web.sqlite`.
- `GITHUB_CLIENT_ID`: optional GitHub OAuth client id.
- `GITHUB_CLIENT_SECRET`: optional GitHub OAuth client secret.

## Database Bootstrap

No database migration command is required for the P0 deployment path.
`src/lib/db.ts` opens SQLite and calls `ensureSqliteSchema` on startup, creating
the database directory and schema automatically.

`npm run db:seed` is optional. It only upserts default plan rows through
`prisma/seed.ts`; user registration works without running it.

## Hub Heartbeat Check

After starting `termx-hub` with matching `TERMX_HUB_CONTROL_SECRET`, its
heartbeat should post to:

```text
POST /api/internal/hubs/heartbeat
```

The request must include `X-TermX-Hub-Secret: <HUB_SECRET>` and should receive a
200 response with a JSON body containing `kick_agents`.
