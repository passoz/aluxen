FROM oven/bun:1.4-slim

WORKDIR /app

# Install dependencies
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile

# Copy project source and build client bundle
COPY . .
RUN bun run build

EXPOSE 3000

ENV NODE_ENV=production
ENV HOST=0.0.0.0
ENV PORT=3000

CMD ["bun", "src/index.tsx"]
