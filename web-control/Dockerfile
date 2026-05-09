FROM m.daocloud.io/docker.io/library/node:22-bookworm-slim AS base

ENV NEXT_TELEMETRY_DISABLED=1

# --- Dependencies ---
FROM base AS deps
WORKDIR /app

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates python3 make g++ && \
    rm -rf /var/lib/apt/lists/*

COPY package.json package-lock.json ./

RUN npm ci

# --- Build ---
FROM base AS builder
WORKDIR /app

COPY --from=deps /app/node_modules ./node_modules
COPY . .

# Provide build-only placeholders inline so they are not persisted as image ENV metadata.
RUN JWT_SECRET="build-time-placeholder" \
    HUB_SECRET="build-time-placeholder" \
    SQLITE_PATH="/tmp/termx-web-build.sqlite" \
    npm run build

# --- Production ---
FROM base AS runner
WORKDIR /app

ENV NODE_ENV=production
ENV HOSTNAME=0.0.0.0
ENV PORT=3000
ENV SQLITE_PATH=/app/data/termx-web.sqlite

RUN groupadd --system --gid 1001 nodejs && \
    useradd --system --uid 1001 --gid nodejs --home-dir /app nextjs

# Copy standalone output
COPY --from=builder /app/.next/standalone ./
COPY --from=builder /app/.next/static ./.next/static
COPY --from=builder /app/public ./public

# Copy Drizzle migrations (needed at runtime for db:migrate)
COPY --from=builder /app/drizzle ./drizzle

# Create runtime directories writable by nextjs user.
RUN mkdir -p .next/cache /app/data && \
    chown -R nextjs:nodejs .next /app/data

USER nextjs

EXPOSE 3000

# All real env vars are provided at runtime via docker run -e or docker-compose
CMD ["node", "server.js"]
