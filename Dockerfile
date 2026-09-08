FROM oven/bun:1.4-slim

WORKDIR /app

# Install Chromium and dependencies for WhatsApp Web automation
RUN apt-get update && apt-get install -y --no-install-recommends \
    chromium \
    chromium-sandbox \
    fonts-freefont-ttf \
    fonts-liberation \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

ENV CHROMIUM_PATH=/usr/bin/chromium

# Install production dependencies
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile --production

# Copy only application runtime files and build client bundle
COPY tsconfig.json ./
COPY drizzle/ ./drizzle/
COPY public/ ./public/
COPY src/ ./src/
RUN bun run build
EXPOSE 3000

ENV NODE_ENV=production
ENV HOST=0.0.0.0
ENV PORT=3000

CMD ["bun", "src/index.tsx"]

