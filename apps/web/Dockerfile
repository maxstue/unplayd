FROM node:24.21.0-bookworm-slim AS build
WORKDIR /app
RUN npm install -g pnpm@12.11.2
COPY package.json pnpm-workspace.yaml pnpm-lock.yaml ./
COPY apps/web/package.json apps/web/package.json
RUN pnpm install --frozen-lockfile --ignore-scripts
COPY apps/web apps/web
COPY vite.config.ts ./
RUN pnpm exec vp run build:web

FROM node:24.21.0-bookworm-slim
WORKDIR /app
ENV NODE_ENV=production HOST=0.0.0.0 PORT=3000
COPY --from=build --chown=node:node /app/apps/web/.output .output
USER node
EXPOSE 3000
CMD ["node", ".output/server/index.mjs"]
